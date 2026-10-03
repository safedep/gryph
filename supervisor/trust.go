package supervisor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/peercred"
	"github.com/safedep/gryph/platform/procs"
)

// agentProcess names one process of a known agent: name:pid:start, with
// the start in Unix nanoseconds. A session binds to the first one seen
// above a hook of the session.
type agentProcess struct {
	name    string
	pid     int
	started time.Time
}

func (a agentProcess) String() string {
	return fmt.Sprintf("%s:%d:%d", a.name, a.pid, a.started.UnixNano())
}

func parseAgentProcess(s string) (agentProcess, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return agentProcess{}, false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return agentProcess{}, false
	}
	started, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return agentProcess{}, false
	}
	return agentProcess{name: parts[0], pid: pid, started: time.Unix(0, started)}, true
}

// agentProcessNames returns the program names of every known agent, as
// the kernel reports them.
func agentProcessNames(cfg *config.Config) []string {
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, cfg)
	var names []string
	for _, adapter := range registry.All() {
		if namer, ok := adapter.(agent.ProcessNamer); ok {
			names = append(names, namer.ProcessNames()...)
		}
	}
	return names
}

// agentAncestor walks the parents of the peer and returns the nearest
// one that runs a known agent as the account of the peer. An agent of
// another account above the peer is not the agent of this account's
// sessions. The walk reads what the kernel reports now, so it runs only
// while the peer is the process seen at accept.
func (s *Server) agentAncestor(peer *peercred.Peer) (agentProcess, bool) {
	if peer == nil || peer.PID <= 0 || !peer.SameProcess() {
		return agentProcess{}, false
	}
	chain, err := s.ancestors(int(peer.PID))
	if err != nil {
		log.Debugf("supervisor: parents of pid %d: %v", peer.PID, err)
	}
	for _, p := range chain {
		if p.UID >= 0 && uint32(p.UID) != peer.UID {
			continue
		}
		for _, name := range s.agentNames {
			if p.Matches(name) {
				return agentProcess{name: p.Name, pid: p.PID, started: p.Started}, true
			}
		}
	}
	return agentProcess{}, false
}

// trustFor decides the trust of a hook of the session from the agent
// above the hook and the agent the session is bound to. The caller holds
// the write lock. It returns the trust and the binding to store when the
// session has none yet.
func (p *partition) trustFor(ctx context.Context, sessionID uuid.UUID, ancestor agentProcess, found bool) (trust string, bind string) {
	sess, err := p.rt.Store.GetSession(ctx, sessionID)
	if err != nil {
		log.Warnf("supervisor: session %s for the trust check: %v", sessionID, err)
	}
	bound := ""
	if sess != nil {
		bound = sess.AgentProcess
	}
	if bound != "" {
		// A session whose agent is gone binds again on the next agent:
		// an agent that resumes a session is a new process. A forger
		// under a live agent stays low.
		if b, ok := parseAgentProcess(bound); ok && !p.running(b) {
			bound = ""
		}
	}
	switch {
	case found && bound == "":
		return approval.PeerTrustAgent, ancestor.String()
	case found && bound == ancestor.String():
		return approval.PeerTrustAgent, ""
	case found:
		return approval.PeerTrustLow, ""
	case bound != "":
		return approval.PeerTrustLow, ""
	}
	return approval.PeerTrustUnknown, ""
}

// running reports whether the bound agent process still lives, by its
// pid and start time.
func (p *partition) running(a agentProcess) bool {
	for _, proc := range p.processes(a.pid) {
		if proc.PID == a.pid && proc.Started.Equal(a.started) {
			return true
		}
	}
	return false
}

// bindSession stores the agent the session is bound to. The session row
// exists after the service handled the hook. trustFor gives a binding
// only when the session has none, or when the agent it had is gone.
func (p *partition) bindSession(ctx context.Context, sessionID uuid.UUID, bind string) {
	if bind == "" {
		return
	}
	sess, err := p.rt.Store.GetSession(ctx, sessionID)
	if err != nil || sess == nil {
		return
	}
	if sess.AgentProcess == bind {
		return
	}
	sess.AgentProcess = bind
	if err := p.rt.Store.UpdateSession(ctx, sess); err != nil {
		log.Warnf("supervisor: bind session %s to %s: %v", sessionID, bind, err)
	}
}

// processes returns the process with pid, when it runs. The default reads
// the start time the kernel reports.
func (p *partition) processes(pid int) []procs.Process {
	if p.lookup != nil {
		return p.lookup(pid)
	}
	chain, err := procs.Ancestors(pid)
	if err != nil && len(chain) == 0 {
		// The walk starts above pid. A pid that has no stat file is gone.
		return nil
	}
	started, err := procs.StartedAt(pid)
	if err != nil {
		return nil
	}
	return []procs.Process{{PID: pid, Started: started}}
}

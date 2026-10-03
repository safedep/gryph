package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/hookside"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/spool"
)

// decisionMargin is what the client keeps back from the agent's hook
// timeout, so the client answers before the agent gives up and lets the
// action through.
const decisionMargin = 500 * time.Millisecond

// unavailableMessage is the one stderr line of a block that the absent
// service caused.
const unavailableMessage = "Gryph supervisor is not running. Run `gryph doctor`."

// clientMode reports whether the hook talks to the decision service
// instead of deciding in process. Only a managed configuration turns it on.
// A socket alone never changes the mode, and a user configuration cannot
// point the hook at a service of its own.
func clientMode(cfg *config.Config) bool {
	return config.ManagedConfigActive() && cfg.Supervisor.Enabled
}

// runClientHook is the hook in client mode. It opens no store and reads no
// key. It parses the payload for the claims only the agent user can make,
// sends the raw payload to the service, and renders the answer. Every
// failure maps to the verdict the fail-mode column of the hook names, so
// the client never returns an exit code that is not a block and never
// hangs past the agent's timeout.
func runClientHook(ctx context.Context, cfg *config.Config, agentName, hookType string, rawData []byte) error {
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, cfg)
	adapter, ok := registry.Get(agentName)
	if !ok {
		// Without an adapter nothing can render a response. The exit code
		// that every agent reads as a block is the safe answer.
		return &exitError{code: 2, message: "gryph: unknown agent: " + agentName}
	}
	spec, _ := registry.HookSpec(agentName, events.HookType(hookType))
	column := string(spec.FailColumn())
	if !ok {
		column = string(events.FailColumnBlocking)
	}
	fail := func(reason string, verdict string) error {
		return failClosed(cfg, adapter, agentName, hookType, column, verdict, reason, rawData)
	}

	event, err := adapter.ParseEvent(ctx, hookType, rawData)
	if err != nil {
		hookErrorToService(ctx, cfg, &decision.HookError{Agent: agentName, HookType: hookType, RawSize: len(rawData), RawEvent: rawData, Message: err.Error()})
		return fail("payload does not parse: "+err.Error(), cfg.Supervisor.EffectiveUnavailable(column))
	}
	handle := ipc.Handle{Agent: agentName, HookType: hookType, RawPayload: rawData, Project: hookside.ClaimProject(event.WorkingDirectory)}
	if event.ActionType == events.ActionSessionEnd {
		handle.Cost = hookside.CollectCost(ctx, event.AgentName, event.TranscriptPath, event.SessionID)
	}

	socket := cfg.Supervisor.SocketPath()
	client, err := ipc.Dial(ctx, socket, ipc.DialOptions{Version: version.Version, VerifyServer: func(conn net.Conn) error {
		return hookside.VerifyServer(conn, socket, cfg.Supervisor.ServerAccount())
	}})
	switch {
	case errors.Is(err, ipc.ErrConnect):
		if cfg.Supervisor.EffectiveProfile() == config.SupervisorProfilePilot && column == string(events.FailColumnBlocking) {
			return localEphemeral(ctx, cfg, adapter, registry, event, hookType, err.Error(), rawData)
		}
		return fail(err.Error(), cfg.Supervisor.EffectiveUnavailable(column))
	case errors.Is(err, ipc.ErrServerIdentity):
		// A socket that is not the system's blocks in every mode and never
		// triggers the fallback. The spool carries the finding to the
		// service, as a tamper event of this account.
		spoolEntry(cfg, spool.Entry{Kind: spool.KindTamper, Verdict: config.UnavailableBlock, Reason: err.Error()})
		return sendResponse(adapter, hookType, agent.DecisionBlock, fmt.Sprintf("Gryph refused the decision service at %s: %v. Run `gryph doctor`.", socket, err))
	case err != nil:
		// The service answered, so it runs, and still refused the
		// handshake. The phase rule applies.
		return fail(err.Error(), phaseVerdict(column))
	}
	defer func() { _ = client.Close() }()

	budget := ipc.DecisionTimeout
	if spec.Timeout > decisionMargin && spec.Timeout-decisionMargin < budget {
		budget = spec.Timeout - decisionMargin
	}
	dctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	d, err := client.Handle(dctx, handle)
	if err != nil {
		return fail(err.Error(), phaseVerdict(column))
	}
	hookDecision, detail := renderDecision(d.Response())
	return sendResponse(adapter, hookType, hookDecision, detail)
}

// phaseVerdict is the verdict after a completed handshake: a deadline, a
// rate limit or a server error. A blocking hook blocks, because the
// service runs and a same-user flood must not turn into allows. A prompt
// or lifecycle hook allows, because a block there stops the user.
func phaseVerdict(column string) string {
	if column == string(events.FailColumnBlocking) {
		return config.UnavailableBlock
	}
	return config.UnavailableAllow
}

// failClosed renders the verdict of a failure and spools what the service
// did not see. An allow leaves the frame in the spool so the service
// records the action later, marked as spooled. A block tells the user in
// one stderr line.
func failClosed(cfg *config.Config, adapter agent.Adapter, agentName, hookType, column, verdict, reason string, rawData []byte) error {
	frame := ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: agentName, HookType: hookType, RawPayload: rawData})
	spoolEntry(cfg, spool.Entry{Verdict: verdict, Reason: reason, Frame: frame})
	if verdict == config.UnavailableAllow {
		log.Debugf("hook: %s allowed without the decision service (%s): %s", hookType, column, reason)
		return sendResponse(adapter, hookType, agent.DecisionAllow, "")
	}
	return sendResponse(adapter, hookType, agent.DecisionBlock, unavailableMessage)
}

// localEphemeral is the fallback of a blocking hook in the pilot profile:
// the client evaluates the managed policy and the built-in rules itself,
// with no store, no context and no signature, and leaves the decision in
// the spool marked degraded. A rule that reads the session context blocks,
// because the client has no context to read. The policy that does not
// load blocks too: the fallback never widens what the service would do.
func localEphemeral(ctx context.Context, cfg *config.Config, adapter agent.Adapter, registry *agent.Registry, event *events.Event, hookType, cause string, rawData []byte) error {
	frame := ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: event.AgentName, HookType: hookType, RawPayload: rawData, Project: hookside.ClaimProject(event.WorkingDirectory)})
	evaluator, err := engine.NewEphemeralEvaluator(cfg)
	if err != nil {
		reason := "local-ephemeral: policy did not load: " + err.Error()
		spoolEntry(cfg, spool.Entry{Kind: spool.KindDegraded, Verdict: config.UnavailableBlock, Reason: reason, Frame: frame})
		return sendResponse(adapter, hookType, agent.DecisionBlock, "Gryph supervisor is not running and the managed policy did not load: "+err.Error()+". Run `gryph doctor`.")
	}
	if spec, ok := registry.HookSpec(event.AgentName, events.HookType(hookType)); ok {
		event.Phase = spec.Phase
	}
	sess := session.NewSessionWithID(event.SessionID, event.AgentName)
	result := evaluator.Evaluate(ctx, event, sess)
	if !result.IsAllowed() {
		reason := "local-ephemeral: " + result.StoredBlockReason
		spoolEntry(cfg, spool.Entry{Kind: spool.KindDegraded, Verdict: config.UnavailableBlock, Reason: reason, Frame: frame})
		return sendResponse(adapter, hookType, agent.DecisionBlock, result.BlockReason)
	}
	spoolEntry(cfg, spool.Entry{Kind: spool.KindDegraded, Verdict: config.UnavailableAllow, Reason: "local-ephemeral: " + cause, Frame: frame})
	log.Debugf("hook: %s allowed by the local-ephemeral evaluation: %s", hookType, cause)
	return sendResponse(adapter, hookType, agent.DecisionAllow, "")
}

// spoolEntry writes the entry under the account's spool directory. A
// spool that is not there is not an error for the hook: the verdict holds
// either way, and doctor reports the missing spool.
func spoolEntry(cfg *config.Config, entry spool.Entry) {
	id, err := account.CurrentID()
	if err != nil {
		log.Warnf("hook: spool entry not written: %v", err)
		return
	}
	if _, err := spool.Write(cfg.Supervisor.SpoolPath(), id, entry); err != nil {
		log.Warnf("hook: spool entry not written: %v", err)
	}
}

// hookErrorToService reports a failure before the decision to the service
// when it is reachable, so the audit trail shows the hook that decided
// nothing. It is best effort and bounded by the handshake budget.
func hookErrorToService(ctx context.Context, cfg *config.Config, report *decision.HookError) {
	socket := cfg.Supervisor.SocketPath()
	client, err := ipc.Dial(ctx, socket, ipc.DialOptions{Version: version.Version, VerifyServer: func(conn net.Conn) error {
		return hookside.VerifyServer(conn, socket, cfg.Supervisor.ServerAccount())
	}})
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	if err := client.ReportHookError(ctx, ipc.ReportHookError(*report)); err != nil {
		log.Debugf("hook: error report not taken: %v", err)
	}
}

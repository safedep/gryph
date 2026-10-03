package supervisor

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/nofollow"
	"github.com/safedep/gryph/platform/procs"
	"github.com/safedep/gryph/selfprotect"
)

// rateLimitEventInterval bounds how often one account's flood becomes a
// tamper event. The first refusal in the interval is recorded, the rest
// only refused.
const rateLimitEventInterval = time.Minute

// partition is the state of one account: its store, its decision service
// and its limits. The peer uid names it. One mutex serializes the writes,
// so the receipt chain and the context state of the account stay in
// order without a lock on the database.
type partition struct {
	uid      uint32
	dir      string
	rt       *engine.Runtime
	service  decision.Service
	spoolRec *engine.SpoolRecorder
	bucket   *bucket

	// write serializes Handle and ReportHookError: the one writer of the
	// partition.
	write sync.Mutex

	connMu  sync.Mutex
	conns   int
	retired bool

	eventMu       sync.Mutex
	lastRateEvent time.Time

	// lookup returns the process with a pid when it runs. Nil takes the
	// platform. A test sets it.
	lookup func(pid int) []procs.Process
}

// openPartition opens or creates the partition of uid under root. The
// directory is the service account's and mode 0700, so no agent user reads
// another account's state. Every step runs relative to the open root and
// refuses a link, so a link planted under the state directory cannot send
// the partition elsewhere.
func openPartition(ctx context.Context, cfg *config.Config, root *nofollow.Dir, uid uint32, limits Limits) (*partition, error) {
	users, err := childDir(root, "users")
	if err != nil {
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	defer func() { _ = users.Close() }()
	home, err := childDir(users, strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	dir := home.Path()
	_ = home.Close()
	rt, err := engine.NewPartition(ctx, cfg, dir)
	if err != nil {
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	spoolRec, err := rt.SpoolRecorder()
	if err != nil {
		_ = rt.Close()
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	p := &partition{
		uid:      uid,
		dir:      dir,
		rt:       rt,
		service:  rt.DecisionService(),
		spoolRec: spoolRec,
		bucket:   newBucket(limits.Rate, limits.Burst, time.Now()),
	}
	// The service answers escalations itself, with the request store of
	// the partition, in place of the prompt or the nop of a user install.
	rt.AddMediatorOptions(aarmsec.WithApprovalService(newApprover(p, cfg.EffectivePolicy().Approval)))
	return p, nil
}

// childDir creates name inside parent when it is missing and opens it on
// the parent's handle.
func childDir(parent *nofollow.Dir, name string) (*nofollow.Dir, error) {
	if err := parent.Mkdir(name, 0o700); err != nil {
		return nil, err
	}
	return parent.OpenDir(name)
}

func (p *partition) close() error {
	return p.rt.Close()
}

// acquireConn counts a connection in. It reports false above the limit.
func (p *partition) acquireConn(limit int) bool {
	p.connMu.Lock()
	defer p.connMu.Unlock()
	if p.conns >= limit {
		return false
	}
	p.conns++
	return true
}

// releaseConn counts a connection out. A retired partition closes with
// its last connection.
func (p *partition) releaseConn() {
	p.connMu.Lock()
	p.conns--
	last := p.retired && p.conns == 0
	p.connMu.Unlock()
	if last {
		if err := p.close(); err != nil {
			log.Warnf("supervisor: close the retired partition of uid %d: %v", p.uid, err)
		}
	}
}

// retire takes the partition out of service: the server opens a new one
// for the account on the next contact, and this one closes when its last
// connection ends. It reports whether it closed now.
func (p *partition) retire() bool {
	p.connMu.Lock()
	defer p.connMu.Unlock()
	p.retired = true
	return p.conns == 0
}

// handle answers one Handle frame. The service parses the raw payload with
// its own registry, so the agent name and the hook type are claims until
// the adapter accepts the payload. The project claim and the cost totals
// stay claims and reach the store marked as such.
func (p *partition) handle(ctx context.Context, h *ipc.Handle) (*ipc.Frame, error) {
	if !p.bucket.take(time.Now()) {
		p.recordRateLimit(ctx, "handle")
		return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
	}
	adapter, ok := p.rt.Registry.Get(h.Agent)
	if !ok {
		return ipc.ErrorFrame(ipc.CodeInvalid, "unknown agent: "+h.Agent), nil
	}
	event, err := adapter.ParseEvent(ctx, h.HookType, h.RawPayload)
	if err != nil {
		return ipc.ErrorFrame(ipc.CodeInvalid, "payload does not parse: "+err.Error()), nil
	}
	req := decision.NewHookRequest(event)
	req.Project = h.Project
	req.Cost = h.Cost

	now := time.Now().UTC()
	p.write.Lock()
	p.expireRequests(ctx, now)
	var bind string
	if pr := prompterFrom(ctx); pr != nil {
		req.PeerTrust, bind = p.trustFor(ctx, event.SessionID, pr.ancestor, pr.found)
	}
	resp, err := p.service.Handle(ctx, req)
	var lines []string
	if err == nil {
		p.bindSession(ctx, event.SessionID, bind)
		lines = append(p.collectionNotice(ctx), p.notices(ctx, event.SessionID, now)...)
	}
	p.write.Unlock()
	if err != nil {
		return nil, err
	}
	d := ipc.Decision(*resp)
	withNotices(&d, lines)
	return ipc.NewFrame(ipc.TypeDecision, d)
}

// reportHookError records a hook invocation that produced no decision.
func (p *partition) reportHookError(ctx context.Context, r *ipc.ReportHookError) error {
	if !p.bucket.take(time.Now()) {
		p.recordRateLimit(ctx, "report_hook_error")
		return errRateLimited
	}
	p.write.Lock()
	defer p.write.Unlock()
	return p.service.ReportHookError(ctx, r.HookError())
}

// recordRateLimit writes one tamper event to the system session of the
// account, at most once per interval, so the flood itself cannot flood
// the store.
func (p *partition) recordRateLimit(ctx context.Context, what string) {
	p.eventMu.Lock()
	if time.Since(p.lastRateEvent) < rateLimitEventInterval {
		p.eventMu.Unlock()
		return
	}
	p.lastRateEvent = time.Now()
	p.eventMu.Unlock()

	p.recordTamper(ctx, events.TamperPayload{
		Operation: events.TamperRateLimited,
		Asset:     string(selfprotect.AssetSupervisor),
		Provider:  ProviderName,
		Detail:    "the account sent more " + what + " requests than the limit allows",
	})
}

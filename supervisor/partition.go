package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
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
	uid     uint32
	dir     string
	rt      *engine.Runtime
	service decision.Service
	bucket  *bucket

	// write serializes Handle and ReportHookError: the one writer of the
	// partition.
	write sync.Mutex

	connMu sync.Mutex
	conns  int

	eventMu       sync.Mutex
	lastRateEvent time.Time
}

// openPartition opens or creates the partition of uid under root. The
// directory is the service account's and mode 0700, so no agent user reads
// another account's state.
func openPartition(ctx context.Context, cfg *config.Config, root string, uid uint32, limits Limits) (*partition, error) {
	dir := filepath.Join(root, "users", strconv.FormatUint(uint64(uid), 10))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	rt, err := engine.NewPartition(ctx, cfg, dir)
	if err != nil {
		return nil, fmt.Errorf("partition %d: %w", uid, err)
	}
	return &partition{
		uid:     uid,
		dir:     dir,
		rt:      rt,
		service: rt.DecisionService(),
		bucket:  newBucket(limits.Rate, limits.Burst, time.Now()),
	}, nil
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

func (p *partition) releaseConn() {
	p.connMu.Lock()
	defer p.connMu.Unlock()
	p.conns--
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

	p.write.Lock()
	resp, err := p.service.Handle(ctx, req)
	p.write.Unlock()
	if err != nil {
		return nil, err
	}
	return ipc.NewFrame(ipc.TypeDecision, ipc.Decision(*resp))
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

	recorder, err := p.rt.TamperRecorderFor(strconv.FormatUint(uint64(p.uid), 10))
	if err != nil {
		log.Warnf("supervisor: rate limit event for uid %d not recorded: %v", p.uid, err)
		return
	}
	p.write.Lock()
	defer p.write.Unlock()
	_, err = recorder.Record(ctx, events.TamperPayload{
		Operation:   events.TamperRateLimited,
		Asset:       string(selfprotect.AssetSupervisor),
		LevelBefore: selfprotect.LevelDetect.String(),
		LevelAfter:  selfprotect.LevelDetect.String(),
		Provider:    ProviderName,
		Detail:      "the account sent more " + what + " requests than the limit allows",
	})
	if err != nil {
		log.Warnf("supervisor: rate limit event for uid %d not recorded: %v", p.uid, err)
	}
}

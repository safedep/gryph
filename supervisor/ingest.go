package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/platform/nofollow"
	"github.com/safedep/gryph/selfprotect"
	"github.com/safedep/gryph/spool"
)

// DefaultIngestInterval is how often the service reads the spool.
const DefaultIngestInterval = time.Minute

// maxReportedNames bounds the names a spool_refused event lists.
const maxReportedNames = 5

// ingestLoop reads the spool once at start and then every interval, until
// ctx ends.
func (s *Server) ingestLoop(ctx context.Context) {
	ticker := time.NewTicker(s.ingestInterval)
	defer ticker.Stop()
	for {
		if err := s.IngestOnce(ctx); err != nil && ctx.Err() == nil {
			log.Warnf("supervisor: spool pass: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// IngestOnce runs one pass over the spool. Every entry goes to the
// partition of the owner of its directory. A spool that does not exist is
// not an error: no client has written yet.
func (s *Server) IngestOnce(ctx context.Context) error {
	root, err := nofollow.OpenDir(s.spoolDir, ".")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	sink := &spoolSink{server: s, ctx: ctx, degraded: map[uint32]int{}}
	err = spool.Ingest(ctx, root, s.spoolLimits, sink)
	sink.flush()
	return err
}

// spoolSink hands the entries of one pass to the partitions and counts
// the entries that a client decided alone while the service was up.
type spoolSink struct {
	server   *Server
	ctx      context.Context
	degraded map[uint32]int
}

func (k *spoolSink) Entry(ctx context.Context, uid uint32, entry spool.Entry) error {
	part, err := k.server.partition(ctx, uid)
	if err != nil {
		return err
	}
	if entry.Kind == "" && entry.RecordedAt.After(k.server.started) {
		k.degraded[uid]++
	}
	return part.recordSpooled(ctx, entry)
}

func (k *spoolSink) Report(ctx context.Context, uid uint32, report spool.Report) {
	part, err := k.server.partition(ctx, uid)
	if err != nil {
		log.Warnf("supervisor: spool report for uid %d not recorded: %v", uid, err)
		return
	}
	part.recordTamper(ctx, events.TamperPayload{
		Operation: events.TamperSpoolRefused,
		Asset:     string(selfprotect.AssetSupervisor),
		Provider:  ProviderName,
		Detail:    describeReport(report),
	})
}

// flush records one degraded event per account that decided alone while
// the service was running. That pattern means the client could not reach
// a running service, which a same-user flood causes on purpose.
func (k *spoolSink) flush() {
	for uid, n := range k.degraded {
		part, err := k.server.partition(k.ctx, uid)
		if err != nil {
			log.Warnf("supervisor: degraded event for uid %d not recorded: %v", uid, err)
			continue
		}
		part.recordTamper(k.ctx, events.TamperPayload{
			Operation: events.TamperDegraded,
			Asset:     string(selfprotect.AssetSupervisor),
			Provider:  ProviderName,
			Detail:    strconv.Itoa(n) + " hook call(s) decided without the service while it was running",
		})
	}
}

func describeReport(report spool.Report) string {
	var parts []string
	if n := len(report.Refused); n > 0 {
		names := make([]string, 0, maxReportedNames)
		for i, r := range report.Refused {
			if i == maxReportedNames {
				names = append(names, "...")
				break
			}
			names = append(names, r.Name+" ("+r.Reason+")")
		}
		parts = append(parts, fmt.Sprintf("%d file(s) refused: %s", n, strings.Join(names, ", ")))
	}
	if report.Dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d entry(ies) dropped over the quota", report.Dropped))
	}
	return strings.Join(parts, "; ")
}

// recordSpooled records one spool entry in the partition: a tamper event
// the client found, or an action the client decided without the service.
func (p *partition) recordSpooled(ctx context.Context, entry spool.Entry) error {
	if entry.Kind == spool.KindTamper {
		p.recordTamper(ctx, events.TamperPayload{
			Operation: events.TamperServerIdentity,
			Asset:     string(selfprotect.AssetSupervisor),
			Provider:  "hook",
			Detail:    entry.Reason,
		})
		return nil
	}
	if entry.Frame == nil {
		log.Warnf("supervisor: spool entry of uid %d holds no frame: %s", p.uid, entry.Reason)
		return nil
	}
	body, err := ipc.Decode(entry.Frame)
	if err != nil {
		return err
	}
	h, ok := body.(*ipc.Handle)
	if !ok {
		log.Warnf("supervisor: spool entry of uid %d holds a %s frame, not handle", p.uid, entry.Frame.Type)
		return nil
	}
	adapter, ok := p.rt.Registry.Get(h.Agent)
	if !ok {
		log.Warnf("supervisor: spool entry of uid %d names an unknown agent %q", p.uid, h.Agent)
		return nil
	}
	event, err := adapter.ParseEvent(ctx, h.HookType, h.RawPayload)
	if err != nil {
		log.Warnf("supervisor: spool entry of uid %d does not parse: %v", p.uid, err)
		return nil
	}
	req := decision.NewHookRequest(event)
	req.Project = h.Project
	req.Cost = h.Cost

	p.write.Lock()
	defer p.write.Unlock()
	return p.spoolRec.Record(ctx, req, decision.Verdict(entry.Verdict), entry.Reason)
}

// recordTamper writes one tamper event to the system session of the
// account.
func (p *partition) recordTamper(ctx context.Context, payload events.TamperPayload) {
	if payload.LevelBefore == "" {
		payload.LevelBefore = selfprotect.LevelDetect.String()
		payload.LevelAfter = selfprotect.LevelDetect.String()
	}
	recorder, err := p.rt.TamperRecorderFor(strconv.FormatUint(uint64(p.uid), 10))
	if err != nil {
		log.Warnf("supervisor: %s event for uid %d not recorded: %v", payload.Operation, p.uid, err)
		return
	}
	p.write.Lock()
	defer p.write.Unlock()
	if _, err := recorder.Record(ctx, payload); err != nil {
		log.Warnf("supervisor: %s event for uid %d not recorded: %v", payload.Operation, p.uid, err)
	}
}

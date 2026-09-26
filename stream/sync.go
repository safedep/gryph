package stream

import (
	"context"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	corestream "github.com/safedep/gryph/core/stream"
	"github.com/safedep/gryph/storage"
)

const defaultBatchSize = 500

// SyncResult holds the overall result of a sync operation.
type SyncResult struct {
	TargetResults []TargetSyncResult
}

// TargetSyncResult holds the result for a single target.
type TargetSyncResult struct {
	TargetName string
	EventsSent int
	AuditsSent int
	Error      error
}

// SyncProgress reports progress during sync.
type SyncProgress struct {
	TargetName string
	EventsSent int
	AuditsSent int
	IsComplete bool
}

// SyncOption configures sync behavior.
type SyncOption func(*syncOptions)

type syncOptions struct {
	onProgress func(SyncProgress)
	batchSize  int
	iterations int // 0 = unlimited (drain all)
}

// WithProgressCallback sets a callback for progress updates.
func WithProgressCallback(fn func(SyncProgress)) SyncOption {
	return func(o *syncOptions) {
		o.onProgress = fn
	}
}

// WithBatchSize sets the number of events and audits to fetch per iteration.
// A value of 0 uses the Syncer's default batch size.
func WithBatchSize(size int) SyncOption {
	return func(o *syncOptions) {
		o.batchSize = size
	}
}

// WithIterations sets the maximum number of batch iterations.
// A value of 0 means unlimited (drain all pending items).
func WithIterations(n int) SyncOption {
	return func(o *syncOptions) {
		o.iterations = n
	}
}

// Syncer orchestrates syncing events and self-audits to stream targets.
type Syncer struct {
	store       storage.Store
	registry    *Registry
	batchSize   int
	profiles    map[string]privacy.ExportProfile
	profileErrs map[string]error
}

// NewSyncer creates a new Syncer.
func NewSyncer(store storage.Store, registry *Registry) *Syncer {
	return &Syncer{
		store:     store,
		registry:  registry,
		batchSize: defaultBatchSize,
	}
}

// SetProfile sets the export profile of a target. A target with no profile
// gets the built-in default profile, so a target never receives an
// unprojected event.
func (s *Syncer) SetProfile(target string, p privacy.ExportProfile) {
	if s.profiles == nil {
		s.profiles = map[string]privacy.ExportProfile{}
	}
	s.profiles[target] = p
}

// SetProfileError marks the export profile of a target as invalid. The
// target then gets no item and reports err, so it never falls back to a
// weaker profile.
func (s *Syncer) SetProfileError(target string, err error) {
	if s.profileErrs == nil {
		s.profileErrs = map[string]error{}
	}
	s.profileErrs[target] = err
}

func (s *Syncer) profile(target string) (privacy.ExportProfile, error) {
	if err, ok := s.profileErrs[target]; ok {
		return privacy.ExportProfile{}, err
	}
	if p, ok := s.profiles[target]; ok {
		return p, nil
	}
	return privacy.BuiltinProfiles()[privacy.ProfileDefault], nil
}

// Sync sends unsent events and self-audits to all enabled targets.
func (s *Syncer) Sync(ctx context.Context, opts ...SyncOption) (*SyncResult, error) {
	var options syncOptions
	for _, opt := range opts {
		opt(&options)
	}

	batchSize := options.batchSize
	if batchSize <= 0 {
		batchSize = s.batchSize
	}

	targets := s.registry.Enabled()
	result := &SyncResult{
		TargetResults: make([]TargetSyncResult, 0, len(targets)),
	}

	for _, target := range targets {
		tr := s.syncTarget(ctx, target, batchSize, options.iterations, options.onProgress)
		result.TargetResults = append(result.TargetResults, tr)
	}

	return result, nil
}

func (s *Syncer) syncTarget(ctx context.Context, target corestream.Target, batchSize, maxIterations int, onProgress func(SyncProgress)) TargetSyncResult {
	tr := TargetSyncResult{TargetName: target.Name()}
	profile, err := s.profile(target.Name())
	if err != nil {
		tr.Error = err
		return tr
	}

	reportProgress := func(complete bool) {
		if onProgress != nil {
			onProgress(SyncProgress{
				TargetName: target.Name(),
				EventsSent: tr.EventsSent,
				AuditsSent: tr.AuditsSent,
				IsComplete: complete,
			})
		}
	}

	reportProgress(false)

	eventCursor, err := s.store.GetEventCursor(ctx, target.Name())
	if err != nil {
		tr.Error = err
		return tr
	}
	auditCursor, err := s.store.GetAuditCursor(ctx, target.Name())
	if err != nil {
		tr.Error = err
		return tr
	}

	var eventAfter, auditAfter time.Time
	var lastEventID, lastAuditID uuid.UUID
	if eventCursor != nil {
		eventAfter = eventCursor.LastSyncedAt
		lastEventID, _ = uuid.Parse(eventCursor.LastID)
	}
	if auditCursor != nil {
		auditAfter = auditCursor.LastSyncedAt
		lastAuditID, _ = uuid.Parse(auditCursor.LastID)
	}

	eventsDrained := false
	auditsDrained := false
	iteration := 0
	for maxIterations <= 0 || iteration < maxIterations {
		var evts []*events.Event
		if !eventsDrained {
			evts, err = s.store.QueryEventsAfter(ctx, eventAfter, lastEventID, batchSize)
			if err != nil {
				tr.Error = err
				return tr
			}
			if len(evts) < batchSize {
				eventsDrained = true
			}
		}

		var audits []*storage.SelfAuditEntry
		if !auditsDrained {
			audits, err = s.store.QuerySelfAuditsAfter(ctx, auditAfter, lastAuditID, batchSize)
			if err != nil {
				tr.Error = err
				return tr
			}
			if len(audits) < batchSize {
				auditsDrained = true
			}
		}

		if len(evts) == 0 && len(audits) == 0 {
			break
		}

		items := make([]corestream.StreamItem, 0, len(evts)+len(audits))

		for _, e := range evts {
			items = append(items, corestream.StreamItem{Event: e.ForExport(profile)})
			eventAfter = e.Timestamp
			lastEventID = e.ID
		}

		for _, a := range audits {
			items = append(items, corestream.StreamItem{SelfAudit: selfAuditForExport(a, profile)})
			auditAfter = a.Timestamp
			lastAuditID = a.ID
		}

		if err := target.Send(ctx, items); err != nil {
			tr.Error = err
			return tr
		}

		tr.EventsSent += len(evts)
		tr.AuditsSent += len(audits)

		if len(evts) > 0 {
			if err := s.store.SaveEventCursor(ctx, &storage.StreamCursor{
				TargetName:   target.Name(),
				LastSyncedAt: eventAfter,
				LastID:       lastEventID.String(),
			}); err != nil {
				tr.Error = err
				return tr
			}
		}

		if len(audits) > 0 {
			if err := s.store.SaveAuditCursor(ctx, &storage.StreamCursor{
				TargetName:   target.Name(),
				LastSyncedAt: auditAfter,
				LastID:       lastAuditID.String(),
			}); err != nil {
				tr.Error = err
				return tr
			}
		}

		iteration++
		reportProgress(false)

		if eventsDrained && auditsDrained {
			break
		}
	}

	reportProgress(true)
	return tr
}

// selfAuditForExport returns a copy of the audit entry for a target. A hook
// error stores the hook input under details.raw_event, and its error can
// quote that input. Neither has a content label, so the copy keeps them only
// when the profile includes every value.
func selfAuditForExport(a *storage.SelfAuditEntry, p privacy.ExportProfile) *storage.SelfAuditEntry {
	out := *a
	if _, ok := a.Details["raw_event"]; !ok || p.IncludesAll() {
		return &out
	}
	out.Details = maps.Clone(a.Details)
	delete(out.Details, "raw_event")
	out.ErrorMessage = ""
	return &out
}

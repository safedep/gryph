package stream

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	store     storage.Store
	registry  *Registry
	batchSize int
}

// NewSyncer creates a new Syncer.
func NewSyncer(store storage.Store, registry *Registry) *Syncer {
	return &Syncer{
		store:     store,
		registry:  registry,
		batchSize: defaultBatchSize,
	}
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

	cp, err := s.store.GetStreamCheckpoint(ctx, target.Name())
	if err != nil {
		tr.Error = err
		return tr
	}

	var after time.Time
	var lastEventID, lastAuditID uuid.UUID
	if cp != nil {
		after = cp.LastSyncedAt
		lastEventID, _ = uuid.Parse(cp.LastEventID)
		lastAuditID, _ = uuid.Parse(cp.LastSelfAuditID)
	}

	iteration := 0
	for maxIterations <= 0 || iteration < maxIterations {

		evts, err := s.store.QueryEventsAfter(ctx, after, lastEventID, batchSize)
		if err != nil {
			tr.Error = err
			return tr
		}

		audits, err := s.store.QuerySelfAuditsAfter(ctx, after, lastAuditID, batchSize)
		if err != nil {
			tr.Error = err
			return tr
		}

		if len(evts) == 0 && len(audits) == 0 {
			break
		}

		items := make([]corestream.StreamItem, 0, len(evts)+len(audits))
		var latestTime time.Time

		for _, e := range evts {
			items = append(items, corestream.StreamItem{Event: e})
			if e.Timestamp.After(latestTime) {
				latestTime = e.Timestamp
			}
			lastEventID = e.ID
		}

		for _, a := range audits {
			items = append(items, corestream.StreamItem{SelfAudit: a})
			if a.Timestamp.After(latestTime) {
				latestTime = a.Timestamp
			}
			lastAuditID = a.ID
		}

		if err := target.Send(ctx, items); err != nil {
			tr.Error = err
			return tr
		}

		tr.EventsSent += len(evts)
		tr.AuditsSent += len(audits)

		newCP := &storage.StreamCheckpoint{
			TargetName:      target.Name(),
			LastSyncedAt:    latestTime,
			LastEventID:     lastEventID.String(),
			LastSelfAuditID: lastAuditID.String(),
		}

		if err := s.store.SaveStreamCheckpoint(ctx, newCP); err != nil {
			tr.Error = err
			return tr
		}

		after = latestTime
		iteration++
		reportProgress(false)

		if len(evts) < batchSize && len(audits) < batchSize {
			break
		}
	}

	reportProgress(true)
	return tr
}

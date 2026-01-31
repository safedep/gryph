package stream

import (
	"context"
	"time"

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
func (s *Syncer) Sync(ctx context.Context) (*SyncResult, error) {
	targets := s.registry.Enabled()
	result := &SyncResult{
		TargetResults: make([]TargetSyncResult, 0, len(targets)),
	}

	for _, target := range targets {
		tr := s.syncTarget(ctx, target)
		result.TargetResults = append(result.TargetResults, tr)
	}

	return result, nil
}

func (s *Syncer) syncTarget(ctx context.Context, target corestream.Target) TargetSyncResult {
	tr := TargetSyncResult{TargetName: target.Name()}

	cp, err := s.store.GetStreamCheckpoint(ctx, target.Name())
	if err != nil {
		tr.Error = err
		return tr
	}

	var after time.Time
	if cp != nil {
		after = cp.LastSyncedAt
	}

	events, err := s.store.QueryEventsAfter(ctx, after, s.batchSize)
	if err != nil {
		tr.Error = err
		return tr
	}

	audits, err := s.store.QuerySelfAuditsAfter(ctx, after, s.batchSize)
	if err != nil {
		tr.Error = err
		return tr
	}

	if len(events) == 0 && len(audits) == 0 {
		return tr
	}

	items := make([]corestream.StreamItem, 0, len(events)+len(audits))
	var latestTime time.Time
	var lastEventID, lastAuditID string

	for _, e := range events {
		items = append(items, corestream.StreamItem{Event: e})
		if e.Timestamp.After(latestTime) {
			latestTime = e.Timestamp
		}
		lastEventID = e.ID.String()
	}

	for _, a := range audits {
		items = append(items, corestream.StreamItem{SelfAudit: a})
		if a.Timestamp.After(latestTime) {
			latestTime = a.Timestamp
		}
		lastAuditID = a.ID.String()
	}

	if err := target.Send(ctx, items); err != nil {
		tr.Error = err
		return tr
	}

	tr.EventsSent = len(events)
	tr.AuditsSent = len(audits)

	newCP := &storage.StreamCheckpoint{
		TargetName:   target.Name(),
		LastSyncedAt: latestTime,
	}

	if lastEventID != "" {
		newCP.LastEventID = lastEventID
	}

	if lastAuditID != "" {
		newCP.LastSelfAuditID = lastAuditID
	}

	if err := s.store.SaveStreamCheckpoint(ctx, newCP); err != nil {
		tr.Error = err
	}

	return tr
}

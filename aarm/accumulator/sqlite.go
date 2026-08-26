package accumulator

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage"
)

// SQLiteAccumulator persists mediated actions to the storage layer and
// produces ContextSnapshots from the denormalized per-session state row.
// It depends only on storage.ContextStore; the concrete backing store is
// the SQLite-backed ent implementation in production.
type SQLiteAccumulator struct {
	store storage.ContextStore
	now   func() time.Time
}

// NewSQLite returns a new SQLite-backed accumulator. The store must be
// non-nil; callers should fall back to Nop when no store is available.
func NewSQLite(store storage.ContextStore) *SQLiteAccumulator {
	return &SQLiteAccumulator{store: store, now: func() time.Time { return time.Now().UTC() }}
}

var _ Accumulator = (*SQLiteAccumulator)(nil)

// Append translates an Action into a ContextActionRow and persists it.
func (a *SQLiteAccumulator) Append(ctx context.Context, action *model.Action) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("accumulator: store is not initialized")
	}
	if action == nil {
		return fmt.Errorf("accumulator: nil action")
	}
	row := &storage.ContextActionRow{
		ID:                  action.ID,
		SessionID:           action.SessionID,
		EventID:             action.EventID,
		Timestamp:           action.Timestamp,
		ActionType:          string(action.Type),
		Tool:                action.Tool,
		Agent:               action.Agent,
		Project:             action.Project,
		WorkingDir:          action.WorkingDir,
		DataClassifications: action.DataClassifications,
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
		action.ID = row.ID
	}
	if row.Timestamp.IsZero() {
		row.Timestamp = a.now()
	}
	if action.InjectionScore != 0 {
		v := action.InjectionScore
		row.InjectionScore = &v
	}
	return a.store.AppendContextAction(ctx, row)
}

// RecordResult flips the action row's result_status and updates the
// session-level errors counter when the new status is "error".
func (a *SQLiteAccumulator) RecordResult(ctx context.Context, actionID uuid.UUID, result model.Result) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("accumulator: store is not initialized")
	}
	status := string(result.Status)
	if status == "" {
		status = string(model.ResultSuccess)
	}
	return a.store.UpdateContextActionResult(ctx, actionID, status, result.Duration.Milliseconds(), result.Error)
}

// Snapshot returns the current ContextSnapshot for a session. When the
// session has no state row yet, an empty snapshot is returned, matching
// the Nop accumulator's shape.
func (a *SQLiteAccumulator) Snapshot(ctx context.Context, sessionID uuid.UUID) (*model.ContextSnapshot, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("accumulator: store is not initialized")
	}
	state, err := a.store.GetContextState(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return &model.ContextSnapshot{}, nil
	}
	duration := a.now().Sub(state.FirstSeenAt)
	if duration < 0 {
		duration = 0
	}
	return &model.ContextSnapshot{
		TotalActions:        state.TotalActions,
		FilesRead:           state.FilesRead,
		FilesWritten:        state.FilesWritten,
		CommandsExecuted:    state.CommandsExecuted,
		NetworkRequests:     state.NetworkRequests,
		Errors:              state.Errors,
		ToolsUsed:           slices.Clone(state.ToolsUsed),
		SessionDuration:     duration,
		ClassificationsSeen: slices.Clone(state.ClassificationsSeen),
		EntitiesSeen:        slices.Clone(state.EntitiesSeen),
		SemanticDrift:       state.SemanticDrift,
	}, nil
}

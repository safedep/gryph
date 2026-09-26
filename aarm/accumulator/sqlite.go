package accumulator

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
)

// SQLiteAccumulator persists the session context through the storage layer.
// It depends only on storage.ContextStore.
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

// Append writes the entry and its state delta.
func (a *SQLiteAccumulator) Append(ctx context.Context, entry *model.ContextEntry) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("accumulator: store is not initialized")
	}
	if entry == nil {
		return fmt.Errorf("accumulator: nil entry")
	}
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = a.now()
	}
	row := entryRow(entry)
	if err := a.store.AppendContextEntry(ctx, row, stateDelta(entry)); err != nil {
		return err
	}
	entry.Sequence = row.Sequence
	return nil
}

func entryRow(e *model.ContextEntry) *storage.ContextEntryRow {
	row := &storage.ContextEntryRow{
		ID:              e.ID,
		SessionID:       e.SessionID,
		EventID:         e.EventID,
		LinkedEventID:   e.LinkedEventID,
		Kind:            string(entryKind(e)),
		Timestamp:       e.Timestamp,
		ActionType:      string(e.ActionType),
		Tool:            e.Tool,
		ToolCallID:      e.ToolCallID,
		Phase:           string(e.Phase),
		TargetHost:      e.Target.Host,
		TargetMCPServer: e.Target.MCPServer,
		TargetMCPTool:   e.Target.MCPTool,
		Origin:          string(e.Origin),
		Tags:            e.Tags,
		Classifications: privacy.Strings(e.Classifications),
		Decision:        string(e.Decision),
		MatchedRuleIDs:  e.MatchedRuleIDs,
		ContentDigest:   e.ContentDigest,
		ResultStatus:    string(e.Result),
	}
	if e.InjectionScore != 0 {
		v := e.InjectionScore
		row.InjectionScore = &v
	}
	return row
}

// stateDelta computes what the entry adds to the session state. Only an
// action adds to tools_used, as with the counters. An intent becomes the
// latest intent only when it reaches the agent. An escalated intent waits
// for the approval, and ConfirmIntent sets it on approve. A failed
// evaluation has no decision, and fail_mode closed then blocks the prompt.
// So a failed entry never becomes the latest intent. Under fail_mode open
// the prompt runs, and the next intent resets the counters.
func stateDelta(e *model.ContextEntry) *storage.ContextStateDelta {
	delta := &storage.ContextStateDelta{
		Classifications: privacy.Strings(e.Classifications),
		Tags:            e.Tags,
		Intent:          entryKind(e) == events.KindIntent && e.Result != model.ResultError && reachesAgent(e.Decision),
	}
	if entryKind(e) == events.KindAction && e.Tool != "" {
		delta.Tools = []string{e.Tool}
	}
	if origin := originKey(e); origin != "" {
		delta.Origins = []string{origin}
	}
	return delta
}

func reachesAgent(d model.Decision) bool {
	switch d {
	case model.DecisionBlock, model.DecisionDefer, model.DecisionEscalate:
		return false
	default:
		return true
	}
}

// originKey names an origin in origins_seen. An MCP origin carries its
// server, as "mcp:<server>".
func originKey(e *model.ContextEntry) string {
	if e.Origin == privacy.OriginMCP && e.Target.MCPServer != "" {
		return "mcp:" + e.Target.MCPServer
	}
	return string(e.Origin)
}

func entryKind(e *model.ContextEntry) model.EntryKind {
	if e.Kind == "" {
		return events.KindAction
	}
	return e.Kind
}

// RecordResult sets the outcome of an entry.
func (a *SQLiteAccumulator) RecordResult(ctx context.Context, entryID uuid.UUID, result model.Result) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("accumulator: store is not initialized")
	}
	status := string(result.Status)
	if status == "" {
		status = string(model.ResultSuccess)
	}
	return a.store.UpdateContextEntryResult(ctx, entryID, status, result.Duration.Milliseconds(), result.Error)
}

// ConfirmIntent makes an escalated intent the latest intent after an
// approval lets it reach the agent.
func (a *SQLiteAccumulator) ConfirmIntent(ctx context.Context, entryID uuid.UUID) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("accumulator: store is not initialized")
	}
	return a.store.SetContextIntent(ctx, entryID)
}

// Snapshot returns the stored context of a session with the pending entry
// added in memory. The counters come from the session row. A session with
// no stored state gives an empty snapshot plus the pending entry.
func (a *SQLiteAccumulator) Snapshot(ctx context.Context, sessionID uuid.UUID, pending *model.ContextEntry) (*model.ContextSnapshot, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("accumulator: store is not initialized")
	}
	state, err := a.store.GetContextState(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &storage.ContextStateRow{SessionID: sessionID}
	}

	snap := &model.ContextSnapshot{
		TotalActions:        state.TotalActions,
		FilesRead:           state.FilesRead,
		FilesWritten:        state.FilesWritten,
		CommandsExecuted:    state.CommandsExecuted,
		NetworkRequests:     state.NetworkRequests,
		Errors:              state.Errors,
		ToolsUsed:           slices.Clone(state.ToolsUsed),
		ClassificationsSeen: slices.Clone(state.ClassificationsSeen),
		EntitiesSeen:        slices.Clone(state.EntitiesSeen),
		IntentAvailable:     state.LastIntentSeq != nil,
		ActionsSinceIntent:  state.ActionsSinceIntent,
	}
	if !state.StartedAt.IsZero() {
		snap.SessionDuration = max(a.now().Sub(state.StartedAt), 0)
	}

	if pending != nil {
		c := session.EventCounts(&events.Event{
			Kind:         entryKind(pending),
			ActionType:   events.ActionType(pending.ActionType),
			ResultStatus: events.ResultStatus(pending.Result),
		})
		snap.TotalActions += c.TotalActions
		snap.FilesRead += c.FilesRead
		snap.FilesWritten += c.FilesWritten
		snap.CommandsExecuted += c.CommandsExecuted
		snap.NetworkRequests += c.NetworkRequests
		snap.Errors += c.Errors
		delta := stateDelta(pending)
		snap.ToolsUsed = addNew(snap.ToolsUsed, delta.Tools)
		snap.ClassificationsSeen = addNew(snap.ClassificationsSeen, delta.Classifications)
		switch entryKind(pending) {
		case events.KindIntent:
			snap.IntentAvailable = true
			snap.ActionsSinceIntent = 0
		case events.KindAction:
			if snap.IntentAvailable {
				snap.ActionsSinceIntent++
			}
		}
	}
	return snap, nil
}

func addNew(set, values []string) []string {
	for _, v := range values {
		if v != "" && !slices.Contains(set, v) {
			set = append(set, v)
		}
	}
	return set
}

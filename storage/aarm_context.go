package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/aarmcontextaction"
	"github.com/safedep/gryph/storage/ent/aarmcontextstate"
)

const (
	// contextResultStatusPending is the initial result status assigned to an
	// action row before UpdateContextActionResult transitions it. There is no
	// canonical events.ResultPending so this lives locally.
	contextResultStatusPending = "pending"

	// contextListMaxLimit caps QueryAllContextStates to protect against
	// accidental full-table materialization when callers pass limit <= 0.
	contextListMaxLimit = 1000
)

// AppendContextAction inserts a new action row and upserts the per-session
// state counters in a single sql.Tx. The state UPSERT uses
// INSERT ... ON CONFLICT(session_id) DO UPDATE so the counter increment is
// atomic against concurrent same-session writes.
func (s *SQLiteStore) AppendContextAction(ctx context.Context, row *ContextActionRow) error {
	if row == nil {
		return fmt.Errorf("storage: AppendContextAction: nil row")
	}
	if row.SessionID == uuid.Nil {
		return fmt.Errorf("storage: AppendContextAction: nil session ID")
	}
	if row.ActionType == "" {
		row.ActionType = "unknown"
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.Timestamp.IsZero() {
		row.Timestamp = time.Now().UTC()
	}
	if row.ResultStatus == "" {
		row.ResultStatus = contextResultStatusPending
	}

	classifications, err := marshalStringSlice(row.DataClassifications)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("storage: begin tx for context append: %w", err)
	}

	insertSQL := `
INSERT INTO aarm_context_actions (
    id, session_id, event_id, timestamp, action_type, tool, agent, project,
    working_dir, result_status, duration_ms, error_message,
    data_classifications, injection_score
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var eventID interface{}
	if row.EventID != uuid.Nil {
		eventID = row.EventID
	}
	var duration interface{}
	if row.DurationMS != nil {
		duration = *row.DurationMS
	}
	var classificationsArg interface{}
	if classifications != "" {
		classificationsArg = classifications
	}
	var injection interface{}
	if row.InjectionScore != nil {
		injection = *row.InjectionScore
	}

	if _, err := tx.ExecContext(ctx, insertSQL,
		row.ID, row.SessionID, eventID, row.Timestamp, row.ActionType,
		row.Tool, row.Agent, row.Project, row.WorkingDir,
		row.ResultStatus, duration, row.ErrorMessage,
		classificationsArg, injection,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("storage: insert context action: %w", err)
	}

	if err := upsertContextState(ctx, tx, row, classifications); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit context append: %w", err)
	}
	return nil
}

func upsertContextState(ctx context.Context, tx *sql.Tx, row *ContextActionRow, classificationsJSON string) error {
	deltaFilesRead := 0
	deltaFilesWritten := 0
	deltaCommands := 0
	deltaNetwork := 0
	switch events.ActionType(row.ActionType) {
	case events.ActionFileRead:
		deltaFilesRead = 1
	case events.ActionFileWrite:
		deltaFilesWritten = 1
	case events.ActionCommandExec:
		deltaCommands = 1
	case events.ActionNetworkRequest:
		deltaNetwork = 1
	}

	initialTools := "[]"
	if row.Tool != "" {
		buf, err := json.Marshal([]string{row.Tool})
		if err != nil {
			return fmt.Errorf("storage: marshal tools_used: %w", err)
		}
		initialTools = string(buf)
	}

	initialClassifications := "[]"
	hasClassifications := classificationsJSON != ""
	if hasClassifications {
		initialClassifications = classificationsJSON
	}

	stmt := `
INSERT INTO aarm_context_states (
    session_id, first_seen_at, last_action_at,
    total_actions, files_read, files_written,
    commands_executed, network_requests, errors,
    tools_used, classifications_seen, entities_seen, semantic_drift
) VALUES (?, ?, ?, 1, ?, ?, ?, ?, 0, ?, ?, '[]', 0)
ON CONFLICT(session_id) DO UPDATE SET
    last_action_at = CASE
        WHEN excluded.last_action_at > aarm_context_states.last_action_at
        THEN excluded.last_action_at
        ELSE aarm_context_states.last_action_at
    END,
    total_actions = aarm_context_states.total_actions + 1,
    files_read = aarm_context_states.files_read + ?,
    files_written = aarm_context_states.files_written + ?,
    commands_executed = aarm_context_states.commands_executed + ?,
    network_requests = aarm_context_states.network_requests + ?,
    tools_used = CASE
        WHEN ? = '' THEN aarm_context_states.tools_used
        WHEN aarm_context_states.tools_used IS NULL OR aarm_context_states.tools_used = '' OR aarm_context_states.tools_used = '[]'
            THEN json_array(?)
        WHEN EXISTS (SELECT 1 FROM json_each(aarm_context_states.tools_used) WHERE value = ?)
            THEN aarm_context_states.tools_used
        ELSE json_insert(aarm_context_states.tools_used, '$[#]', ?)
    END,
    classifications_seen = CASE
        WHEN ? = 0 THEN aarm_context_states.classifications_seen
        WHEN aarm_context_states.classifications_seen IS NULL OR aarm_context_states.classifications_seen = '' OR aarm_context_states.classifications_seen = '[]'
            THEN ?
        ELSE (
            SELECT json_group_array(value) FROM (
                SELECT DISTINCT value FROM (
                    SELECT value FROM json_each(aarm_context_states.classifications_seen)
                    UNION
                    SELECT value FROM json_each(?)
                )
            )
        )
    END
`

	hasClassificationsArg := 0
	if hasClassifications {
		hasClassificationsArg = 1
	}

	if _, err := tx.ExecContext(ctx, stmt,
		row.SessionID, row.Timestamp, row.Timestamp,
		deltaFilesRead, deltaFilesWritten, deltaCommands, deltaNetwork,
		initialTools, initialClassifications,
		deltaFilesRead, deltaFilesWritten, deltaCommands, deltaNetwork,
		row.Tool, row.Tool, row.Tool, row.Tool,
		hasClassificationsArg, initialClassifications, initialClassifications,
	); err != nil {
		return fmt.Errorf("storage: upsert context state: %w", err)
	}

	return nil
}

// UpdateContextActionResult transitions an action row from pending to a final
// result_status and bumps the state errors counter when status=error.
func (s *SQLiteStore) UpdateContextActionResult(ctx context.Context, actionID uuid.UUID, status string, durationMS int64, errorMsg string) error {
	if actionID == uuid.Nil {
		return fmt.Errorf("storage: UpdateContextActionResult: nil action ID")
	}
	if status == "" {
		status = string(events.ResultSuccess)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("storage: begin tx for result update: %w", err)
	}

	var sessionID uuid.UUID
	row := tx.QueryRowContext(ctx,
		`SELECT session_id FROM aarm_context_actions WHERE id = ?`, actionID)
	if err := row.Scan(&sessionID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("storage: lookup context action: %w", err)
	}

	updateSQL := `
UPDATE aarm_context_actions
SET result_status = ?,
    duration_ms = CASE WHEN ? > 0 THEN ? ELSE duration_ms END,
    error_message = CASE WHEN ? <> '' THEN ? ELSE error_message END
WHERE id = ?`
	if _, err := tx.ExecContext(ctx, updateSQL,
		status, durationMS, durationMS, errorMsg, errorMsg, actionID,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("storage: update context action result: %w", err)
	}

	if status == string(events.ResultError) {
		if _, err := tx.ExecContext(ctx,
			`UPDATE aarm_context_states SET errors = errors + 1 WHERE session_id = ?`,
			sessionID,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("storage: bump errors counter: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit result update: %w", err)
	}
	return nil
}

// GetContextState returns the per-session state row, or (nil, nil) if the
// session has not yet appended any actions.
func (s *SQLiteStore) GetContextState(ctx context.Context, sessionID uuid.UUID) (*ContextStateRow, error) {
	state, err := s.client.AarmContextState.Query().
		Where(aarmcontextstate.SessionIDEQ(sessionID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get context state: %w", err)
	}
	return entToContextState(state), nil
}

// GetContextStateByPrefix returns the per-session state row whose session_id
// (as text) starts with prefix. Returns (nil, nil) when no row matches and an
// error when the prefix is ambiguous (>1 match).
func (s *SQLiteStore) GetContextStateByPrefix(ctx context.Context, prefix string) (*ContextStateRow, error) {
	rows, err := s.client.AarmContextState.Query().
		Where(func(sel *entsql.Selector) {
			sel.Where(entsql.Like(aarmcontextstate.FieldSessionID, prefix+"%"))
		}).
		Limit(2).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: get context state by prefix: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("storage: context state prefix %q is ambiguous", prefix)
	}
	return entToContextState(rows[0]), nil
}

// QueryContextActions returns the most recent N action rows for a session,
// ordered by timestamp DESC.
func (s *SQLiteStore) QueryContextActions(ctx context.Context, sessionID uuid.UUID, limit int) ([]*ContextActionRow, error) {
	q := s.client.AarmContextAction.Query().
		Where(aarmcontextaction.SessionIDEQ(sessionID)).
		Order(aarmcontextaction.ByTimestamp(entsql.OrderDesc()), aarmcontextaction.ByID(entsql.OrderDesc()))
	if limit > 0 {
		q.Limit(limit)
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query context actions: %w", err)
	}
	out := make([]*ContextActionRow, len(rows))
	for i, r := range rows {
		out[i] = entToContextAction(r)
	}
	return out, nil
}

// QueryAllContextStates returns every state row, ordered by last_action_at
// DESC. Used by `gryph policy context` without --session. limit is clamped
// to contextListMaxLimit to prevent unbounded materialization.
func (s *SQLiteStore) QueryAllContextStates(ctx context.Context, limit int) ([]*ContextStateRow, error) {
	if limit <= 0 || limit > contextListMaxLimit {
		limit = contextListMaxLimit
	}
	rows, err := s.client.AarmContextState.Query().
		Order(aarmcontextstate.ByLastActionAt(entsql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query context states: %w", err)
	}
	out := make([]*ContextStateRow, len(rows))
	for i, r := range rows {
		out[i] = entToContextState(r)
	}
	return out, nil
}

// DeleteContextBefore removes action rows older than before and prunes
// orphaned state rows whose last_action_at is also past the cutoff and have
// no remaining actions referencing their session_id. Returns the number of
// action rows deleted.
func (s *SQLiteStore) DeleteContextBefore(ctx context.Context, before time.Time) (int, error) {
	deleted, err := s.client.AarmContextAction.Delete().
		Where(aarmcontextaction.TimestampLT(before)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: delete context actions: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM aarm_context_states
WHERE last_action_at < ?
  AND NOT EXISTS (
      SELECT 1 FROM aarm_context_actions
      WHERE aarm_context_actions.session_id = aarm_context_states.session_id
  )`, before); err != nil {
		return deleted, fmt.Errorf("storage: prune context states: %w", err)
	}

	return deleted, nil
}

// CountContextBefore returns the number of action rows older than before.
func (s *SQLiteStore) CountContextBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := s.client.AarmContextAction.Query().
		Where(aarmcontextaction.TimestampLT(before)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: count context actions: %w", err)
	}
	return n, nil
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("storage: marshal string slice: %w", err)
	}
	return string(buf), nil
}

func entToContextAction(e *ent.AarmContextAction) *ContextActionRow {
	row := &ContextActionRow{
		ID:                  e.ID,
		SessionID:           e.SessionID,
		EventID:             e.EventID,
		Timestamp:           e.Timestamp,
		ActionType:          string(e.ActionType),
		Tool:                e.Tool,
		Agent:               e.Agent,
		Project:             e.Project,
		WorkingDir:          e.WorkingDir,
		ResultStatus:        string(e.ResultStatus),
		ErrorMessage:        e.ErrorMessage,
		DataClassifications: e.DataClassifications,
	}
	if e.DurationMs != nil {
		v := *e.DurationMs
		row.DurationMS = &v
	}
	if e.InjectionScore != nil {
		v := *e.InjectionScore
		row.InjectionScore = &v
	}
	return row
}

func entToContextState(e *ent.AarmContextState) *ContextStateRow {
	return &ContextStateRow{
		SessionID:           e.SessionID,
		FirstSeenAt:         e.FirstSeenAt,
		LastActionAt:        e.LastActionAt,
		TotalActions:        e.TotalActions,
		FilesRead:           e.FilesRead,
		FilesWritten:        e.FilesWritten,
		CommandsExecuted:    e.CommandsExecuted,
		NetworkRequests:     e.NetworkRequests,
		Errors:              e.Errors,
		ToolsUsed:           e.ToolsUsed,
		ClassificationsSeen: e.ClassificationsSeen,
		EntitiesSeen:        e.EntitiesSeen,
		SemanticDrift:       e.SemanticDrift,
	}
}

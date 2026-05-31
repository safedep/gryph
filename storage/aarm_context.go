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
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
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

	// contextListSetCap bounds the per-row distinct-value lists
	// (tools_used, classifications_seen). The upsert SQL stops appending
	// when the existing list already contains this many values so the row
	// stays bounded across long sessions. The entities_seen column is
	// reserved for richer entity extraction landing in Phase 4 and is not
	// maintained today.
	contextListSetCap = 100
)

// AppendContextAction inserts a new action row, computes its place in the
// per-session hash chain, and upserts the per-session state counters inside
// a single writer transaction. contextWriteMu serializes the SELECT-then-
// INSERT path so two concurrent same-session writers cannot both observe the
// same last (sequence, hash) before one upgrades to a writer (which would
// surface as SQLITE_BUSY under WAL).
//
// The hash input mirrors aarm/accumulator.ContextChainInput. See that
// package for the canonical field ordering. The chain hash covers the
// as-mediated row, not the post-hook outcome, so UpdateContextActionResult
// does not need to re-hash.
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

	s.contextWriteMu.Lock()
	defer s.contextWriteMu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("storage: begin tx for context append: %w", err)
	}

	prevSeq, prevHash, err := readLastContextChainTx(ctx, tx, row.SessionID)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("storage: read last context chain: %w", err)
	}

	var nextSeq int64 = 1
	if prevSeq != nil {
		nextSeq = *prevSeq + 1
	}

	var injectionScore float32
	if row.InjectionScore != nil {
		injectionScore = *row.InjectionScore
	}
	hashInput := contextchain.InputFromRow(
		nextSeq, prevHash, row.Timestamp,
		row.SessionID, row.EventID, row.ID,
		row.ActionType, row.Tool, row.Agent, row.Project, row.WorkingDir,
		row.DataClassifications, injectionScore,
	)
	hash, err := contextchain.ComputeHash(hashInput)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("storage: compute context hash: %w", err)
	}

	row.Sequence = &nextSeq
	row.PrevHash = prevHash
	row.Hash = hash

	if err := insertContextActionTx(ctx, tx, row, classifications); err != nil {
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

// readLastContextChainTx fetches the highest-sequence chain row for
// sessionID inside an open sql.Tx, returning (nil, nil, nil) when the
// session has no chained rows yet.
func readLastContextChainTx(ctx context.Context, tx *sql.Tx, sessionID uuid.UUID) (*int64, []byte, error) {
	const q = `
SELECT sequence, hash
FROM aarm_context_actions
WHERE session_id = ? AND sequence IS NOT NULL
ORDER BY sequence DESC
LIMIT 1`
	row := tx.QueryRowContext(ctx, q, sessionID)
	var (
		seq  sql.NullInt64
		hash []byte
	)
	if err := row.Scan(&seq, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if !seq.Valid {
		return nil, nil, nil
	}
	v := seq.Int64
	return &v, hash, nil
}

func insertContextActionTx(ctx context.Context, tx *sql.Tx, row *ContextActionRow, classificationsJSON string) error {
	const insertSQL = `
INSERT INTO aarm_context_actions (
    id, session_id, event_id, timestamp, action_type, tool, agent, project,
    working_dir, result_status, duration_ms, error_message,
    data_classifications, injection_score,
    sequence, prev_hash, hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var eventID interface{}
	if row.EventID != uuid.Nil {
		eventID = row.EventID
	}
	var duration interface{}
	if row.DurationMS != nil {
		duration = *row.DurationMS
	}
	var classificationsArg interface{}
	if classificationsJSON != "" {
		classificationsArg = classificationsJSON
	}
	var injection interface{}
	if row.InjectionScore != nil {
		injection = *row.InjectionScore
	}
	var sequenceArg interface{}
	if row.Sequence != nil {
		sequenceArg = *row.Sequence
	}
	var prevHashArg interface{}
	if len(row.PrevHash) > 0 {
		prevHashArg = row.PrevHash
	}
	var hashArg interface{}
	if len(row.Hash) > 0 {
		hashArg = row.Hash
	}

	_, err := tx.ExecContext(ctx, insertSQL,
		row.ID, row.SessionID, eventID, row.Timestamp, row.ActionType,
		row.Tool, row.Agent, row.Project, row.WorkingDir,
		row.ResultStatus, duration, row.ErrorMessage,
		classificationsArg, injection,
		sequenceArg, prevHashArg, hashArg,
	)
	return err
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
    tools_used, classifications_seen, semantic_drift
) VALUES (?, ?, ?, 1, ?, ?, ?, ?, 0, ?, ?, 0)
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
        WHEN (SELECT COUNT(*) FROM json_each(aarm_context_states.tools_used)) >= ?
            THEN aarm_context_states.tools_used
        ELSE json_insert(aarm_context_states.tools_used, '$[#]', ?)
    END,
    classifications_seen = CASE
        WHEN ? = 0 THEN aarm_context_states.classifications_seen
        WHEN aarm_context_states.classifications_seen IS NULL OR aarm_context_states.classifications_seen = '' OR aarm_context_states.classifications_seen = '[]'
            THEN ?
        ELSE (
            SELECT json_group_array(value) FROM (
                SELECT value FROM (
                    SELECT DISTINCT value FROM (
                        SELECT value FROM json_each(aarm_context_states.classifications_seen)
                        UNION
                        SELECT value FROM json_each(?)
                    )
                )
                LIMIT ?
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
		row.Tool, row.Tool, row.Tool, contextListSetCap, row.Tool,
		hasClassificationsArg, initialClassifications, initialClassifications, contextListSetCap,
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

// QueryContextActionsFiltered returns action rows matching filter. When
// filter.SessionID is set and filter.Ascending is true, rows are ordered by
// (sequence ASC, timestamp ASC) so the full per-session chain is returned in
// chain order. Otherwise rows fall back to the table-friendly
// (timestamp DESC, id DESC) ordering.
//
// Limit semantics:
//   - filter.Limit > 0: capped at contextListMaxLimit.
//   - filter.Limit == 0 (default): capped at contextListMaxLimit.
//   - filter.Limit == -1: no LIMIT clause. Reserved for admin operations
//     such as full per-session chain verification.
func (s *SQLiteStore) QueryContextActionsFiltered(ctx context.Context, filter *ContextActionFilter) ([]*ContextActionRow, error) {
	if filter == nil {
		filter = &ContextActionFilter{}
	}
	q := s.client.AarmContextAction.Query()
	if filter.SessionID != nil {
		q.Where(aarmcontextaction.SessionIDEQ(*filter.SessionID))
	}

	if filter.SessionID != nil && filter.Ascending {
		q.Order(
			aarmcontextaction.BySequence(entsql.OrderAsc()),
			aarmcontextaction.ByTimestamp(entsql.OrderAsc()),
			aarmcontextaction.ByID(entsql.OrderAsc()),
		)
	} else {
		q.Order(aarmcontextaction.ByTimestamp(entsql.OrderDesc()), aarmcontextaction.ByID(entsql.OrderDesc()))
	}

	if filter.Limit != -1 {
		limit := filter.Limit
		if limit <= 0 || limit > contextListMaxLimit {
			limit = contextListMaxLimit
		}
		q.Limit(limit)
	}

	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query context actions filtered: %w", err)
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

// ListContextSessionIDs returns the distinct session IDs that appear in the
// context-action log. Intended for admin operations such as full-cluster
// chain verification.
func (s *SQLiteStore) ListContextSessionIDs(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.client.AarmContextAction.Query().
		Unique(true).
		Select(aarmcontextaction.FieldSessionID).
		Scan(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("storage: list context session IDs: %w", err)
	}
	return ids, nil
}

// DeleteContextBefore removes context action rows in fixed-size batches, then
// prunes the now-orphaned per-session state rows.
//
// Retention operates at session granularity for two reasons. First, the
// per-session action rows form a hash chain; deleting only a prefix would leave
// the oldest surviving row with a non-zero prev_hash and a sequence other than
// 1, which the chain verifier reports as a spurious break. Second, the
// aarm_context_states counters are cumulative over the whole session, so
// deleting a subset of a session's actions without resetting the state row
// would leave context.total_actions (and the per-type counters) permanently
// larger than the rows that remain, firing threshold rules on phantom history.
// A session is therefore purged only when its most recent action is older than
// before, so it is deleted whole (actions plus state) or not at all. Returns
// the total number of action rows deleted.
func (s *SQLiteStore) DeleteContextBefore(ctx context.Context, before time.Time) (int, error) {
	const deleteBatch = 1000
	total := 0
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM aarm_context_actions WHERE id IN (
				SELECT id FROM aarm_context_actions
				WHERE session_id IN (
					SELECT session_id FROM aarm_context_actions
					GROUP BY session_id
					HAVING MAX(timestamp) < ?
				)
				LIMIT ?
			)`,
			before, deleteBatch,
		)
		if err != nil {
			return total, fmt.Errorf("storage: delete context actions: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("storage: delete context actions rows affected: %w", err)
		}
		total += int(n)
		if n == 0 {
			break
		}
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM aarm_context_states
WHERE last_action_at < ?
  AND NOT EXISTS (
      SELECT 1 FROM aarm_context_actions
      WHERE aarm_context_actions.session_id = aarm_context_states.session_id
  )`, before); err != nil {
		return total, fmt.Errorf("storage: prune context states: %w", err)
	}

	return total, nil
}

// CountContextBefore returns the number of action rows eligible for retention
// deletion: rows belonging to sessions whose most recent action is older than
// before. This mirrors the session-granularity DeleteContextBefore so the
// retention dry-run reports the count that will actually be purged.
func (s *SQLiteStore) CountContextBefore(ctx context.Context, before time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM aarm_context_actions
		WHERE session_id IN (
			SELECT session_id FROM aarm_context_actions
			GROUP BY session_id
			HAVING MAX(timestamp) < ?
		)`,
		before,
	).Scan(&n)
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
		PrevHash:            e.PrevHash,
		Hash:                e.Hash,
	}
	if e.DurationMs != nil {
		v := *e.DurationMs
		row.DurationMS = &v
	}
	if e.InjectionScore != nil {
		v := *e.InjectionScore
		row.InjectionScore = &v
	}
	if e.Sequence != nil {
		v := *e.Sequence
		row.Sequence = &v
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

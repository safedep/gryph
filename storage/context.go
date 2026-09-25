package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/contextentry"
	"github.com/safedep/gryph/storage/ent/contextstate"
	entsession "github.com/safedep/gryph/storage/ent/session"
)

const (
	// contextResultStatusPending is the result status of an entry before
	// UpdateContextEntryResult sets the outcome.
	contextResultStatusPending = "pending"

	contextKindAction = "action"
	contextKindIntent = "intent"

	// contextListMaxLimit caps list queries when callers pass limit <= 0.
	contextListMaxLimit = 1000
)

// Caps bound the per-session sets so the state row stays small across long
// sessions. A set stops growing when it reaches its cap.
const (
	capTools           = 100
	capClassifications = 100
	capTags            = 200
	capOrigins         = 100
	capEntities        = 500
	capEgressHosts     = 500
)

// AppendContextEntry inserts an entry, computes its place in the per-session
// hash chain, and merges the delta into the state row, in one writer
// transaction. contextWriteMu serializes the SELECT-then-INSERT path in one
// process. The transaction takes the write lock at BEGIN
// (_txlock=immediate), so writers in other hook processes wait, and the
// read-merge-write of the state row is safe.
func (s *SQLiteStore) AppendContextEntry(ctx context.Context, row *ContextEntryRow, delta *ContextStateDelta) error {
	if row == nil {
		return fmt.Errorf("storage: AppendContextEntry: nil row")
	}
	if row.SessionID == uuid.Nil {
		return fmt.Errorf("storage: AppendContextEntry: nil session ID")
	}
	if row.ActionType == "" {
		row.ActionType = "unknown"
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.Timestamp.IsZero() {
		row.Timestamp = time.Now()
	}
	row.Timestamp = row.Timestamp.UTC()
	if row.ResultStatus == "" {
		row.ResultStatus = contextResultStatusPending
	}

	s.contextWriteMu.Lock()
	defer s.contextWriteMu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("storage: begin tx for context append: %w", err)
	}
	if err := appendContextEntryTx(ctx, tx, row, delta); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit context append: %w", err)
	}
	return nil
}

func appendContextEntryTx(ctx context.Context, tx *sql.Tx, row *ContextEntryRow, delta *ContextStateDelta) error {
	var (
		lastSeq  sql.NullInt64
		lastHash []byte
	)
	err := tx.QueryRowContext(ctx,
		`SELECT sequence, hash FROM context_entries WHERE session_id = ? ORDER BY sequence DESC LIMIT 1`,
		row.SessionID,
	).Scan(&lastSeq, &lastHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("storage: read last context entry: %w", err)
	}

	row.Sequence = lastSeq.Int64 + 1
	row.PrevHash = lastHash
	row.HashVersion = contextchain.Version
	row.Hash, err = contextchain.ComputeHash(ContextChainInput(row))
	if err != nil {
		return fmt.Errorf("storage: compute context hash: %w", err)
	}

	if err := insertContextEntryTx(ctx, tx, row); err != nil {
		return fmt.Errorf("storage: insert context entry: %w", err)
	}
	if err := mergeContextStateTx(ctx, tx, row, delta); err != nil {
		return fmt.Errorf("storage: merge context state: %w", err)
	}
	return nil
}

// ContextChainInput maps a stored entry to its hash input. The insert path
// and the verifier both use it, so they cannot disagree. An empty list and a
// nil list hash the same, because the column stores both as NULL.
func ContextChainInput(row *ContextEntryRow) contextchain.Input {
	var score float32
	if row.InjectionScore != nil {
		score = *row.InjectionScore
	}
	return contextchain.Input{
		Sequence:          row.Sequence,
		PrevHash:          row.PrevHash,
		TimestampUnixNano: row.Timestamp.UnixNano(),
		SessionID:         row.SessionID,
		EventID:           row.EventID,
		EntryID:           row.ID,
		Kind:              row.Kind,
		ActionType:        row.ActionType,
		Tool:              row.Tool,
		ToolCallID:        row.ToolCallID,
		LinkedEventID:     row.LinkedEventID,
		Phase:             row.Phase,
		Target: contextchain.Target{
			Host:      row.TargetHost,
			MCPServer: row.TargetMCPServer,
			MCPTool:   row.TargetMCPTool,
		},
		Origin:          row.Origin,
		Tags:            nilIfEmpty(row.Tags),
		Classifications: nilIfEmpty(row.Classifications),
		InjectionScore:  score,
		Decision:        row.Decision,
		MatchedRuleIDs:  nilIfEmpty(row.MatchedRuleIDs),
		ContentDigest:   row.ContentDigest,
	}
}

func insertContextEntryTx(ctx context.Context, tx *sql.Tx, row *ContextEntryRow) error {
	const q = `
INSERT INTO context_entries (
    id, session_id, event_id, sequence, kind, timestamp,
    action_type, tool, tool_call_id, linked_event_id, phase,
    target_host, target_mcp_server, target_mcp_tool, origin,
    tags, classifications, injection_score,
    decision, matched_rule_ids, content_digest,
    result_status, duration_ms, error_message,
    hash_version, prev_hash, hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	tags, err := jsonOrNil(row.Tags)
	if err != nil {
		return err
	}
	classes, err := jsonOrNil(row.Classifications)
	if err != nil {
		return err
	}
	rules, err := jsonOrNil(row.MatchedRuleIDs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, q,
		row.ID, row.SessionID, nilUUID(row.EventID), row.Sequence, row.Kind, row.Timestamp,
		row.ActionType, row.Tool, row.ToolCallID, nilUUID(row.LinkedEventID), row.Phase,
		row.TargetHost, row.TargetMCPServer, row.TargetMCPTool, row.Origin,
		tags, classes, nilPtr(row.InjectionScore),
		row.Decision, rules, row.ContentDigest,
		row.ResultStatus, nilPtr(row.DurationMS), row.ErrorMessage,
		row.HashVersion, nilBytes(row.PrevHash), row.Hash,
	)
	return err
}

// mergeContextStateTx reads the state row inside the transaction, merges
// the delta, and writes the row back. Reading inside the transaction keeps
// a concurrent writer from losing an update.
func mergeContextStateTx(ctx context.Context, tx *sql.Tx, row *ContextEntryRow, delta *ContextStateDelta) error {
	state := contextStateColumns{}
	err := tx.QueryRowContext(ctx, `
SELECT tools_used, classifications_seen, tags_seen, origins_seen, entities_seen, egress_hosts, last_intent_seq, last_intent_at
FROM context_states WHERE session_id = ?`, row.SessionID).Scan(
		&state.tools, &state.classes, &state.tags, &state.origins, &state.entities, &state.hosts,
		&state.lastIntentSeq, &state.lastIntentAt,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	cur, err := state.decode()
	if err != nil {
		return err
	}

	if delta != nil {
		cur.ToolsUsed = addCapped(cur.ToolsUsed, delta.Tools, capTools)
		cur.ClassificationsSeen = addCapped(cur.ClassificationsSeen, delta.Classifications, capClassifications)
		cur.OriginsSeen = addCapped(cur.OriginsSeen, delta.Origins, capOrigins)
		cur.EntitiesSeen = addCapped(cur.EntitiesSeen, delta.Entities, capEntities)
		cur.EgressHosts = addCapped(cur.EgressHosts, delta.EgressHosts, capEgressHosts)
		for _, tag := range delta.Tags {
			if _, ok := cur.TagsSeen[tag]; !ok && len(cur.TagsSeen) < capTags {
				cur.TagsSeen[tag] = row.Sequence
			}
		}
		if delta.Intent {
			seq, at := row.Sequence, row.Timestamp
			cur.LastIntentSeq, cur.LastIntentAt = &seq, &at
		}
	}

	cols, err := encodeContextState(cur)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO context_states (
    session_id, last_entry_at, tools_used, classifications_seen, tags_seen,
    origins_seen, entities_seen, egress_hosts, last_intent_seq, last_intent_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    last_entry_at = CASE WHEN excluded.last_entry_at > context_states.last_entry_at
        THEN excluded.last_entry_at ELSE context_states.last_entry_at END,
    tools_used = excluded.tools_used,
    classifications_seen = excluded.classifications_seen,
    tags_seen = excluded.tags_seen,
    origins_seen = excluded.origins_seen,
    entities_seen = excluded.entities_seen,
    egress_hosts = excluded.egress_hosts,
    last_intent_seq = excluded.last_intent_seq,
    last_intent_at = excluded.last_intent_at`,
		row.SessionID, row.Timestamp, cols.tools, cols.classes, cols.tags,
		cols.origins, cols.entities, cols.hosts, cols.lastIntentSeq, cols.lastIntentAt,
	)
	return err
}

// contextStateColumns holds the raw JSON columns of a state row.
type contextStateColumns struct {
	tools, classes, tags, origins, entities, hosts sql.NullString
	lastIntentSeq                                  sql.NullInt64
	lastIntentAt                                   sql.NullTime
}

func (c contextStateColumns) decode() (*ContextStateRow, error) {
	out := &ContextStateRow{TagsSeen: map[string]int64{}}
	for _, f := range []struct {
		raw sql.NullString
		dst any
	}{
		{c.tools, &out.ToolsUsed}, {c.classes, &out.ClassificationsSeen}, {c.tags, &out.TagsSeen},
		{c.origins, &out.OriginsSeen}, {c.entities, &out.EntitiesSeen}, {c.hosts, &out.EgressHosts},
	} {
		if f.raw.Valid && f.raw.String != "" {
			if err := json.Unmarshal([]byte(f.raw.String), f.dst); err != nil {
				return nil, fmt.Errorf("storage: decode context state: %w", err)
			}
		}
	}
	if out.TagsSeen == nil {
		out.TagsSeen = map[string]int64{}
	}
	if c.lastIntentSeq.Valid {
		v := c.lastIntentSeq.Int64
		out.LastIntentSeq = &v
	}
	if c.lastIntentAt.Valid {
		v := c.lastIntentAt.Time
		out.LastIntentAt = &v
	}
	return out, nil
}

type encodedContextState struct {
	tools, classes, tags, origins, entities, hosts string
	lastIntentSeq                                  any
	lastIntentAt                                   any
}

func encodeContextState(st *ContextStateRow) (encodedContextState, error) {
	var out encodedContextState
	for _, f := range []struct {
		src any
		dst *string
	}{
		{nonNil(st.ToolsUsed), &out.tools}, {nonNil(st.ClassificationsSeen), &out.classes},
		{st.TagsSeen, &out.tags}, {nonNil(st.OriginsSeen), &out.origins},
		{nonNil(st.EntitiesSeen), &out.entities}, {nonNil(st.EgressHosts), &out.hosts},
	} {
		b, err := json.Marshal(f.src)
		if err != nil {
			return out, fmt.Errorf("storage: encode context state: %w", err)
		}
		*f.dst = string(b)
	}
	out.lastIntentSeq = nilPtr(st.LastIntentSeq)
	out.lastIntentAt = nilPtr(st.LastIntentAt)
	return out, nil
}

// addCapped appends each value that set does not hold yet, until the set
// reaches its cap.
func addCapped(set, values []string, capacity int) []string {
	for _, v := range values {
		if v == "" || slices.Contains(set, v) || len(set) >= capacity {
			continue
		}
		set = append(set, v)
	}
	return set
}

// UpdateContextEntryResult sets the outcome of an entry. The chain hash does
// not cover the outcome, so no re-hash is needed. An unknown entry is not an
// error, because the entry can predate a retention purge.
func (s *SQLiteStore) UpdateContextEntryResult(ctx context.Context, entryID uuid.UUID, status string, durationMS int64, errorMsg string) error {
	if entryID == uuid.Nil {
		return fmt.Errorf("storage: UpdateContextEntryResult: nil entry ID")
	}
	if status == "" {
		status = "success"
	}
	upd := s.client.ContextEntry.Update().
		Where(contextentry.IDEQ(entryID)).
		SetResultStatus(status)
	if durationMS > 0 {
		upd.SetDurationMs(durationMS)
	}
	if errorMsg != "" {
		upd.SetErrorMessage(errorMsg)
	}
	if _, err := upd.Save(ctx); err != nil {
		return fmt.Errorf("storage: update context entry result: %w", err)
	}
	return nil
}

// SetContextIntent makes an intent entry the latest intent of its session.
// It never moves the latest intent back to an older entry. An unknown entry
// or an entry of another kind changes nothing.
func (s *SQLiteStore) SetContextIntent(ctx context.Context, entryID uuid.UUID) error {
	entry, err := s.client.ContextEntry.Get(ctx, entryID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: get context entry: %w", err)
	}
	if entry.Kind != contextKindIntent {
		return nil
	}

	s.contextWriteMu.Lock()
	defer s.contextWriteMu.Unlock()
	_, err = s.db.ExecContext(ctx, `
UPDATE context_states SET last_intent_seq = ?, last_intent_at = ?
WHERE session_id = ? AND (last_intent_seq IS NULL OR last_intent_seq < ?)`,
		entry.Sequence, entry.Timestamp.UTC(), entry.SessionID, entry.Sequence)
	if err != nil {
		return fmt.Errorf("storage: set context intent: %w", err)
	}
	return nil
}

// GetContextState returns the state row of a session joined with the
// session counters. It returns (nil, nil) when the session has neither a
// session row nor a state row.
func (s *SQLiteStore) GetContextState(ctx context.Context, sessionID uuid.UUID) (*ContextStateRow, error) {
	state, err := s.client.ContextState.Query().Where(contextstate.SessionIDEQ(sessionID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("storage: get context state: %w", err)
	}
	sess, err := s.client.Session.Get(ctx, sessionID)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("storage: get session for context state: %w", err)
	}
	if state == nil && sess == nil {
		return nil, nil
	}
	row := joinContextState(sessionID, state, sess)
	if err := s.countActionsSinceIntent(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *SQLiteStore) countActionsSinceIntent(ctx context.Context, row *ContextStateRow) error {
	if row.LastIntentSeq == nil {
		return nil
	}
	n, err := s.client.ContextEntry.Query().
		Where(
			contextentry.SessionIDEQ(row.SessionID),
			contextentry.KindEQ(contextKindAction),
			contextentry.SequenceGT(*row.LastIntentSeq),
		).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("storage: count actions since intent: %w", err)
	}
	row.ActionsSinceIntent = n
	return nil
}

// GetContextStateByPrefix returns the state row whose session_id (as text)
// starts with prefix. It returns (nil, nil) when no row matches and an error
// when the prefix is ambiguous.
func (s *SQLiteStore) GetContextStateByPrefix(ctx context.Context, prefix string) (*ContextStateRow, error) {
	rows, err := s.client.ContextState.Query().
		Where(func(sel *entsql.Selector) {
			sel.Where(entsql.Like(contextstate.FieldSessionID, prefix+"%"))
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
	return s.GetContextState(ctx, rows[0].SessionID)
}

// QueryAllContextStates returns the state rows, newest first, joined with
// the session counters.
func (s *SQLiteStore) QueryAllContextStates(ctx context.Context, limit int) ([]*ContextStateRow, error) {
	if limit <= 0 || limit > contextListMaxLimit {
		limit = contextListMaxLimit
	}
	states, err := s.client.ContextState.Query().
		Order(contextstate.ByLastEntryAt(entsql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query context states: %w", err)
	}
	ids := make([]uuid.UUID, len(states))
	for i, st := range states {
		ids[i] = st.SessionID
	}
	sessions, err := s.client.Session.Query().Where(entsession.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query sessions for context states: %w", err)
	}
	byID := make(map[uuid.UUID]*ent.Session, len(sessions))
	for _, sess := range sessions {
		byID[sess.ID] = sess
	}
	out := make([]*ContextStateRow, len(states))
	for i, st := range states {
		out[i] = joinContextState(st.SessionID, st, byID[st.SessionID])
		if err := s.countActionsSinceIntent(ctx, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func joinContextState(sessionID uuid.UUID, st *ent.ContextState, sess *ent.Session) *ContextStateRow {
	out := &ContextStateRow{SessionID: sessionID, TagsSeen: map[string]int64{}}
	if sess != nil {
		out.StartedAt = sess.StartedAt
		out.TotalActions = sess.TotalActions
		out.FilesRead = sess.FilesRead
		out.FilesWritten = sess.FilesWritten
		out.CommandsExecuted = sess.CommandsExecuted
		out.NetworkRequests = sess.NetworkRequests
		out.Errors = sess.Errors
	}
	if st != nil {
		out.LastEntryAt = st.LastEntryAt
		out.ToolsUsed = st.ToolsUsed
		out.ClassificationsSeen = st.ClassificationsSeen
		if st.TagsSeen != nil {
			out.TagsSeen = st.TagsSeen
		}
		out.OriginsSeen = st.OriginsSeen
		out.EntitiesSeen = st.EntitiesSeen
		out.EgressHosts = st.EgressHosts
		out.LastIntentSeq = st.LastIntentSeq
		out.LastIntentAt = st.LastIntentAt
	}
	return out
}

// QueryContextEntries returns the entries that match filter.
func (s *SQLiteStore) QueryContextEntries(ctx context.Context, filter *ContextEntryFilter) ([]*ContextEntryRow, error) {
	if filter == nil {
		filter = &ContextEntryFilter{}
	}
	q := s.client.ContextEntry.Query()
	if filter.SessionID != nil {
		q.Where(contextentry.SessionIDEQ(*filter.SessionID))
	}
	if filter.Ascending {
		q.Order(contextentry.BySessionID(), contextentry.BySequence(entsql.OrderAsc()))
	} else {
		q.Order(contextentry.ByTimestamp(entsql.OrderDesc()), contextentry.BySequence(entsql.OrderDesc()))
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
		return nil, fmt.Errorf("storage: query context entries: %w", err)
	}
	out := make([]*ContextEntryRow, len(rows))
	for i, r := range rows {
		out[i] = entToContextEntry(r)
	}
	return out, nil
}

// ListContextSessionIDs returns the distinct session IDs in the entry log.
// It is for admin operations such as full chain verification.
func (s *SQLiteStore) ListContextSessionIDs(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.client.ContextEntry.Query().
		Unique(true).
		Select(contextentry.FieldSessionID).
		Scan(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("storage: list context session IDs: %w", err)
	}
	return ids, nil
}

// DeleteContextBefore removes the entries of each session whose newest
// entry is older than before, in fixed-size batches, then removes the state
// rows that have no entries left.
//
// Retention works on whole sessions. The entries of a session form a hash
// chain. Deleting only a prefix would leave the oldest surviving entry with
// a sequence other than 1, which the verifier reports as a break. Returns
// the number of entries deleted.
func (s *SQLiteStore) DeleteContextBefore(ctx context.Context, before time.Time) (int, error) {
	const deleteBatch = 1000
	total := 0
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM context_entries WHERE id IN (
				SELECT id FROM context_entries
				WHERE session_id IN (
					SELECT session_id FROM context_entries
					GROUP BY session_id
					HAVING MAX(timestamp) < ?
				)
				LIMIT ?
			)`,
			before, deleteBatch,
		)
		if err != nil {
			return total, fmt.Errorf("storage: delete context entries: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("storage: delete context entries rows affected: %w", err)
		}
		total += int(n)
		if n == 0 {
			break
		}
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM context_states
WHERE last_entry_at < ?
  AND NOT EXISTS (
      SELECT 1 FROM context_entries
      WHERE context_entries.session_id = context_states.session_id
  )`, before); err != nil {
		return total, fmt.Errorf("storage: prune context states: %w", err)
	}
	return total, nil
}

// CountContextBefore returns the number of entries that DeleteContextBefore
// would delete.
func (s *SQLiteStore) CountContextBefore(ctx context.Context, before time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM context_entries
		WHERE session_id IN (
			SELECT session_id FROM context_entries
			GROUP BY session_id
			HAVING MAX(timestamp) < ?
		)`,
		before,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count context entries: %w", err)
	}
	return n, nil
}

func entToContextEntry(e *ent.ContextEntry) *ContextEntryRow {
	row := &ContextEntryRow{
		ID:              e.ID,
		SessionID:       e.SessionID,
		EventID:         e.EventID,
		Sequence:        e.Sequence,
		Kind:            e.Kind,
		Timestamp:       e.Timestamp,
		ActionType:      e.ActionType,
		Tool:            e.Tool,
		ToolCallID:      e.ToolCallID,
		Phase:           e.Phase,
		TargetHost:      e.TargetHost,
		TargetMCPServer: e.TargetMcpServer,
		TargetMCPTool:   e.TargetMcpTool,
		Origin:          e.Origin,
		Tags:            e.Tags,
		Classifications: e.Classifications,
		InjectionScore:  e.InjectionScore,
		Decision:        e.Decision,
		MatchedRuleIDs:  e.MatchedRuleIds,
		ContentDigest:   e.ContentDigest,
		ResultStatus:    e.ResultStatus,
		DurationMS:      e.DurationMs,
		ErrorMessage:    e.ErrorMessage,
		HashVersion:     e.HashVersion,
		PrevHash:        e.PrevHash,
		Hash:            e.Hash,
	}
	if e.LinkedEventID != nil {
		row.LinkedEventID = *e.LinkedEventID
	}
	return row
}

func jsonOrNil(values []string) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal string slice: %w", err)
	}
	return string(b), nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nilUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func nilBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nilPtr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/aarmreceipt"
)

const (
	receiptResultStatusPending = "pending"
	receiptResultStatusSuccess = "success"

	// receiptListMaxLimit caps QueryReceipts to protect against accidental
	// full-table materialization when callers pass a non-positive limit.
	receiptListMaxLimit = 5000
)

// InsertReceipt persists a fully-prepared receipt row. The caller (the
// receipt generator) is responsible for computing sequence, prev_hash, and
// hash; this function just records the row. For race-free same-session
// inserts, prefer RecordReceiptInTx, which performs the read+build+insert
// inside a single writer transaction.
func (s *SQLiteStore) InsertReceipt(ctx context.Context, row *ReceiptRow) error {
	if err := normalizeReceiptForInsert(row); err != nil {
		return err
	}
	if _, err := receiptCreate(s.client.AarmReceipt, row).Save(ctx); err != nil {
		return fmt.Errorf("storage: insert receipt: %w", err)
	}
	return nil
}

// RecordReceiptInTx opens a single writer transaction, reads the highest
// existing sequence for sessionID, hands it to build, then inserts the
// returned row. This eliminates the same-session race where two goroutines
// observe the same last.Sequence and both compute the same nextSeq.
//
// receiptWriteMu serializes the SELECT-then-INSERT path so two concurrent
// writers cannot both acquire a read snapshot before one upgrades to a
// writer (which would surface as SQLITE_BUSY under WAL).
func (s *SQLiteStore) RecordReceiptInTx(ctx context.Context, sessionID uuid.UUID, build func(prev *ReceiptRow) (*ReceiptRow, error)) (*ReceiptRow, error) {
	if sessionID == uuid.Nil {
		return nil, fmt.Errorf("storage: RecordReceiptInTx: nil session ID")
	}
	if build == nil {
		return nil, fmt.Errorf("storage: RecordReceiptInTx: nil build func")
	}

	s.receiptWriteMu.Lock()
	defer s.receiptWriteMu.Unlock()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("storage: begin tx for receipt insert: %w", err)
	}

	var prev *ReceiptRow
	prev, err = readLastReceiptTx(ctx, tx, sessionID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("storage: read last receipt: %w", err)
	}

	row, err := build(prev)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := normalizeReceiptForInsert(row); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := insertReceiptTx(ctx, tx, row); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("storage: insert receipt: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("storage: commit receipt insert: %w", err)
	}
	return row, nil
}

// readLastReceiptTx fetches the highest-sequence receipt for sessionID inside
// an open sql.Tx, returning (nil, nil) when the session has no receipts yet.
func readLastReceiptTx(ctx context.Context, tx *sql.Tx, sessionID uuid.UUID) (*ReceiptRow, error) {
	const q = `
SELECT id, session_id, action_id, event_id, recorded_at, sequence,
       agent, tool, action_type, project, decision, severity, message,
       result_status, prev_hash, hash
FROM aarm_receipts
WHERE session_id = ?
ORDER BY sequence DESC
LIMIT 1`
	row := tx.QueryRowContext(ctx, q, sessionID)
	var (
		r            ReceiptRow
		actionID     uuid.NullUUID
		eventID      uuid.NullUUID
		agent        sql.NullString
		tool         sql.NullString
		project      sql.NullString
		severity     sql.NullString
		message      sql.NullString
		resultStatus string
		prevHash     []byte
	)
	if err := row.Scan(
		&r.ID, &r.SessionID, &actionID, &eventID, &r.RecordedAt, &r.Sequence,
		&agent, &tool, &r.ActionType, &project, &r.Decision, &severity, &message,
		&resultStatus, &prevHash, &r.Hash,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if actionID.Valid {
		r.ActionID = actionID.UUID
	}
	if eventID.Valid {
		r.EventID = eventID.UUID
	}
	if agent.Valid {
		r.Agent = agent.String
	}
	if tool.Valid {
		r.Tool = tool.String
	}
	if project.Valid {
		r.Project = project.String
	}
	if severity.Valid {
		r.Severity = severity.String
	}
	if message.Valid {
		r.Message = message.String
	}
	r.ResultStatus = resultStatus
	r.PrevHash = prevHash
	return &r, nil
}

// insertReceiptTx writes a fully-normalized receipt row inside an open sql.Tx.
// Mirrors the ent-driven create in receiptCreate but routes through the same
// transaction that holds the writer lock.
func insertReceiptTx(ctx context.Context, tx *sql.Tx, row *ReceiptRow) error {
	const stmt = `
INSERT INTO aarm_receipts (
    id, session_id, action_id, event_id, recorded_at, sequence,
    agent, tool, action_type, project,
    decision, matched_rule_ids, severity, message,
    result_status, duration_ms, error_message,
    snapshot, action_payload, prev_hash, hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	var actionIDArg, eventIDArg, agentArg, toolArg, projectArg interface{}
	if row.ActionID != uuid.Nil {
		actionIDArg = row.ActionID
	}
	if row.EventID != uuid.Nil {
		eventIDArg = row.EventID
	}
	if row.Agent != "" {
		agentArg = row.Agent
	}
	if row.Tool != "" {
		toolArg = row.Tool
	}
	if row.Project != "" {
		projectArg = row.Project
	}

	var ruleIDsArg interface{}
	if len(row.MatchedRuleIDs) > 0 {
		buf, err := json.Marshal(row.MatchedRuleIDs)
		if err != nil {
			return fmt.Errorf("marshal matched_rule_ids: %w", err)
		}
		ruleIDsArg = string(buf)
	}

	var severityArg, messageArg interface{}
	if row.Severity != "" {
		severityArg = row.Severity
	}
	if row.Message != "" {
		messageArg = row.Message
	}

	var durationArg interface{}
	if row.DurationMS != nil {
		durationArg = *row.DurationMS
	}
	var errorMsgArg interface{}
	if row.ErrorMessage != "" {
		errorMsgArg = row.ErrorMessage
	}

	var snapshotArg interface{}
	if row.Snapshot != nil {
		buf, err := json.Marshal(row.Snapshot)
		if err != nil {
			return fmt.Errorf("marshal snapshot: %w", err)
		}
		snapshotArg = string(buf)
	}
	var payloadArg interface{}
	if row.ActionPayload != nil {
		buf, err := json.Marshal(row.ActionPayload)
		if err != nil {
			return fmt.Errorf("marshal action_payload: %w", err)
		}
		payloadArg = string(buf)
	}
	var prevHashArg interface{}
	if len(row.PrevHash) > 0 {
		prevHashArg = row.PrevHash
	}

	_, err := tx.ExecContext(ctx, stmt,
		row.ID, row.SessionID, actionIDArg, eventIDArg, row.RecordedAt, row.Sequence,
		agentArg, toolArg, row.ActionType, projectArg,
		row.Decision, ruleIDsArg, severityArg, messageArg,
		row.ResultStatus, durationArg, errorMsgArg,
		snapshotArg, payloadArg, prevHashArg, row.Hash,
	)
	return err
}

func normalizeReceiptForInsert(row *ReceiptRow) error {
	if row == nil {
		return fmt.Errorf("storage: InsertReceipt: nil row")
	}
	if row.SessionID == uuid.Nil {
		return fmt.Errorf("storage: InsertReceipt: nil session ID")
	}
	if row.Sequence <= 0 {
		return fmt.Errorf("storage: InsertReceipt: sequence must be positive, got %d", row.Sequence)
	}
	if len(row.Hash) == 0 {
		return fmt.Errorf("storage: InsertReceipt: hash is required")
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.RecordedAt.IsZero() {
		row.RecordedAt = time.Now().UTC()
	}
	if row.ResultStatus == "" {
		row.ResultStatus = receiptResultStatusPending
	}
	if row.ActionType == "" {
		row.ActionType = "unknown"
	}
	if row.Decision == "" {
		row.Decision = "unknown"
	}
	return nil
}

func receiptCreate(client *ent.AarmReceiptClient, row *ReceiptRow) *ent.AarmReceiptCreate {
	create := client.Create().
		SetID(row.ID).
		SetSessionID(row.SessionID).
		SetRecordedAt(row.RecordedAt).
		SetSequence(row.Sequence).
		SetActionType(row.ActionType).
		SetDecision(row.Decision).
		SetResultStatus(aarmreceipt.ResultStatus(row.ResultStatus)).
		SetHash(row.Hash)

	if row.ActionID != uuid.Nil {
		create.SetActionID(row.ActionID)
	}
	if row.EventID != uuid.Nil {
		create.SetEventID(row.EventID)
	}
	if row.Agent != "" {
		create.SetAgent(row.Agent)
	}
	if row.Tool != "" {
		create.SetTool(row.Tool)
	}
	if row.Project != "" {
		create.SetProject(row.Project)
	}
	if len(row.MatchedRuleIDs) > 0 {
		create.SetMatchedRuleIds(row.MatchedRuleIDs)
	}
	if row.Severity != "" {
		create.SetSeverity(row.Severity)
	}
	if row.Message != "" {
		create.SetMessage(row.Message)
	}
	if row.DurationMS != nil {
		create.SetDurationMs(*row.DurationMS)
	}
	if row.ErrorMessage != "" {
		create.SetErrorMessage(row.ErrorMessage)
	}
	if row.Snapshot != nil {
		create.SetSnapshot(row.Snapshot)
	}
	if row.ActionPayload != nil {
		create.SetActionPayload(row.ActionPayload)
	}
	if len(row.PrevHash) > 0 {
		create.SetPrevHash(row.PrevHash)
	}
	return create
}

// GetLastReceiptForSession returns the highest-sequence receipt for a
// session, or (nil, nil) when the session has no receipts yet.
func (s *SQLiteStore) GetLastReceiptForSession(ctx context.Context, sessionID uuid.UUID) (*ReceiptRow, error) {
	r, err := s.client.AarmReceipt.Query().
		Where(aarmreceipt.SessionIDEQ(sessionID)).
		Order(aarmreceipt.BySequence(entsql.OrderDesc())).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get last receipt: %w", err)
	}
	return entToReceipt(r), nil
}

// UpdateReceiptResult transitions a receipt row from pending to a final
// result_status. The pair (session_id, sequence) is the natural key.
func (s *SQLiteStore) UpdateReceiptResult(ctx context.Context, sessionID uuid.UUID, sequence int64, status string, durationMS int64, errorMsg string) error {
	if sessionID == uuid.Nil {
		return fmt.Errorf("storage: UpdateReceiptResult: nil session ID")
	}
	if status == "" {
		status = receiptResultStatusSuccess
	}

	rows, err := s.client.AarmReceipt.Query().
		Where(
			aarmreceipt.SessionIDEQ(sessionID),
			aarmreceipt.SequenceEQ(sequence),
		).
		Limit(1).
		All(ctx)
	if err != nil {
		return fmt.Errorf("storage: lookup receipt: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	update := s.client.AarmReceipt.UpdateOneID(rows[0].ID).
		SetResultStatus(aarmreceipt.ResultStatus(status))
	if durationMS > 0 {
		update.SetDurationMs(durationMS)
	}
	if errorMsg != "" {
		update.SetErrorMessage(errorMsg)
	}
	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("storage: update receipt result: %w", err)
	}
	return nil
}

// QueryReceipts returns receipts matching filter. When SessionID is set, rows
// are ordered by (session_id, sequence) ASC. Otherwise rows are ordered by
// recorded_at DESC.
//
// Limit handling:
//   - filter.Limit > 0: capped at receiptListMaxLimit.
//   - filter.Limit == 0 (default): capped at receiptListMaxLimit.
//   - filter.Limit == -1: no LIMIT clause is applied. Reserved for admin
//     operations such as full hash-chain verification.
func (s *SQLiteStore) QueryReceipts(ctx context.Context, filter *ReceiptFilter) ([]*ReceiptRow, error) {
	if filter == nil {
		filter = &ReceiptFilter{}
	}
	q := s.client.AarmReceipt.Query()
	if filter.SessionID != nil {
		q.Where(aarmreceipt.SessionIDEQ(*filter.SessionID))
	}
	if filter.Decision != "" {
		q.Where(aarmreceipt.DecisionEQ(filter.Decision))
	}
	if filter.Since != nil {
		q.Where(aarmreceipt.RecordedAtGTE(*filter.Since))
	}
	if filter.Until != nil {
		q.Where(aarmreceipt.RecordedAtLTE(*filter.Until))
	}

	if filter.SessionID != nil {
		q.Order(aarmreceipt.BySequence(entsql.OrderAsc()))
	} else {
		q.Order(aarmreceipt.ByRecordedAt(entsql.OrderDesc()), aarmreceipt.ByID(entsql.OrderDesc()))
	}

	if filter.Limit != -1 {
		limit := filter.Limit
		if limit <= 0 || limit > receiptListMaxLimit {
			limit = receiptListMaxLimit
		}
		q.Limit(limit)
	}

	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query receipts: %w", err)
	}
	out := make([]*ReceiptRow, len(rows))
	for i, r := range rows {
		out[i] = entToReceipt(r)
	}
	return out, nil
}

// ListReceiptSessionIDs returns the distinct session IDs that appear in the
// receipt log. Intended for admin operations such as full-cluster hash-chain
// verification. Not for hot paths.
func (s *SQLiteStore) ListReceiptSessionIDs(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.client.AarmReceipt.Query().
		Unique(true).
		Select(aarmreceipt.FieldSessionID).
		Scan(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("storage: list receipt session IDs: %w", err)
	}
	return ids, nil
}

// CountReceipts returns the count of receipts matching filter (ignoring
// Limit and ordering).
func (s *SQLiteStore) CountReceipts(ctx context.Context, filter *ReceiptFilter) (int, error) {
	if filter == nil {
		filter = &ReceiptFilter{}
	}
	q := s.client.AarmReceipt.Query()
	if filter.SessionID != nil {
		q.Where(aarmreceipt.SessionIDEQ(*filter.SessionID))
	}
	if filter.Decision != "" {
		q.Where(aarmreceipt.DecisionEQ(filter.Decision))
	}
	if filter.Since != nil {
		q.Where(aarmreceipt.RecordedAtGTE(*filter.Since))
	}
	if filter.Until != nil {
		q.Where(aarmreceipt.RecordedAtLTE(*filter.Until))
	}
	n, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: count receipts: %w", err)
	}
	return n, nil
}

// DeleteReceiptsBefore deletes receipts older than before in fixed-size
// batches so a heavy retention sweep does not hold the SQLite writer lock
// for the entire purge. Returns the total number of rows deleted.
func (s *SQLiteStore) DeleteReceiptsBefore(ctx context.Context, before time.Time) (int, error) {
	const deleteBatch = 1000
	total := 0
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM aarm_receipts WHERE id IN (SELECT id FROM aarm_receipts WHERE recorded_at < ? LIMIT ?)`,
			before, deleteBatch,
		)
		if err != nil {
			return total, fmt.Errorf("storage: delete receipts: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("storage: delete receipts rows affected: %w", err)
		}
		total += int(n)
		if n == 0 {
			return total, nil
		}
	}
}

// CountReceiptsBefore returns the number of receipts older than before.
func (s *SQLiteStore) CountReceiptsBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := s.client.AarmReceipt.Query().
		Where(aarmreceipt.RecordedAtLT(before)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: count receipts: %w", err)
	}
	return n, nil
}

func entToReceipt(e *ent.AarmReceipt) *ReceiptRow {
	row := &ReceiptRow{
		ID:             e.ID,
		SessionID:      e.SessionID,
		ActionID:       e.ActionID,
		EventID:        e.EventID,
		RecordedAt:     e.RecordedAt,
		Sequence:       e.Sequence,
		Agent:          e.Agent,
		Tool:           e.Tool,
		ActionType:     e.ActionType,
		Project:        e.Project,
		Decision:       e.Decision,
		MatchedRuleIDs: e.MatchedRuleIds,
		Severity:       e.Severity,
		Message:        e.Message,
		ResultStatus:   string(e.ResultStatus),
		ErrorMessage:   e.ErrorMessage,
		Snapshot:       e.Snapshot,
		ActionPayload:  e.ActionPayload,
		PrevHash:       e.PrevHash,
		Hash:           e.Hash,
	}
	if e.DurationMs != nil {
		v := *e.DurationMs
		row.DurationMS = &v
	}
	return row
}

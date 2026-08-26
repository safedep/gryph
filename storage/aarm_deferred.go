package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/aarmdeferredaction"
)

const (
	deferredActionListMaxLimit = 5000
	deferredActionListDefault  = 100
)

// InsertDeferredAction persists a fully-prepared deferred-action row.
func (s *SQLiteStore) InsertDeferredAction(ctx context.Context, row *DeferredActionRow) error {
	if err := normalizeDeferredActionForInsert(row); err != nil {
		return err
	}
	create := s.client.AarmDeferredAction.Create().
		SetID(row.ID).
		SetSessionID(row.SessionID).
		SetReceiptSequence(row.ReceiptSequence).
		SetActionID(row.ActionID).
		SetDeferredAt(row.DeferredAt).
		SetExpiresAt(row.ExpiresAt).
		SetReason(row.Reason).
		SetStatus(aarmdeferredaction.Status(row.Status))
	if row.ResolvedAt != nil {
		create.SetResolvedAt(*row.ResolvedAt)
	}
	if row.Resolver != "" {
		create.SetResolver(row.Resolver)
	}
	if row.ResolutionNote != "" {
		create.SetResolutionNote(row.ResolutionNote)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("storage: insert deferred action: %w", err)
	}
	return nil
}

// GetDeferredAction returns the deferred-action row for the given id, or nil
// when not present.
func (s *SQLiteStore) GetDeferredAction(ctx context.Context, id uuid.UUID) (*DeferredActionRow, error) {
	row, err := s.client.AarmDeferredAction.Query().
		Where(aarmdeferredaction.IDEQ(id)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: get deferred action: %w", err)
	}
	return entToDeferredAction(row), nil
}

// GetDeferredActionByPrefix resolves a deferred-action row by id prefix.
// Returns (nil, nil) when no row matches.
func (s *SQLiteStore) GetDeferredActionByPrefix(ctx context.Context, prefix string) (*DeferredActionRow, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return nil, fmt.Errorf("storage: GetDeferredActionByPrefix: empty prefix")
	}
	if id, err := uuid.Parse(trimmed); err == nil {
		return s.GetDeferredAction(ctx, id)
	}
	rows, err := s.client.AarmDeferredAction.Query().
		Where(func(sel *entsql.Selector) {
			sel.Where(entsql.Like(aarmdeferredaction.FieldID, trimmed+"%"))
		}).
		Limit(2).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: get deferred action by prefix: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("storage: deferred-action prefix %q is ambiguous", trimmed)
	}
	return entToDeferredAction(rows[0]), nil
}

// QueryDeferredActions returns the deferred-action rows matching filter. Rows
// are ordered by deferred_at DESC.
func (s *SQLiteStore) QueryDeferredActions(ctx context.Context, filter *DeferredActionFilter) ([]*DeferredActionRow, error) {
	if filter == nil {
		filter = &DeferredActionFilter{}
	}
	q := s.client.AarmDeferredAction.Query()
	if filter.SessionID != nil {
		q.Where(aarmdeferredaction.SessionIDEQ(*filter.SessionID))
	}
	if filter.Status != "" {
		q.Where(aarmdeferredaction.StatusEQ(aarmdeferredaction.Status(filter.Status)))
	}
	if filter.ExpiredBefore != nil {
		q.Where(aarmdeferredaction.ExpiresAtLT(*filter.ExpiredBefore))
	}
	q.Order(aarmdeferredaction.ByDeferredAt(entsql.OrderDesc()), aarmdeferredaction.ByID(entsql.OrderDesc()))

	limit := filter.Limit
	if limit == 0 {
		limit = deferredActionListDefault
	}
	if limit != -1 {
		if limit <= 0 || limit > deferredActionListMaxLimit {
			limit = deferredActionListMaxLimit
		}
		q.Limit(limit)
	}

	rows, err := q.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: query deferred actions: %w", err)
	}
	out := make([]*DeferredActionRow, len(rows))
	for i, r := range rows {
		out[i] = entToDeferredAction(r)
	}
	return out, nil
}

// UpdateDeferredActionResolution transitions a deferred-action row to a
// terminal status. The status string must be one of the
// DeferredActionStatusResolved* constants. Returns a no-op error path when the
// row does not exist.
func (s *SQLiteStore) UpdateDeferredActionResolution(ctx context.Context, id uuid.UUID, status, resolver, note string, resolvedAt time.Time) error {
	if id == uuid.Nil {
		return fmt.Errorf("storage: UpdateDeferredActionResolution: nil id")
	}
	switch status {
	case DeferredActionStatusResolvedAllow, DeferredActionStatusResolvedDeny, DeferredActionStatusResolvedTimeout:
	default:
		return fmt.Errorf("storage: UpdateDeferredActionResolution: invalid status %q", status)
	}
	update := s.client.AarmDeferredAction.UpdateOneID(id).
		SetStatus(aarmdeferredaction.Status(status)).
		SetResolvedAt(resolvedAt)
	if resolver != "" {
		update.SetResolver(resolver)
	}
	if note != "" {
		update.SetResolutionNote(note)
	}
	if _, err := update.Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("storage: update deferred action: %w", err)
	}
	return nil
}

// DeleteDeferredActionsBefore deletes rows whose deferred_at predates the
// given cutoff in fixed-size batches. Returns the total number of rows
// deleted. Mirrors DeleteReceiptsBefore so the retention sweep can prune the
// queue alongside its referenced receipts.
func (s *SQLiteStore) DeleteDeferredActionsBefore(ctx context.Context, before time.Time) (int, error) {
	const deleteBatch = 1000
	total := 0
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM aarm_deferred_actions WHERE id IN (SELECT id FROM aarm_deferred_actions WHERE deferred_at < ? LIMIT ?)`,
			before, deleteBatch,
		)
		if err != nil {
			return total, fmt.Errorf("storage: delete deferred actions: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("storage: delete deferred actions rows affected: %w", err)
		}
		total += int(n)
		if n == 0 {
			return total, nil
		}
	}
}

// CountDeferredActionsBefore returns the number of deferred-action rows older
// than before.
func (s *SQLiteStore) CountDeferredActionsBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := s.client.AarmDeferredAction.Query().
		Where(aarmdeferredaction.DeferredAtLT(before)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: count deferred actions: %w", err)
	}
	return n, nil
}

func normalizeDeferredActionForInsert(row *DeferredActionRow) error {
	if row == nil {
		return fmt.Errorf("storage: InsertDeferredAction: nil row")
	}
	if row.SessionID == uuid.Nil {
		return fmt.Errorf("storage: InsertDeferredAction: nil session ID")
	}
	if row.ReceiptSequence <= 0 {
		return fmt.Errorf("storage: InsertDeferredAction: receipt_sequence must be positive, got %d", row.ReceiptSequence)
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.DeferredAt.IsZero() {
		row.DeferredAt = time.Now().UTC()
	}
	if row.ExpiresAt.IsZero() {
		return fmt.Errorf("storage: InsertDeferredAction: expires_at is required")
	}
	if row.Reason == "" {
		return fmt.Errorf("storage: InsertDeferredAction: reason is required")
	}
	if row.Status == "" {
		row.Status = DeferredActionStatusPending
	}
	return nil
}

func entToDeferredAction(e *ent.AarmDeferredAction) *DeferredActionRow {
	row := &DeferredActionRow{
		ID:              e.ID,
		SessionID:       e.SessionID,
		ReceiptSequence: e.ReceiptSequence,
		ActionID:        e.ActionID,
		DeferredAt:      e.DeferredAt,
		ExpiresAt:       e.ExpiresAt,
		Reason:          e.Reason,
		Status:          string(e.Status),
		Resolver:        e.Resolver,
		ResolutionNote:  e.ResolutionNote,
	}
	if e.ResolvedAt != nil {
		t := *e.ResolvedAt
		row.ResolvedAt = &t
	}
	return row
}

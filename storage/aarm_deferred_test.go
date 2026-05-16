package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDeferredRow(sessionID uuid.UUID, seq int64) *storage.DeferredActionRow {
	return &storage.DeferredActionRow{
		SessionID:       sessionID,
		ReceiptSequence: seq,
		ActionID:        uuid.New(),
		DeferredAt:      time.Now().UTC().Truncate(time.Microsecond),
		ExpiresAt:       time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond),
		Reason:          "wait_for_classification",
	}
}

func TestInsertAndGetDeferredAction(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	row := makeDeferredRow(sessionID, 1)
	require.NoError(t, store.InsertDeferredAction(ctx, row))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, row.ID, got.ID)
	assert.Equal(t, sessionID, got.SessionID)
	assert.Equal(t, int64(1), got.ReceiptSequence)
	assert.Equal(t, storage.DeferredActionStatusPending, got.Status)
	assert.Equal(t, "wait_for_classification", got.Reason)
}

func TestGetDeferredAction_NotFound(t *testing.T) {
	store := storagetest.NewStore(t)
	got, err := store.GetDeferredAction(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetDeferredActionByPrefix(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := makeDeferredRow(sessionID, 1)
	require.NoError(t, store.InsertDeferredAction(ctx, row))

	got, err := store.GetDeferredActionByPrefix(ctx, row.ID.String()[:8])
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, row.ID, got.ID)

	_, err = store.GetDeferredActionByPrefix(ctx, "")
	require.Error(t, err)

	missing, err := store.GetDeferredActionByPrefix(ctx, "ffffffff")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestQueryDeferredActions_FilterByStatus(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	for i := int64(1); i <= 3; i++ {
		require.NoError(t, store.InsertDeferredAction(ctx, makeDeferredRow(sessionID, i)))
	}

	rows, err := store.QueryDeferredActions(ctx, &storage.DeferredActionFilter{
		Status: storage.DeferredActionStatusPending,
	})
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	rows, err = store.QueryDeferredActions(ctx, &storage.DeferredActionFilter{
		Status: storage.DeferredActionStatusResolvedAllow,
	})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestUpdateDeferredActionResolution(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	row := makeDeferredRow(sessionID, 1)
	require.NoError(t, store.InsertDeferredAction(ctx, row))

	resolvedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.UpdateDeferredActionResolution(ctx, row.ID,
		storage.DeferredActionStatusResolvedAllow, "alice", "approved manually", resolvedAt))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedAllow, got.Status)
	assert.Equal(t, "alice", got.Resolver)
	assert.Equal(t, "approved manually", got.ResolutionNote)
	require.NotNil(t, got.ResolvedAt)
	assert.WithinDuration(t, resolvedAt, *got.ResolvedAt, time.Second)
}

func TestUpdateDeferredActionResolution_RejectsInvalidStatus(t *testing.T) {
	store := storagetest.NewStore(t)
	err := store.UpdateDeferredActionResolution(context.Background(), uuid.New(),
		"resolved_bogus", "alice", "", time.Now())
	require.Error(t, err)
}

func TestQueryDeferredActions_ExpiredBefore(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	expired := makeDeferredRow(sessionID, 1)
	expired.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)
	require.NoError(t, store.InsertDeferredAction(ctx, expired))

	fresh := makeDeferredRow(sessionID, 2)
	require.NoError(t, store.InsertDeferredAction(ctx, fresh))

	cutoff := time.Now().UTC()
	rows, err := store.QueryDeferredActions(ctx, &storage.DeferredActionFilter{
		Status:        storage.DeferredActionStatusPending,
		ExpiredBefore: &cutoff,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, expired.ID, rows[0].ID)
}

func TestDeleteDeferredActionsBefore(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	old := makeDeferredRow(sessionID, 1)
	old.DeferredAt = time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, store.InsertDeferredAction(ctx, old))

	fresh := makeDeferredRow(sessionID, 2)
	require.NoError(t, store.InsertDeferredAction(ctx, fresh))

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	count, err := store.CountDeferredActionsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	deleted, err := store.DeleteDeferredActionsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	remaining, err := store.QueryDeferredActions(ctx, &storage.DeferredActionFilter{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, fresh.ID, remaining[0].ID)
}

func TestInsertDeferredAction_RejectsDuplicateSessionSequence(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	require.NoError(t, store.InsertDeferredAction(ctx, makeDeferredRow(sessionID, 1)))
	err := store.InsertDeferredAction(ctx, makeDeferredRow(sessionID, 1))
	require.Error(t, err)
}

func TestReceiptRow_DeferFieldsRoundTrip(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	originalSeq := int64(1)
	deferral := int64(7)
	row := &storage.ReceiptRow{
		SessionID:          sessionID,
		Sequence:           originalSeq,
		RecordedAt:         time.Now().UTC().Truncate(time.Microsecond),
		ActionType:         "file_write",
		Decision:           "defer",
		ResultStatus:       "deferred",
		DeferReason:        "wait_for_classification",
		DeferralOfSequence: &deferral,
		Hash:               []byte("00000000000000000000000000000099"),
	}
	require.NoError(t, store.InsertReceipt(ctx, row))

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]
	assert.Equal(t, "wait_for_classification", got.DeferReason)
	require.NotNil(t, got.DeferralOfSequence)
	assert.Equal(t, int64(7), *got.DeferralOfSequence)
}

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

func makeReceiptRow(sessionID uuid.UUID, sequence int64) *storage.ReceiptRow {
	return &storage.ReceiptRow{
		SessionID:    sessionID,
		Sequence:     sequence,
		RecordedAt:   time.Now().UTC().Truncate(time.Microsecond),
		ActionType:   "file_read",
		Agent:        "claude-code",
		Tool:         "Read",
		Decision:     "block",
		ResultStatus: "blocked",
		Hash:         []byte("deadbeefdeadbeefdeadbeefdeadbeef"),
	}
}

func TestInsertReceipt_AndGetLast(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	last, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Nil(t, last)

	row := makeReceiptRow(sessionID, 1)
	require.NoError(t, store.InsertReceipt(ctx, row))

	got, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(1), got.Sequence)
	assert.Equal(t, row.Hash, got.Hash)

	row2 := makeReceiptRow(sessionID, 2)
	row2.PrevHash = row.Hash
	row2.Hash = []byte("00000000000000000000000000000002")
	require.NoError(t, store.InsertReceipt(ctx, row2))

	got, err = store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(2), got.Sequence)
}

func TestInsertReceipt_RejectsDuplicateSequence(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionID, 1)))
	err := store.InsertReceipt(ctx, makeReceiptRow(sessionID, 1))
	require.Error(t, err)
}

func TestUpdateReceiptResult(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	row := makeReceiptRow(sessionID, 1)
	row.ResultStatus = "pending"
	require.NoError(t, store.InsertReceipt(ctx, row))

	require.NoError(t, store.UpdateReceiptResult(ctx, sessionID, 1, "success", 250, ""))

	got, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "success", got.ResultStatus)
	require.NotNil(t, got.DurationMS)
	assert.Equal(t, int64(250), *got.DurationMS)
}

func TestUpdateReceiptResult_NoOpWhenMissing(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	require.NoError(t, store.UpdateReceiptResult(ctx, uuid.New(), 99, "success", 0, ""))
}

func TestQueryReceipts_BySession_OrderedAscending(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessionA := uuid.New()
	sessionB := uuid.New()
	for i := int64(1); i <= 3; i++ {
		require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionA, i)))
		require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionB, i)))
	}

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionA})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, int64(1), rows[0].Sequence)
	assert.Equal(t, int64(2), rows[1].Sequence)
	assert.Equal(t, int64(3), rows[2].Sequence)
	for _, r := range rows {
		assert.Equal(t, sessionA, r.SessionID)
	}
}

func TestQueryReceipts_FilterByDecision(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	r1 := makeReceiptRow(sessionID, 1)
	r1.Decision = "block"
	r2 := makeReceiptRow(sessionID, 2)
	r2.Decision = "guidance"
	require.NoError(t, store.InsertReceipt(ctx, r1))
	require.NoError(t, store.InsertReceipt(ctx, r2))

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: "block"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "block", rows[0].Decision)
}

func TestCountReceipts(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	for i := int64(1); i <= 5; i++ {
		require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionID, i)))
	}

	n, err := store.CountReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Equal(t, 5, n)
}

func TestQueryReceipts_LimitMinusOneReturnsAll(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	const total = 12
	for i := int64(1); i <= total; i++ {
		require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionID, i)))
	}

	capped, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: 5})
	require.NoError(t, err)
	assert.Len(t, capped, 5, "positive limit must be respected")

	unbounded, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	assert.Len(t, unbounded, total, "Limit: -1 must return every row regardless of default cap")
}

func TestListReceiptSessionIDs(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	got, err := store.ListReceiptSessionIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)

	sessionA := uuid.New()
	sessionB := uuid.New()
	require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionA, 1)))
	require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionA, 2)))
	require.NoError(t, store.InsertReceipt(ctx, makeReceiptRow(sessionB, 1)))

	got, err = store.ListReceiptSessionIDs(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{sessionA, sessionB}, got)
}

func TestDeleteReceiptsBefore(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	old := makeReceiptRow(sessionID, 1)
	old.RecordedAt = time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, store.InsertReceipt(ctx, old))

	fresh := makeReceiptRow(sessionID, 2)
	require.NoError(t, store.InsertReceipt(ctx, fresh))

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	n, err := store.CountReceiptsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	deleted, err := store.DeleteReceiptsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	remaining, err := store.CountReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Equal(t, 1, remaining)
}

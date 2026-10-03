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

func makeRequest(sessionID uuid.UUID, seq int64) *storage.ApprovalRequestRow {
	return &storage.ApprovalRequestRow{
		SessionID:       sessionID,
		ActionID:        uuid.New(),
		ReceiptSequence: seq,
		ActionDigest:    "sha256:abc",
		RuleIDs:         []string{"r1"},
		Summary:         "command_exec: make deploy",
		MinAssurance:    "local-admin",
		ExpiresAt:       time.Now().Add(time.Hour),
	}
}

func TestApprovalRequest_DecidedOnce(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := makeRequest(sessionID, 1)
	require.NoError(t, store.InsertApprovalRequest(ctx, row))

	got, err := store.GetApprovalRequestByPrefix(ctx, row.ID.String()[:8])
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.ApprovalRequestPending, got.State)
	assert.Equal(t, []string{"r1"}, got.RuleIDs)

	res := storage.ApprovalResolution{State: storage.ApprovalRequestApproved, Channel: "local-admin", Assurance: "local-admin", Approver: "admin", PeerTrust: "unknown", Note: "ok", Scope: "once"}
	require.NoError(t, store.ResolveApprovalRequest(ctx, row.ID, res))
	err = store.ResolveApprovalRequest(ctx, row.ID, storage.ApprovalResolution{State: storage.ApprovalRequestDenied})
	assert.ErrorIs(t, err, storage.ErrApprovalRequestDecided, "the first answer wins")

	got, err = store.GetApprovalRequest(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRequestApproved, got.State)
	assert.Equal(t, "admin", got.Approver)
	assert.NotNil(t, got.DecidedAt)
	assert.Nil(t, got.NotifiedAt)

	unnotified, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{SessionID: &sessionID, Unnotified: true})
	require.NoError(t, err)
	require.Len(t, unnotified, 1)
	require.NoError(t, store.MarkApprovalRequestNotified(ctx, row.ID, time.Now()))
	unnotified, err = store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{SessionID: &sessionID, Unnotified: true})
	require.NoError(t, err)
	assert.Empty(t, unnotified)

	assert.Error(t, store.ResolveApprovalRequest(ctx, row.ID, storage.ApprovalResolution{State: "bogus"}))
}

func TestApprovalRequest_ExpiredFilter(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	old := makeRequest(sessionID, 1)
	old.ExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, store.InsertApprovalRequest(ctx, old))
	require.NoError(t, store.InsertApprovalRequest(ctx, makeRequest(sessionID, 2)))
	now := time.Now()
	rows, err := store.QueryApprovalRequests(ctx, &storage.ApprovalRequestFilter{State: storage.ApprovalRequestPending, ExpiredBefore: &now})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, old.ID, rows[0].ID)
}

func TestApprovalGrant_MatchByScope(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	other := uuid.New()
	now := time.Now()
	insert := func(scope string, session uuid.UUID, expires time.Time) uuid.UUID {
		row := &storage.ApprovalGrantRow{RequestID: uuid.New(), SessionID: session, ActionDigest: "sha256:d", Scope: scope, ExpiresAt: expires, Approver: "admin"}
		require.NoError(t, store.InsertApprovalGrant(ctx, row))
		return row.ID
	}

	none, err := store.MatchApprovalGrant(ctx, sessionID, "sha256:d", now)
	require.NoError(t, err)
	assert.Nil(t, none)

	once := insert("once", sessionID, now.Add(time.Hour))
	g, err := store.MatchApprovalGrant(ctx, sessionID, "sha256:d", now)
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, once, g.ID)
	none, err = store.MatchApprovalGrant(ctx, other, "sha256:d", now)
	require.NoError(t, err)
	assert.Nil(t, none, "a once grant binds to its session")
	require.NoError(t, store.UseApprovalGrant(ctx, once, now))
	none, err = store.MatchApprovalGrant(ctx, sessionID, "sha256:d", now)
	require.NoError(t, err)
	assert.Nil(t, none, "a used once grant is gone")

	sess := insert("session", sessionID, now.Add(time.Hour))
	g, err = store.MatchApprovalGrant(ctx, sessionID, "sha256:d", now)
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, sess, g.ID)
	require.NoError(t, store.UseApprovalGrant(ctx, sess, now))
	g, err = store.MatchApprovalGrant(ctx, sessionID, "sha256:d", now)
	require.NoError(t, err)
	require.NotNil(t, g, "a session grant matches again")
	assert.Equal(t, 1, g.Uses)
	none, err = store.MatchApprovalGrant(ctx, other, "sha256:d", now)
	require.NoError(t, err)
	assert.Nil(t, none)

	window := insert("window", other, now.Add(time.Hour))
	g, err = store.MatchApprovalGrant(ctx, uuid.New(), "sha256:d", now)
	require.NoError(t, err)
	require.NotNil(t, g, "a window grant matches every session of the account")
	assert.Equal(t, window, g.ID)
	none, err = store.MatchApprovalGrant(ctx, uuid.New(), "sha256:d", now.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Nil(t, none, "an expired grant never matches")
	none, err = store.MatchApprovalGrant(ctx, sessionID, "sha256:other", now)
	require.NoError(t, err)
	assert.Nil(t, none, "another digest is another action")
}

func TestReceipt_ApprovalMetadata(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	_, err := store.RecordReceiptInTx(ctx, sessionID, func(_ *storage.ReceiptRow) (*storage.ReceiptRow, error) {
		return &storage.ReceiptRow{ID: uuid.New(), SessionID: sessionID, Sequence: 1, RecordedAt: time.Now(), ActionType: "command_exec", Decision: "escalate", Hash: make([]byte, 32)}, nil
	})
	require.NoError(t, err)
	require.NoError(t, store.UpdateReceiptApproval(ctx, sessionID, 1, map[string]interface{}{"channel": "same-user-tty", "approver": "dev"}))
	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "same-user-tty", rows[0].Approval["channel"])
}

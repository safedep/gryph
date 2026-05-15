package cli

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAarmSessionID_FullUUID(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	id := uuid.New()

	got, err := resolveAarmSessionID(ctx, store, id.String())
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveAarmSessionID_PrefixFindsContextStateWithoutSession(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sessionID := uuid.New()
	require.NoError(t, store.AppendContextAction(ctx, &storage.ContextActionRow{
		SessionID:  sessionID,
		Timestamp:  now,
		ActionType: "file_read",
		Tool:       "Read",
		Agent:      "claude-code",
	}))

	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Nil(t, sess, "sessions row must not exist for this test case")

	got, err := resolveAarmSessionID(ctx, store, sessionID.String()[:8])
	require.NoError(t, err)
	assert.Equal(t, sessionID, got)
}

func TestResolveAarmSessionID_FallsBackToSession(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessionID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, session.NewSessionWithID(sessionID, "claude-code")))

	got, err := resolveAarmSessionID(ctx, store, sessionID.String()[:8])
	require.NoError(t, err)
	assert.Equal(t, sessionID, got)
}

func TestResolveAarmSessionID_NoMatch(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	_, err := resolveAarmSessionID(ctx, store, "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session or context state matches")
}

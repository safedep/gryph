package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_GetContextStateByPrefix(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sessionID := uuid.New()
	require.NoError(t, store.AppendContextAction(ctx, &ContextActionRow{
		SessionID:  sessionID,
		Timestamp:  now,
		ActionType: string(events.ActionFileRead),
		Tool:       "Read",
		Agent:      "claude-code",
	}))

	t.Run("full id matches", func(t *testing.T) {
		got, err := store.GetContextStateByPrefix(ctx, sessionID.String())
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, sessionID, got.SessionID)
	})

	t.Run("8-char prefix matches", func(t *testing.T) {
		got, err := store.GetContextStateByPrefix(ctx, sessionID.String()[:8])
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, sessionID, got.SessionID)
	})

	t.Run("non-matching prefix returns nil", func(t *testing.T) {
		got, err := store.GetContextStateByPrefix(ctx, "ffffffff-ffff-ffff")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("works without a sessions row", func(t *testing.T) {
		sess, err := store.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.Nil(t, sess)

		got, err := store.GetContextStateByPrefix(ctx, sessionID.String()[:8])
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, sessionID, got.SessionID)
	})
}

func TestSQLiteStore_GetContextStateByPrefix_Ambiguous(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	var a, b uuid.UUID
	for i := 0; i < 64; i++ {
		a = uuid.New()
		b = uuid.New()
		if a.String()[0] == b.String()[0] && a != b {
			break
		}
	}
	require.Equal(t, a.String()[0], b.String()[0], "could not synthesize two UUIDs sharing a leading hex char")

	for _, id := range []uuid.UUID{a, b} {
		require.NoError(t, store.AppendContextAction(ctx, &ContextActionRow{
			SessionID:  id,
			Timestamp:  now,
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
		}))
	}

	got, err := store.GetContextStateByPrefix(ctx, a.String()[:1])
	assert.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

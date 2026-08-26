package storage

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
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

func TestAppendContextAction_ChainConstruction(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	row1 := &ContextActionRow{
		SessionID:  sessionID,
		Timestamp:  now,
		ActionType: string(events.ActionFileRead),
		Tool:       "Read",
		Agent:      "claude-code",
	}
	require.NoError(t, store.AppendContextAction(ctx, row1))
	require.NotNil(t, row1.Sequence)
	assert.Equal(t, int64(1), *row1.Sequence)
	assert.Empty(t, row1.PrevHash)
	require.Len(t, row1.Hash, contextchain.HashSize)

	row2 := &ContextActionRow{
		SessionID:  sessionID,
		Timestamp:  now.Add(1 * time.Millisecond),
		ActionType: string(events.ActionFileWrite),
		Tool:       "Edit",
		Agent:      "claude-code",
	}
	require.NoError(t, store.AppendContextAction(ctx, row2))
	require.NotNil(t, row2.Sequence)
	assert.Equal(t, int64(2), *row2.Sequence)
	assert.True(t, bytes.Equal(row2.PrevHash, row1.Hash), "row2.prev_hash must equal row1.hash")
	require.Len(t, row2.Hash, contextchain.HashSize)
	assert.False(t, bytes.Equal(row1.Hash, row2.Hash), "chained rows produce distinct hashes")
}

func TestAppendContextAction_SequenceMonotonic(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	base := time.Now().UTC().Truncate(time.Millisecond)

	for i := 0; i < 5; i++ {
		row := &ContextActionRow{
			SessionID:  sessionID,
			Timestamp:  base.Add(time.Duration(i) * time.Millisecond),
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
		}
		require.NoError(t, store.AppendContextAction(ctx, row))
		require.NotNil(t, row.Sequence)
		assert.Equal(t, int64(i+1), *row.Sequence)
	}

	rows, err := store.QueryContextActionsFiltered(ctx, &ContextActionFilter{
		SessionID: &sessionID,
		Limit:     -1,
		Ascending: true,
	})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for i, r := range rows {
		require.NotNil(t, r.Sequence)
		assert.Equal(t, int64(i+1), *r.Sequence)
	}
}

func TestAppendContextAction_ConcurrentSameSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	const writers = 16

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			row := &ContextActionRow{
				SessionID:  sessionID,
				Timestamp:  time.Now().UTC(),
				ActionType: string(events.ActionFileRead),
				Tool:       "Read",
			}
			if err := store.AppendContextAction(ctx, row); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	rows, err := store.QueryContextActionsFiltered(ctx, &ContextActionFilter{
		SessionID: &sessionID,
		Limit:     -1,
		Ascending: true,
	})
	require.NoError(t, err)
	require.Len(t, rows, writers)

	seen := map[int64]bool{}
	var prevHash []byte
	for i, r := range rows {
		require.NotNil(t, r.Sequence, "row %d missing sequence", i)
		seq := *r.Sequence
		assert.Falsef(t, seen[seq], "duplicate sequence %d", seq)
		seen[seq] = true
		assert.Equal(t, int64(i+1), seq)
		if i == 0 {
			assert.Empty(t, r.PrevHash, "first row prev_hash must be empty")
		} else {
			assert.True(t, bytes.Equal(r.PrevHash, prevHash), "row %d prev_hash mismatch", i)
		}
		prevHash = r.Hash
	}
}

func TestAppendContextAction_CrossSessionIsolation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionA := uuid.New()
	sessionB := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i := 0; i < 3; i++ {
		require.NoError(t, store.AppendContextAction(ctx, &ContextActionRow{
			SessionID:  sessionA,
			Timestamp:  now.Add(time.Duration(i) * time.Millisecond),
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
		}))
		require.NoError(t, store.AppendContextAction(ctx, &ContextActionRow{
			SessionID:  sessionB,
			Timestamp:  now.Add(time.Duration(i) * time.Millisecond),
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
		}))
	}

	rowsA, err := store.QueryContextActionsFiltered(ctx, &ContextActionFilter{
		SessionID: &sessionA,
		Limit:     -1,
		Ascending: true,
	})
	require.NoError(t, err)
	require.Len(t, rowsA, 3)
	rowsB, err := store.QueryContextActionsFiltered(ctx, &ContextActionFilter{
		SessionID: &sessionB,
		Limit:     -1,
		Ascending: true,
	})
	require.NoError(t, err)
	require.Len(t, rowsB, 3)

	for i := 0; i < 3; i++ {
		require.NotNil(t, rowsA[i].Sequence)
		require.NotNil(t, rowsB[i].Sequence)
		assert.Equal(t, int64(i+1), *rowsA[i].Sequence)
		assert.Equal(t, int64(i+1), *rowsB[i].Sequence)
		assert.NotEqual(t, rowsA[i].Hash, rowsB[i].Hash, "per-session chains stay independent")
	}
}

func TestAppendContextAction_ChainVerify(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i := 0; i < 4; i++ {
		require.NoError(t, store.AppendContextAction(ctx, &ContextActionRow{
			SessionID:  sessionID,
			Timestamp:  now.Add(time.Duration(i) * time.Millisecond),
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
			Agent:      "claude-code",
		}))
	}

	rows, err := store.QueryContextActionsFiltered(ctx, &ContextActionFilter{
		SessionID: &sessionID,
		Limit:     -1,
		Ascending: true,
	})
	require.NoError(t, err)
	require.Len(t, rows, 4)

	chainRows := make([]contextchain.Row, 0, len(rows))
	for _, r := range rows {
		require.NotNil(t, r.Sequence)
		var injection float32
		if r.InjectionScore != nil {
			injection = *r.InjectionScore
		}
		chainRows = append(chainRows, contextchain.Row{
			SessionID: r.SessionID,
			Sequence:  *r.Sequence,
			PrevHash:  r.PrevHash,
			Hash:      r.Hash,
			Fields: contextchain.InputFromRow(
				*r.Sequence, r.PrevHash, r.Timestamp,
				r.SessionID, r.EventID, r.ID,
				r.ActionType, r.Tool, r.Agent, r.Project, r.WorkingDir,
				r.DataClassifications, injection,
			),
		})
	}
	verified, breaks := contextchain.Verify(chainRows)
	assert.Empty(t, breaks, "freshly inserted chain must verify cleanly")
	assert.Equal(t, len(chainRows), verified, "every freshly inserted row must count as verified")
}

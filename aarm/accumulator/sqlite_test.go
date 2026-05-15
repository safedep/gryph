package accumulator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSQLiteAccumulator(t *testing.T) (*SQLiteAccumulator, *storage.SQLiteStore) {
	t.Helper()
	store := storagetest.NewStore(t)
	return NewSQLite(store), store
}

func newAction(t *testing.T, sessionID uuid.UUID, at model.ActionType, tool string) *model.Action {
	t.Helper()
	return &model.Action{
		ID:        uuid.New(),
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Type:      at,
		Tool:      tool,
	}
}

func TestSQLiteAccumulator_AppendIncrementsCounters(t *testing.T) {
	cases := []struct {
		name        string
		actionType  model.ActionType
		expectField func(t *testing.T, s *model.ContextSnapshot)
	}{
		{
			name:       "file_read",
			actionType: model.ActionFileRead,
			expectField: func(t *testing.T, s *model.ContextSnapshot) {
				assert.Equal(t, 1, s.FilesRead)
			},
		},
		{
			name:       "file_write",
			actionType: model.ActionFileWrite,
			expectField: func(t *testing.T, s *model.ContextSnapshot) {
				assert.Equal(t, 1, s.FilesWritten)
			},
		},
		{
			name:       "command_exec",
			actionType: model.ActionCommandExec,
			expectField: func(t *testing.T, s *model.ContextSnapshot) {
				assert.Equal(t, 1, s.CommandsExecuted)
			},
		},
		{
			name:       "network_request",
			actionType: model.ActionNetworkRequest,
			expectField: func(t *testing.T, s *model.ContextSnapshot) {
				assert.Equal(t, 1, s.NetworkRequests)
			},
		},
		{
			name:       "tool_use_no_counter",
			actionType: model.ActionToolUse,
			expectField: func(t *testing.T, s *model.ContextSnapshot) {
				assert.Equal(t, 0, s.FilesRead)
				assert.Equal(t, 0, s.FilesWritten)
				assert.Equal(t, 0, s.CommandsExecuted)
				assert.Equal(t, 0, s.NetworkRequests)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc, _ := newTestSQLiteAccumulator(t)
			sessionID := uuid.New()
			ctx := context.Background()

			action := newAction(t, sessionID, tc.actionType, "Read")
			require.NoError(t, acc.Append(ctx, action))

			snap, err := acc.Snapshot(ctx, sessionID)
			require.NoError(t, err)
			require.NotNil(t, snap)
			assert.Equal(t, 1, snap.TotalActions)
			tc.expectField(t, snap)
		})
	}
}

func TestSQLiteAccumulator_DistinctToolsAccumulate(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	require.NoError(t, acc.Append(ctx, newAction(t, sessionID, model.ActionFileRead, "Read")))
	require.NoError(t, acc.Append(ctx, newAction(t, sessionID, model.ActionFileRead, "Read")))
	require.NoError(t, acc.Append(ctx, newAction(t, sessionID, model.ActionFileWrite, "Write")))
	require.NoError(t, acc.Append(ctx, newAction(t, sessionID, model.ActionCommandExec, "Bash")))

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, 4, snap.TotalActions)
	assert.Equal(t, 2, snap.FilesRead)
	assert.Equal(t, 1, snap.FilesWritten)
	assert.Equal(t, 1, snap.CommandsExecuted)
	assert.ElementsMatch(t, []string{"Read", "Write", "Bash"}, snap.ToolsUsed)
}

func TestSQLiteAccumulator_NewSessionReturnsEmpty(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	snap, err := acc.Snapshot(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, 0, snap.TotalActions)
	assert.Empty(t, snap.ToolsUsed)
	assert.Equal(t, time.Duration(0), snap.SessionDuration)
}

func TestSQLiteAccumulator_RecordResultFlipsStatus(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	action := newAction(t, sessionID, model.ActionCommandExec, "Bash")
	require.NoError(t, acc.Append(ctx, action))

	require.NoError(t, acc.RecordResult(ctx, action.ID, model.Result{
		Status:   model.ResultSuccess,
		Duration: 250 * time.Millisecond,
	}))

	rows, err := store.QueryContextActions(ctx, sessionID, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "success", rows[0].ResultStatus)
	require.NotNil(t, rows[0].DurationMS)
	assert.Equal(t, int64(250), *rows[0].DurationMS)

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 0, snap.Errors, "success result must not bump errors")
}

func TestSQLiteAccumulator_RecordResultErrorBumpsErrorsCounter(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	action := newAction(t, sessionID, model.ActionCommandExec, "Bash")
	require.NoError(t, acc.Append(ctx, action))

	require.NoError(t, acc.RecordResult(ctx, action.ID, model.Result{
		Status: model.ResultError,
		Error:  "boom",
	}))

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 1, snap.Errors)
}

func TestSQLiteAccumulator_ConcurrentSessionsDoNotCorrupt(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()

	sessionA := uuid.New()
	sessionB := uuid.New()
	const perSession = 8

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < perSession; i++ {
			err := acc.Append(ctx, newAction(t, sessionA, model.ActionFileRead, "Read"))
			assert.NoError(t, err)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < perSession; i++ {
			err := acc.Append(ctx, newAction(t, sessionB, model.ActionCommandExec, "Bash"))
			assert.NoError(t, err)
		}
	}()
	wg.Wait()

	snapA, err := acc.Snapshot(ctx, sessionA)
	require.NoError(t, err)
	assert.Equal(t, perSession, snapA.TotalActions)
	assert.Equal(t, perSession, snapA.FilesRead)
	assert.Equal(t, 0, snapA.CommandsExecuted)

	snapB, err := acc.Snapshot(ctx, sessionB)
	require.NoError(t, err)
	assert.Equal(t, perSession, snapB.TotalActions)
	assert.Equal(t, perSession, snapB.CommandsExecuted)
	assert.Equal(t, 0, snapB.FilesRead)
}

func TestSQLiteAccumulator_AppendUnionsClassificationsAndEntities(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	a1 := newAction(t, sessionID, model.ActionFileRead, "Read")
	a1.DataClassifications = []string{"secret"}
	require.NoError(t, acc.Append(ctx, a1))

	a2 := newAction(t, sessionID, model.ActionFileRead, "Read")
	a2.DataClassifications = []string{"secret", "config"}
	require.NoError(t, acc.Append(ctx, a2))

	a3 := newAction(t, sessionID, model.ActionCommandExec, "Bash")
	a3.DataClassifications = []string{"config"}
	require.NoError(t, acc.Append(ctx, a3))

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.ElementsMatch(t, []string{"secret", "config"}, snap.ClassificationsSeen)
	assert.Empty(t, snap.EntitiesSeen, "entities_seen is reserved for Phase 4 and is not maintained today")
	assert.ElementsMatch(t, []string{"Read", "Bash"}, snap.ToolsUsed)
}

func TestSQLiteAccumulator_AppendNoToolSkipsEntities(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	a := newAction(t, sessionID, model.ActionFileRead, "")
	require.NoError(t, acc.Append(ctx, a))

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Empty(t, snap.EntitiesSeen)
	assert.Empty(t, snap.ToolsUsed)
}

func TestSQLiteAccumulator_SnapshotComputesSessionDuration(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	sessionID := uuid.New()
	ctx := context.Background()

	past := time.Now().UTC().Add(-2 * time.Minute)
	action := &model.Action{
		ID:        uuid.New(),
		SessionID: sessionID,
		Timestamp: past,
		Type:      model.ActionFileRead,
		Tool:      "Read",
	}
	require.NoError(t, acc.Append(ctx, action))

	snap, err := acc.Snapshot(ctx, sessionID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, snap.SessionDuration, time.Minute)
}

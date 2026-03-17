package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage/ent/auditevent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSearchableText(t *testing.T) {
	tests := []struct {
		name     string
		event    *events.Event
		contains []string
	}{
		{
			name: "file_write extracts path",
			event: &events.Event{
				ActionType: events.ActionFileWrite,
				Payload:    mustMarshalFTS(events.FileWritePayload{Path: "/src/migration/001.go"}),
			},
			contains: []string{"migration"},
		},
		{
			name: "command_exec extracts command",
			event: &events.Event{
				ActionType: events.ActionCommandExec,
				Payload:    mustMarshalFTS(events.CommandExecPayload{Command: "go test ./..."}),
			},
			contains: []string{"go test"},
		},
		{
			name: "error message is included",
			event: &events.Event{
				ActionType:   events.ActionFileRead,
				ErrorMessage: "permission denied reading file",
			},
			contains: []string{"permission denied"},
		},
		{
			name: "tool name is included",
			event: &events.Event{
				ActionType: events.ActionFileRead,
				ToolName:   "ReadFile",
			},
			contains: []string{"ReadFile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := buildSearchableText(tt.event)
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
		})
	}
}

func TestSQLiteStore_FTSIndexAndSearch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	writePayload, _ := json.Marshal(events.FileWritePayload{Path: "/src/migration/001.go"})
	err := store.SaveEvent(ctx, &events.Event{
		ID:           uuid.New(),
		SessionID:    sessionID,
		Sequence:     1,
		Timestamp:    now,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileWrite,
		ResultStatus: events.ResultSuccess,
		Payload:      writePayload,
	})
	require.NoError(t, err)

	cmdPayload, _ := json.Marshal(events.CommandExecPayload{Command: "go test ./..."})
	err = store.SaveEvent(ctx, &events.Event{
		ID:           uuid.New(),
		SessionID:    sessionID,
		Sequence:     2,
		Timestamp:    now.Add(time.Second),
		AgentName:    "claude-code",
		ActionType:   events.ActionCommandExec,
		ResultStatus: events.ResultSuccess,
		Payload:      cmdPayload,
	})
	require.NoError(t, err)

	results, err := store.SearchEvents(ctx, "migration", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, sessionID, results[0].SessionID)

	results, err = store.SearchEvents(ctx, "test", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, sessionID, results[0].SessionID)
}

func TestSQLiteStore_HasSearch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	assert.True(t, store.HasSearch())
}

func TestSQLiteStore_BackfillFTS(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	// Drop the FTS table so indexEvent won't be called during the direct insert below.
	_, err := store.db.ExecContext(ctx, "DROP TABLE IF EXISTS events_fts")
	require.NoError(t, err)

	// Insert an event directly via ent client, bypassing SaveEvent (and its FTS indexing).
	cmdPayload := map[string]interface{}{"command": "go build ./..."}
	_, err = store.client.AuditEvent.Create().
		SetID(uuid.New()).
		SetSessionID(sessionID).
		SetSequence(1).
		SetTimestamp(now).
		SetAgentName("claude-code").
		SetActionType(auditevent.ActionTypeCommandExec).
		SetResultStatus(auditevent.ResultStatusSuccess).
		SetIsSensitive(false).
		SetPayload(cmdPayload).
		Save(ctx)
	require.NoError(t, err)

	// Recreate the FTS table (empty).
	err = store.InitFTS(ctx)
	require.NoError(t, err)

	// Backfill should index the one event.
	indexed, err := store.BackfillFTS(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, indexed)

	// Running backfill again should be a no-op (FTS count >= event count).
	indexed, err = store.BackfillFTS(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 0, indexed)
}

func mustMarshalFTS(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

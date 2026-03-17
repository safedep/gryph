package storage

import (
	"context"
	"encoding/json"
	"strings"
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

	// Drop the FTS tables so indexEvent won't be called during the direct insert below.
	_, err := store.db.ExecContext(ctx, "DROP TABLE IF EXISTS events_fts")
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, "DROP TABLE IF EXISTS events_fts_meta")
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

	// Recreate the FTS tables (empty).
	err = store.InitFTS(ctx)
	require.NoError(t, err)

	// Backfill should index the one event.
	indexed, err := store.BackfillFTS(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, indexed)

	// Second backfill should be instant no-op via meta flag.
	indexed, err = store.BackfillFTS(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 0, indexed)

	// Verify the meta flag is set.
	var done string
	err = store.db.QueryRowContext(ctx,
		"SELECT value FROM events_fts_meta WHERE key = 'backfill_done'",
	).Scan(&done)
	require.NoError(t, err)
	assert.Equal(t, "1", done)

	// Insert another event directly (simulating pre-FTS data added after backfill).
	// Backfill should still be a no-op because the flag is set.
	cmdPayload2 := map[string]interface{}{"command": "go test ./..."}
	_, err = store.client.AuditEvent.Create().
		SetID(uuid.New()).
		SetSessionID(sessionID).
		SetSequence(2).
		SetTimestamp(now.Add(time.Second)).
		SetAgentName("claude-code").
		SetActionType(auditevent.ActionTypeCommandExec).
		SetResultStatus(auditevent.ResultStatusSuccess).
		SetIsSensitive(false).
		SetPayload(cmdPayload2).
		Save(ctx)
	require.NoError(t, err)

	indexed, err = store.BackfillFTS(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 0, indexed, "should skip backfill because meta flag is set")
}

func TestSQLiteStore_FTSCleanupOnDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	now := time.Now().UTC()
	payload, _ := json.Marshal(events.FileWritePayload{Path: "/src/cleanup.go"})
	err := store.SaveEvent(ctx, &events.Event{
		ID:           uuid.New(),
		SessionID:    sessionID,
		Sequence:     1,
		Timestamp:    now.Add(-48 * time.Hour),
		AgentName:    "claude-code",
		ActionType:   events.ActionFileWrite,
		ResultStatus: events.ResultSuccess,
		Payload:      payload,
	})
	require.NoError(t, err)

	results, err := store.SearchEvents(ctx, "cleanup", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	deleted, err := store.DeleteEventsBefore(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	results, err = store.SearchEvents(ctx, "cleanup", 10)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestEscapeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple word", "migration", `"migration"`},
		{"two words", "go test", `"go" "test"`},
		{"FTS operator minus", "rm -rf", `"rm" "-rf"`},
		{"FTS operator NOT", "NOT secret", `"NOT" "secret"`},
		{"FTS operator OR", "foo OR bar", `"foo" "OR" "bar"`},
		{"FTS operator AND", "foo AND bar", `"foo" "AND" "bar"`},
		{"FTS wildcard star", "test*", `"test*"`},
		{"parentheses", "(drop table)", `"(drop" "table)"`},
		{"embedded quotes", `say "hello"`, `"say" """hello"""`},
		{"empty string", "", ""},
		{"only spaces", "   ", ""},
		{"special chars", ".env* OR password", `".env*" "OR" "password"`},
		{"SQL injection attempt", `"; DROP TABLE events_fts; --`, `""";" "DROP" "TABLE" "events_fts;" "--"`},
		{"column reference attempt", "searchable_text:secret", `"searchable_text:secret"`},
		{"caret prefix", "^start", `"^start"`},
		{"curly braces NEAR", "NEAR(a b)", `"NEAR(a" "b)"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeFTSQuery(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSQLiteStore_SearchEventsSecurityInputs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	// Index an event so the FTS table is not empty
	payload, _ := json.Marshal(events.CommandExecPayload{Command: "rm -rf /tmp/test"})
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID: uuid.New(), SessionID: sessionID, Sequence: 1,
		Timestamp: time.Now().UTC(), AgentName: "claude-code",
		ActionType: events.ActionCommandExec, ResultStatus: events.ResultSuccess,
		Payload: payload,
	}))

	// All these inputs must not cause SQL errors or panics
	dangerousInputs := []struct {
		name  string
		query string
	}{
		{"FTS operator minus", "rm -rf"},
		{"FTS operator NOT", "NOT secret"},
		{"FTS operator OR", "foo OR bar"},
		{"SQL injection semicolon", `"; DROP TABLE events_fts; --`},
		{"SQL injection union", `" UNION SELECT * FROM sessions --`},
		{"FTS column prefix", "searchable_text:password"},
		{"empty after trim", "   "},
		{"embedded quotes", `he said "hello"`},
		{"wildcard star", "test*"},
		{"NEAR operator", "NEAR(secret password)"},
		{"parentheses", "(admin) OR (root)"},
		{"backslash", `C:\Users\secret`},
		{"null byte", "test\x00injection"},
		{"very long input", strings.Repeat("a", 1000)},
	}

	for _, tt := range dangerousInputs {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.SearchEvents(ctx, tt.query, 10)
			// Must not error — safe handling required
			assert.NoError(t, err, "query %q should not cause an error", tt.query)
			// Results may be empty, that's fine — just no crashes
			_ = results
		})
	}

	// Verify the legitimate search still works through escaping
	results, err := store.SearchEvents(ctx, "rm -rf", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "should find the event containing 'rm -rf'")
}

func mustMarshalFTS(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

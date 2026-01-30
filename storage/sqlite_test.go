package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Init(ctx)
	require.NoError(t, err)

	cleanup := func() {
		err := store.Close()
		require.NoError(t, err)
	}

	return store, cleanup
}

// createTestSession creates a session for testing (needed before saving events)
func createTestSession(t *testing.T, store *SQLiteStore, sessionID uuid.UUID, agentName string) {
	t.Helper()
	ctx := context.Background()

	sess := &session.Session{
		ID:        sessionID,
		AgentName: agentName,
		StartedAt: time.Now().UTC(),
	}
	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)
}

func TestSQLiteStore_SaveAndGetEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create session first (foreign key constraint)
	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sess := &session.Session{
		ID:        sessionID,
		AgentName: "claude-code",
		StartedAt: now,
	}
	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)

	// Create a test event
	eventID := uuid.New()

	payload := events.FileReadPayload{Path: "/test/file.go"}
	payloadBytes, _ := json.Marshal(payload)

	event := &events.Event{
		ID:               eventID,
		SessionID:        sessionID,
		Sequence:         1,
		Timestamp:        now,
		AgentName:        "claude-code",
		AgentVersion:     "1.0.0",
		WorkingDirectory: "/test/project",
		ActionType:       events.ActionFileRead,
		ToolName:         "Read",
		ResultStatus:     events.ResultSuccess,
		Payload:          payloadBytes,
		IsSensitive:      false,
	}

	// Save the event
	err = store.SaveEvent(ctx, event)
	require.NoError(t, err)

	// Retrieve the event
	retrieved, err := store.GetEvent(ctx, eventID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify fields
	assert.Equal(t, eventID, retrieved.ID)
	assert.Equal(t, sessionID, retrieved.SessionID)
	assert.Equal(t, 1, retrieved.Sequence)
	assert.Equal(t, now.Unix(), retrieved.Timestamp.Unix())
	assert.Equal(t, "claude-code", retrieved.AgentName)
	assert.Equal(t, "1.0.0", retrieved.AgentVersion)
	assert.Equal(t, events.ActionFileRead, retrieved.ActionType)
	assert.Equal(t, "Read", retrieved.ToolName)
	assert.Equal(t, events.ResultSuccess, retrieved.ResultStatus)
	assert.False(t, retrieved.IsSensitive)
}

func TestSQLiteStore_GetEventNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get a non-existent event
	retrieved, err := store.GetEvent(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestSQLiteStore_QueryEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	// Create session first
	createTestSession(t, store, sessionID, "claude-code")

	// Create multiple events
	for i := 0; i < 5; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Query all events
	filter := events.NewEventFilter()
	results, err := store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Query with limit
	filter = events.NewEventFilter().WithLimit(3)
	results, err = store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Query by agent
	filter = events.NewEventFilter().WithAgents("claude-code")
	results, err = store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Query by non-existent agent
	filter = events.NewEventFilter().WithAgents("cursor")
	results, err = store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestSQLiteStore_QueryEventsWithTimeFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	baseTime := time.Now().UTC().Add(-time.Hour)

	// Create session first
	createTestSession(t, store, sessionID, "claude-code")

	// Create events at different times
	for i := 0; i < 5; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    baseTime.Add(time.Duration(i) * 10 * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Query with since filter (should get events after 25 minutes)
	sinceTime := baseTime.Add(25 * time.Minute)
	filter := events.NewEventFilter().WithSince(sinceTime)
	results, err := store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 2) // Events at 30min and 40min
}

func TestSQLiteStore_CountEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	// Create session first
	createTestSession(t, store, sessionID, "claude-code")

	// Create multiple events
	for i := 0; i < 10; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Count all events
	filter := events.NewEventFilter()
	count, err := store.CountEvents(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestSQLiteStore_DeleteEventsBefore(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	baseTime := time.Now().UTC().Add(-time.Hour)

	// Create session first
	createTestSession(t, store, sessionID, "claude-code")

	// Create events at different times
	for i := 0; i < 5; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    baseTime.Add(time.Duration(i) * 10 * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Delete events before 25 minutes mark (should delete 3 events)
	cutoff := baseTime.Add(25 * time.Minute)
	deleted, err := store.DeleteEventsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 3, deleted)

	// Verify remaining events
	filter := events.NewEventFilter()
	remaining, err := store.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestSQLiteStore_CountEventsBefore(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	baseTime := time.Now().UTC().Add(-time.Hour)

	// Create session first
	createTestSession(t, store, sessionID, "claude-code")

	// Create events at different times
	for i := 0; i < 5; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    baseTime.Add(time.Duration(i) * 10 * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Count events before 25 minutes mark (should be 3 events)
	cutoff := baseTime.Add(25 * time.Minute)
	count, err := store.CountEventsBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestSQLiteStore_GetEventsBySession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID1 := uuid.New()
	sessionID2 := uuid.New()
	now := time.Now().UTC()

	// Create sessions first
	createTestSession(t, store, sessionID1, "claude-code")
	createTestSession(t, store, sessionID2, "cursor")

	// Create events for session 1
	for i := 0; i < 3; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID1,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Create events for session 2
	for i := 0; i < 2; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID2,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "cursor",
			ActionType:   events.ActionFileWrite,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Get events for session 1
	results, err := store.GetEventsBySession(ctx, sessionID1)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Get events for session 2
	results, err = store.GetEventsBySession(ctx, sessionID2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSQLiteStore_SaveAndGetSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sess := &session.Session{
		ID:               sessionID,
		AgentSessionID:   "agent-session-123",
		AgentName:        "claude-code",
		AgentVersion:     "1.0.0",
		StartedAt:        now,
		WorkingDirectory: "/test/project",
		ProjectName:      "test-project",
		TotalActions:     10,
		FilesRead:        5,
		FilesWritten:     3,
		CommandsExecuted: 2,
		Errors:           0,
	}

	// Save session
	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)

	// Retrieve session
	retrieved, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify fields
	assert.Equal(t, sessionID, retrieved.ID)
	assert.Equal(t, "agent-session-123", retrieved.AgentSessionID)
	assert.Equal(t, "claude-code", retrieved.AgentName)
	assert.Equal(t, "1.0.0", retrieved.AgentVersion)
	assert.Equal(t, "/test/project", retrieved.WorkingDirectory)
	assert.Equal(t, "test-project", retrieved.ProjectName)
	assert.Equal(t, 10, retrieved.TotalActions)
	assert.Equal(t, 5, retrieved.FilesRead)
	assert.Equal(t, 3, retrieved.FilesWritten)
	assert.Equal(t, 2, retrieved.CommandsExecuted)
	assert.Equal(t, 0, retrieved.Errors)
}

func TestSQLiteStore_UpdateSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sess := &session.Session{
		ID:           sessionID,
		AgentName:    "claude-code",
		StartedAt:    now,
		TotalActions: 0,
	}

	// Save session
	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)

	// Update session
	sess.TotalActions = 5
	sess.FilesWritten = 2
	sess.End()

	err = store.UpdateSession(ctx, sess)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 5, retrieved.TotalActions)
	assert.Equal(t, 2, retrieved.FilesWritten)
	assert.False(t, retrieved.EndedAt.IsZero())
}

func TestSQLiteStore_QuerySessions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	// Create sessions for different agents
	for i := 0; i < 3; i++ {
		sess := &session.Session{
			ID:        uuid.New(),
			AgentName: "claude-code",
			StartedAt: now.Add(time.Duration(i) * time.Hour),
		}
		err := store.SaveSession(ctx, sess)
		require.NoError(t, err)
	}

	for i := 0; i < 2; i++ {
		sess := &session.Session{
			ID:        uuid.New(),
			AgentName: "cursor",
			StartedAt: now.Add(time.Duration(i) * time.Hour),
		}
		err := store.SaveSession(ctx, sess)
		require.NoError(t, err)
	}

	// Query all sessions
	filter := session.NewSessionFilter()
	results, err := store.QuerySessions(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 5)

	// Query by agent
	filter = session.NewSessionFilter().WithAgent("claude-code")
	results, err = store.QuerySessions(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestSQLiteStore_GetActiveSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	// Create a completed session
	completedSess := &session.Session{
		ID:        uuid.New(),
		AgentName: "claude-code",
		StartedAt: now.Add(-2 * time.Hour),
	}
	completedSess.End()
	err := store.SaveSession(ctx, completedSess)
	require.NoError(t, err)

	// Create an active session
	activeSess := &session.Session{
		ID:        uuid.New(),
		AgentName: "claude-code",
		StartedAt: now.Add(-time.Hour),
	}
	err = store.SaveSession(ctx, activeSess)
	require.NoError(t, err)

	// Get active session
	retrieved, err := store.GetActiveSession(ctx, "claude-code")
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, activeSess.ID, retrieved.ID)
	assert.True(t, retrieved.EndedAt.IsZero())
}

func TestSQLiteStore_SaveAndQuerySelfAudit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	entry := &SelfAuditEntry{
		ID:          uuid.New(),
		Timestamp:   now,
		Action:      "install",
		AgentName:   "claude-code",
		Details:     map[string]interface{}{"hooks_installed": []string{"PreToolUse"}},
		Result:      "success",
		ToolVersion: "1.0.0",
	}

	// Save self-audit
	err := store.SaveSelfAudit(ctx, entry)
	require.NoError(t, err)

	// Query self-audits
	filter := &SelfAuditFilter{Limit: 10}
	results, err := store.QuerySelfAudits(ctx, filter)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify fields
	assert.Equal(t, entry.ID, results[0].ID)
	assert.Equal(t, "install", results[0].Action)
	assert.Equal(t, "claude-code", results[0].AgentName)
	assert.Equal(t, "success", results[0].Result)
	assert.Equal(t, "1.0.0", results[0].ToolVersion)
}

func TestSQLiteStore_GetDatabaseInfo(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	// Create a session first
	sess := &session.Session{
		ID:        sessionID,
		AgentName: "claude-code",
		StartedAt: now,
	}
	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)

	// Create some events
	for i := 0; i < 3; i++ {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Get database info
	info, err := store.GetDatabaseInfo(ctx)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, 3, info.EventCount)
	assert.Equal(t, 1, info.SessionCount)
	assert.Greater(t, info.SizeBytes, int64(0))
}

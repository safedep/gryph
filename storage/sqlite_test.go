package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
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

func TestSQLiteStore_GetEventByPrefix(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	createTestSession(t, store, sessionID, "claude-code")

	eventID := uuid.New()
	event := &events.Event{
		ID:           eventID,
		SessionID:    sessionID,
		Sequence:     1,
		Timestamp:    now,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileRead,
		ResultStatus: events.ResultSuccess,
	}
	err := store.SaveEvent(ctx, event)
	require.NoError(t, err)

	tests := []struct {
		name    string
		prefix  string
		wantNil bool
	}{
		{
			name:    "full ID matches",
			prefix:  eventID.String(),
			wantNil: false,
		},
		{
			name:    "8-char prefix matches",
			prefix:  eventID.String()[:8],
			wantNil: false,
		},
		{
			name:    "non-matching prefix",
			prefix:  "00000000",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.GetEventByPrefix(ctx, tt.prefix)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, eventID, result.ID)
			}
		})
	}
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

func TestSQLiteStore_QueryEventsOrdersTiesByID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	// Every event shares one timestamp, so only the id can order them.
	sameTime := time.Date(2025, 2, 1, 9, 0, 0, 0, time.UTC)

	const total = 6
	ids := make([]uuid.UUID, total)
	for i := range ids {
		ids[i] = uuid.New()
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i].String() > ids[j].String() {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}

	// Insert in an order that matches neither the ascending nor the
	// descending id order. A missing id tie break cannot pass by accident.
	for seq, idx := range []int{2, 5, 0, 4, 1, 3} {
		err := store.SaveEvent(ctx, &events.Event{
			ID:           ids[idx],
			SessionID:    sessionID,
			Sequence:     seq + 1,
			Timestamp:    sameTime,
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		})
		require.NoError(t, err)
	}

	// A full ascending query returns the ids in order.
	asc, err := store.QueryEvents(ctx, events.NewEventFilter().WithSort(events.SortAsc))
	require.NoError(t, err)
	require.Len(t, asc, total)
	for i, e := range asc {
		assert.Equal(t, ids[i], e.ID, "ascending ties must order by id")
	}

	// A descending query returns the ids in reverse order.
	desc, err := store.QueryEvents(ctx, events.NewEventFilter().WithSort(events.SortDesc))
	require.NoError(t, err)
	require.Len(t, desc, total)
	for i, e := range desc {
		assert.Equal(t, ids[total-1-i], e.ID, "descending ties must order by id")
	}

	// A limit inside the tie group keeps the smallest ids ascending.
	const limit = 4
	ascPage, err := store.QueryEvents(ctx, events.NewEventFilter().WithSort(events.SortAsc).WithLimit(limit))
	require.NoError(t, err)
	require.Len(t, ascPage, limit)
	for i, e := range ascPage {
		assert.Equal(t, ids[i], e.ID, "ascending page must hold the smallest ids")
	}

	// A descending limit keeps the largest ids descending.
	descPage, err := store.QueryEvents(ctx, events.NewEventFilter().WithSort(events.SortDesc).WithLimit(limit))
	require.NoError(t, err)
	require.Len(t, descPage, limit)
	for i, e := range descPage {
		assert.Equal(t, ids[total-1-i], e.ID, "descending page must hold the largest ids")
	}
}

// TestSQLiteStore_QueryEventsAfterNormalizesNonUTCTimestamp covers events
// imported with a non-UTC offset. The driver stores timestamps as text, so an
// unnormalized offset can sort before a UTC cursor even though the instant is
// later, which makes QueryEventsAfter skip the event.
func TestSQLiteStore_QueryEventsAfterNormalizesNonUTCTimestamp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	ctx := context.Background()
	require.NoError(t, store.Init(ctx))

	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	// 00:31 at -05:00 is the same instant as 05:31 UTC. A cursor at 05:30 UTC
	// precedes it chronologically but not lexicographically when the offset is
	// still present in the stored text.
	imported := time.Date(2025, 3, 1, 0, 31, 0, 0, time.FixedZone("EST", -5*60*60))
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID:           uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		SessionID:    sessionID,
		Sequence:     1,
		Timestamp:    imported,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileRead,
		ResultStatus: events.ResultSuccess,
		Imported:     true,
	}))

	// Record the physical representation to document the text comparison.
	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, raw.Close()) }()
	var stored, quoted, kind string
	require.NoError(t, raw.QueryRowContext(ctx, `SELECT timestamp, quote(timestamp), typeof(timestamp) FROM audit_events`).Scan(&stored, &quoted, &kind))
	t.Logf("stored timestamp: kind=%s quoted=%s go=%q", kind, quoted, stored)

	cursor := time.Date(2025, 3, 1, 5, 30, 0, 0, time.UTC)
	got, err := store.QueryEventsAfter(ctx, cursor, uuid.Nil, 10)
	require.NoError(t, err)
	require.Len(t, got, 1, "the imported event is chronologically after the UTC cursor")

	// The round-tripped instant must match 05:31 UTC.
	require.True(t, got[0].Timestamp.Equal(imported))
	require.True(t, got[0].Timestamp.UTC().Equal(time.Date(2025, 3, 1, 5, 31, 0, 0, time.UTC)))
}

// TestSQLiteStore_QueryBoundsNormalizeToUTC covers filter bounds and cursors
// that carry a non-UTC offset. Bounds reach the same text comparison as the
// stored timestamps, so an unnormalized offset can include or exclude events
// for the wrong instant.
func TestSQLiteStore_QueryBoundsNormalizeToUTC(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	// Two events at 10:00 and 10:10 UTC.
	early := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	late := time.Date(2025, 3, 1, 10, 10, 0, 0, time.UTC)
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID:           uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		SessionID:    sessionID,
		Sequence:     1,
		Timestamp:    early,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileRead,
		ResultStatus: events.ResultSuccess,
	}))
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID:           uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		SessionID:    sessionID,
		Sequence:     2,
		Timestamp:    late,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileRead,
		ResultStatus: events.ResultSuccess,
	}))

	// 15:25 at +05:30 is 09:55 UTC, before both events. An unnormalized bound
	// keeps the offset in its text and sorts after "10:0", excluding both.
	since := time.Date(2025, 3, 1, 15, 25, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	got, err := store.QueryEvents(ctx, events.NewEventFilter().WithSince(since).WithSort(events.SortAsc))
	require.NoError(t, err)
	require.Len(t, got, 2, "both events are at or after the 09:55 UTC bound")
	assert.Equal(t, early, got[0].Timestamp.UTC())
	assert.Equal(t, late, got[1].Timestamp.UTC())

	// 05:05 at -05:00 is 10:05 UTC, between the two events.
	until := time.Date(2025, 3, 1, 5, 5, 0, 0, time.FixedZone("EST", -5*60*60))
	got, err = store.QueryEvents(ctx, events.NewEventFilter().WithUntil(until).WithSort(events.SortAsc))
	require.NoError(t, err)
	require.Len(t, got, 1, "only the 10:00 UTC event is at or before the 10:05 UTC bound")
	assert.Equal(t, early, got[0].Timestamp.UTC())

	// The same instant as a polling cursor must resume past both events.
	got, err = store.QueryEventsAfter(ctx, since, uuid.Nil, 10)
	require.NoError(t, err)
	require.Len(t, got, 2, "both events are after the 09:55 UTC cursor")
	assert.Equal(t, early, got[0].Timestamp.UTC())
	assert.Equal(t, late, got[1].Timestamp.UTC())
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
	sess.ProjectName = "gryph"
	sess.End()

	err = store.UpdateSession(ctx, sess)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Zero(t, retrieved.TotalActions, "only RecordEvent changes the counters")
	assert.Zero(t, retrieved.FilesWritten)
	assert.Equal(t, "gryph", retrieved.ProjectName)
	assert.False(t, retrieved.EndedAt.IsZero())
}

func TestSQLiteStore_RecordEvent(t *testing.T) {
	cases := []struct {
		name         string
		eventCount   int
		totalActions int
		wantFirstSeq int
	}{
		{name: "new session", wantFirstSeq: 1},
		{name: "session with events", eventCount: 7, totalActions: 3, wantFirstSeq: 8},
		{name: "legacy session with no event count", totalActions: 5, wantFirstSeq: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := setupTestStore(t)
			defer cleanup()
			ctx := context.Background()
			sessionID := uuid.New()
			require.NoError(t, store.SaveSession(ctx, &session.Session{
				ID: sessionID, AgentName: "claude-code", StartedAt: time.Now().UTC(),
				EventCount: tc.eventCount, TotalActions: tc.totalActions,
			}))

			event := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
			event.ResultStatus = events.ResultError
			require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
			assert.Equal(t, tc.wantFirstSeq, event.Sequence)

			sess, err := store.GetSession(ctx, sessionID)
			require.NoError(t, err)
			assert.Equal(t, tc.wantFirstSeq, sess.EventCount)
			assert.Equal(t, tc.totalActions+1, sess.TotalActions)
			assert.Equal(t, 1, sess.CommandsExecuted)
			assert.Equal(t, 1, sess.Errors)
		})
	}
}

func TestSQLiteStore_RecordEvent_ParallelStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	ctx := context.Background()
	const writers = 8
	const perWriter = 5

	stores := make([]*SQLiteStore, writers)
	for i := range stores {
		store, err := NewSQLiteStore(path)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))
		t.Cleanup(func() { require.NoError(t, store.Close()) })
		stores[i] = store
	}

	sessionID := uuid.New()
	require.NoError(t, stores[0].SaveSession(ctx, &session.Session{ID: sessionID, AgentName: "claude-code", StartedAt: time.Now().UTC()}))

	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for _, store := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWriter {
				event := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
				errs <- store.RecordEvent(ctx, event, session.EventCounts(event))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	sess, err := stores[0].GetSession(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, writers*perWriter, sess.CommandsExecuted, "no counter update is lost")
	assert.Equal(t, writers*perWriter, sess.EventCount)

	evts, err := stores[0].QueryEvents(ctx, &events.EventFilter{SessionID: &sessionID, Limit: 1000})
	require.NoError(t, err)
	seen := map[int]bool{}
	for _, e := range evts {
		assert.False(t, seen[e.Sequence], "sequence %d is used twice", e.Sequence)
		seen[e.Sequence] = true
	}
	assert.Len(t, seen, writers*perWriter)
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

func TestSQLiteStore_QueryEventsWithFilePattern(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	files := []string{
		"/src/main.go",
		"/src/utils/helper.go",
		"/src/utils/helper_test.go",
		"/docs/readme.md",
		"/config.yaml",
	}

	for i, path := range files {
		payload, _ := json.Marshal(events.FileReadPayload{Path: path})
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
			Payload:      payload,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	tests := []struct {
		name    string
		pattern string
		want    int
	}{
		{
			name:    "match all go files",
			pattern: "*.go",
			want:    3,
		},
		{
			name:    "match test files",
			pattern: "*_test.go",
			want:    1,
		},
		{
			name:    "match files in utils directory",
			pattern: "/src/utils/*",
			want:    2,
		},
		{
			name:    "match markdown files",
			pattern: "*.md",
			want:    1,
		},
		{
			name:    "no matches",
			pattern: "*.rs",
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := events.NewEventFilter().WithFilePattern(tt.pattern)
			results, err := store.QueryEvents(ctx, filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.want)
		})
	}
}

func TestSQLiteStore_QueryEventsWithCommandPattern(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	commands := []string{
		"npm install",
		"npm test",
		"go build ./...",
		"git status",
		"git commit -m 'fix'",
	}

	for i, cmd := range commands {
		payload, err := json.Marshal(events.CommandExecPayload{Command: privacy.NewText(cmd)})
		require.NoError(t, err)
		if cmd == "git status" {
			// A row from before content labels holds the command as a string.
			payload = []byte(`{"command":"git status","exit_code":0}`)
		}
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionCommandExec,
			ResultStatus: events.ResultSuccess,
			Payload:      payload,
		}
		require.NoError(t, store.SaveEvent(ctx, event))
	}

	tests := []struct {
		name    string
		pattern string
		want    int
	}{
		{
			name:    "match npm commands",
			pattern: "npm *",
			want:    2,
		},
		{
			name:    "match git commands",
			pattern: "git *",
			want:    2,
		},
		{
			name:    "match go commands",
			pattern: "go *",
			want:    1,
		},
		{
			name:    "no matches",
			pattern: "docker *",
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := events.NewEventFilter().WithCommandPattern(tt.pattern)
			results, err := store.QueryEvents(ctx, filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.want)
		})
	}
}

func TestSQLiteStore_QueryEventsWithSessionFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID1 := uuid.New()
	sessionID2 := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID1, "claude-code")
	createTestSession(t, store, sessionID2, "cursor")

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
		require.NoError(t, store.SaveEvent(ctx, event))
	}

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
		require.NoError(t, store.SaveEvent(ctx, event))
	}

	tests := []struct {
		name      string
		sessionID uuid.UUID
		want      int
	}{
		{
			name:      "filter by session 1",
			sessionID: sessionID1,
			want:      3,
		},
		{
			name:      "filter by session 2",
			sessionID: sessionID2,
			want:      2,
		},
		{
			name:      "filter by non-existent session",
			sessionID: uuid.New(),
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := events.NewEventFilter().WithSession(tt.sessionID)
			results, err := store.QueryEvents(ctx, filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.want)
		})
	}
}

func TestSQLiteStore_QueryEventsWithStatusFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	statuses := []events.ResultStatus{
		events.ResultSuccess,
		events.ResultSuccess,
		events.ResultError,
		events.ResultBlocked,
		events.ResultError,
	}

	for i, s := range statuses {
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: s,
		}
		require.NoError(t, store.SaveEvent(ctx, event))
	}

	tests := []struct {
		name     string
		statuses []events.ResultStatus
		want     int
	}{
		{
			name:     "filter by success",
			statuses: []events.ResultStatus{events.ResultSuccess},
			want:     2,
		},
		{
			name:     "filter by error",
			statuses: []events.ResultStatus{events.ResultError},
			want:     2,
		},
		{
			name:     "filter by blocked",
			statuses: []events.ResultStatus{events.ResultBlocked},
			want:     1,
		},
		{
			name:     "filter by multiple statuses",
			statuses: []events.ResultStatus{events.ResultError, events.ResultBlocked},
			want:     3,
		},
		{
			name:     "filter by rejected returns none",
			statuses: []events.ResultStatus{events.ResultRejected},
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := events.NewEventFilter().WithStatuses(tt.statuses...)
			results, err := store.QueryEvents(ctx, filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.want)
		})
	}
}

func TestSQLiteStore_QueryEventsWithSensitiveFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	now := time.Now().UTC()

	createTestSession(t, store, sessionID, "claude-code")

	for i, sensitive := range []bool{false, true, false, true, true} {
		payload, _ := json.Marshal(events.FileReadPayload{Path: "/some/file"})
		event := &events.Event{
			ID:           uuid.New(),
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
			Payload:      payload,
			IsSensitive:  sensitive,
		}
		err := store.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name      string
		sensitive *bool
		want      int
	}{
		{
			name:      "no filter returns all",
			sensitive: nil,
			want:      5,
		},
		{
			name:      "filter sensitive only",
			sensitive: boolPtr(true),
			want:      3,
		},
		{
			name:      "filter non-sensitive only",
			sensitive: boolPtr(false),
			want:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := events.NewEventFilter()
			if tt.sensitive != nil {
				filter = filter.WithSensitive(*tt.sensitive)
			}
			results, err := store.QueryEvents(ctx, filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.want)
		})
	}
}

func TestQueryEventsAfterCompoundCursor(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	sameTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	laterTime := sameTime.Add(time.Second)

	ids := make([]uuid.UUID, 4)
	for i := range ids {
		ids[i] = uuid.New()
	}
	// Sort IDs so we know ordering
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i].String() > ids[j].String() {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}

	// 3 events at sameTime, 1 at laterTime
	for i := 0; i < 3; i++ {
		err := store.SaveEvent(ctx, &events.Event{
			ID:           ids[i],
			SessionID:    sessionID,
			Sequence:     i + 1,
			Timestamp:    sameTime,
			AgentName:    "claude-code",
			ActionType:   events.ActionFileRead,
			ResultStatus: events.ResultSuccess,
		})
		require.NoError(t, err)
	}
	err := store.SaveEvent(ctx, &events.Event{
		ID:           ids[3],
		SessionID:    sessionID,
		Sequence:     4,
		Timestamp:    laterTime,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileRead,
		ResultStatus: events.ResultSuccess,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		after   time.Time
		afterID uuid.UUID
		limit   int
		wantIDs []uuid.UUID
	}{
		{
			name:    "nil afterID returns all after timestamp",
			after:   sameTime.Add(-time.Second),
			afterID: uuid.Nil,
			limit:   10,
			wantIDs: ids,
		},
		{
			name:    "compound cursor skips first event at same timestamp",
			after:   sameTime,
			afterID: ids[0],
			limit:   10,
			wantIDs: []uuid.UUID{ids[1], ids[2], ids[3]},
		},
		{
			name:    "compound cursor skips first two events at same timestamp",
			after:   sameTime,
			afterID: ids[1],
			limit:   10,
			wantIDs: []uuid.UUID{ids[2], ids[3]},
		},
		{
			name:    "compound cursor at last same-timestamp event returns only later",
			after:   sameTime,
			afterID: ids[2],
			limit:   10,
			wantIDs: []uuid.UUID{ids[3]},
		},
		{
			name:    "batch limit respected with compound cursor",
			after:   sameTime,
			afterID: ids[0],
			limit:   2,
			wantIDs: []uuid.UUID{ids[1], ids[2]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.QueryEventsAfter(ctx, tt.after, tt.afterID, tt.limit)
			require.NoError(t, err)
			gotIDs := make([]uuid.UUID, len(results))
			for i, r := range results {
				gotIDs[i] = r.ID
			}
			assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestSQLiteStore_QuerySessionsExtendedFilters(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sess1 := session.NewSession("claude-code")
	sess1.Errors = 2
	sess1.SensitiveActions = 1
	require.NoError(t, store.SaveSession(ctx, sess1))

	sess2 := session.NewSession("cursor")
	sess2.BlockedActions = 3
	require.NoError(t, store.SaveSession(ctx, sess2))

	sess3 := session.NewSession("claude-code")
	require.NoError(t, store.SaveSession(ctx, sess3))

	tests := []struct {
		name     string
		filter   *session.SessionFilter
		expected int
	}{
		{"multi-agent filter", session.NewSessionFilter().WithAgents([]string{"claude-code"}), 2},
		{"has errors", session.NewSessionFilter().WithHasErrors(true), 1},
		{"has sensitive", session.NewSessionFilter().WithHasSensitive(true), 1},
		{"has blocked", session.NewSessionFilter().WithHasBlocked(true), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.Limit = 100
			results, err := store.QuerySessions(ctx, tt.filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.expected)
		})
	}
}

func TestSQLiteStore_DistinctAgents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.SaveSession(ctx, session.NewSession("claude-code")))
	require.NoError(t, store.SaveSession(ctx, session.NewSession("cursor")))
	require.NoError(t, store.SaveSession(ctx, session.NewSession("claude-code")))

	agents, err := store.DistinctAgents(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-code", "cursor"}, agents)
}

func TestQuerySelfAuditsAfterCompoundCursor(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	sameTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	laterTime := sameTime.Add(time.Second)

	ids := make([]uuid.UUID, 4)
	for i := range ids {
		ids[i] = uuid.New()
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i].String() > ids[j].String() {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}

	for i := 0; i < 3; i++ {
		err := store.SaveSelfAudit(ctx, &SelfAuditEntry{
			ID:          ids[i],
			Timestamp:   sameTime,
			Action:      "install",
			AgentName:   "claude-code",
			Result:      "success",
			ToolVersion: "1.0.0",
		})
		require.NoError(t, err)
	}
	err := store.SaveSelfAudit(ctx, &SelfAuditEntry{
		ID:          ids[3],
		Timestamp:   laterTime,
		Action:      "install",
		AgentName:   "claude-code",
		Result:      "success",
		ToolVersion: "1.0.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		after   time.Time
		afterID uuid.UUID
		limit   int
		wantIDs []uuid.UUID
	}{
		{
			name:    "nil afterID returns all after timestamp",
			after:   sameTime.Add(-time.Second),
			afterID: uuid.Nil,
			limit:   10,
			wantIDs: ids,
		},
		{
			name:    "compound cursor skips first audit at same timestamp",
			after:   sameTime,
			afterID: ids[0],
			limit:   10,
			wantIDs: []uuid.UUID{ids[1], ids[2], ids[3]},
		},
		{
			name:    "compound cursor at last same-timestamp audit returns only later",
			after:   sameTime,
			afterID: ids[2],
			limit:   10,
			wantIDs: []uuid.UUID{ids[3]},
		},
		{
			name:    "batch limit respected with compound cursor",
			after:   sameTime,
			afterID: ids[0],
			limit:   2,
			wantIDs: []uuid.UUID{ids[1], ids[2]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.QuerySelfAuditsAfter(ctx, tt.after, tt.afterID, tt.limit)
			require.NoError(t, err)
			gotIDs := make([]uuid.UUID, len(results))
			for i, r := range results {
				gotIDs[i] = r.ID
			}
			assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestSQLiteStore_QuerySessionsEventSubQuery(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sess1 := session.NewSession("claude-code")
	require.NoError(t, store.SaveSession(ctx, sess1))

	sess2 := session.NewSession("cursor")
	require.NoError(t, store.SaveSession(ctx, sess2))

	now := time.Now().UTC()

	payload1, _ := json.Marshal(events.FileWritePayload{Path: "/src/main.go"})
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID:           uuid.New(),
		SessionID:    sess1.ID,
		Sequence:     1,
		Timestamp:    now,
		AgentName:    "claude-code",
		ActionType:   events.ActionFileWrite,
		ResultStatus: events.ResultSuccess,
		Payload:      payload1,
	}))

	payload2, _ := json.Marshal(events.CommandExecPayload{Command: privacy.NewText("npm test")})
	require.NoError(t, store.SaveEvent(ctx, &events.Event{
		ID:           uuid.New(),
		SessionID:    sess2.ID,
		Sequence:     1,
		Timestamp:    now,
		AgentName:    "cursor",
		ActionType:   events.ActionCommandExec,
		ResultStatus: events.ResultSuccess,
		Payload:      payload2,
	}))

	tests := []struct {
		name      string
		filter    *session.SessionFilter
		wantCount int
		wantID    uuid.UUID
	}{
		{
			name:      "filter by action type file_write",
			filter:    session.NewSessionFilter().WithEventActions([]string{"file_write"}).WithLimit(100),
			wantCount: 1,
			wantID:    sess1.ID,
		},
		{
			name:      "filter by file pattern",
			filter:    session.NewSessionFilter().WithFilePattern("*/main.go").WithLimit(100),
			wantCount: 1,
			wantID:    sess1.ID,
		},
		{
			name:      "filter by command pattern",
			filter:    session.NewSessionFilter().WithCommandPattern("npm*").WithLimit(100),
			wantCount: 1,
			wantID:    sess2.ID,
		},
		{
			name:      "no match for unknown action",
			filter:    session.NewSessionFilter().WithEventActions([]string{"network_request"}).WithLimit(100),
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := store.QuerySessions(ctx, tt.filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Equal(t, tt.wantID, results[0].ID)
			}
		})
	}
}

func TestSQLiteStore_FindPreEventByToolCall(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionID := uuid.New()
	otherSession := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")
	createTestSession(t, store, otherSession, "claude-code")

	base := time.Now().UTC()
	sequence := 0
	save := func(sess uuid.UUID, phase events.Phase, callID string, at time.Duration) *events.Event {
		e := events.NewEvent(sess, "claude-code", events.ActionCommandExec)
		e.Timestamp = base.Add(at)
		e.Phase = phase
		e.ToolCallID = callID
		sequence++
		e.Sequence = sequence
		require.NoError(t, store.SaveEvent(ctx, e))
		return e
	}

	first := save(sessionID, events.PhasePre, "tu-1", 0)
	save(sessionID, events.PhasePre, "tu-1", time.Second)
	save(sessionID, events.PhasePost, "tu-2", 2*time.Second)
	save(otherSession, events.PhasePre, "tu-3", 3*time.Second)

	cases := []struct {
		name    string
		session uuid.UUID
		callID  string
		want    uuid.UUID
	}{
		{"returns the earliest pre event", sessionID, "tu-1", first.ID},
		{"ignores a post event", sessionID, "tu-2", uuid.Nil},
		{"does not match another session", sessionID, "tu-3", uuid.Nil},
		{"empty id finds nothing", sessionID, "", uuid.Nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.FindPreEventByToolCall(ctx, tc.session, tc.callID)
			require.NoError(t, err)
			if tc.want == uuid.Nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.ID)
		})
	}
}

// TestSQLiteStore_InitInParallel opens separate stores at the same time, as
// the decision service does for many accounts. Under -race it fails when two
// ent migrations run at once.
func TestSQLiteStore_InitInParallel(t *testing.T) {
	ctx := context.Background()
	const stores = 4
	var wg sync.WaitGroup
	errs := make(chan error, stores)
	for i := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store, err := NewSQLiteStore(filepath.Join(t.TempDir(), fmt.Sprintf("audit-%d.db", i)))
			if err != nil {
				errs <- err
				return
			}
			errs <- errors.Join(store.Init(ctx), store.Close())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

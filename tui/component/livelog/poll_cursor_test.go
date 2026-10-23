package livelog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCursorStore mimics the SQLite cursor semantics over an in-memory list.
// QueryEventsAfter returns the events after the compound cursor, ordered
// ascending by (timestamp, id), limited to limit entries.
type fakeCursorStore struct {
	storage.ReadStore
	events []*events.Event
}

func (s *fakeCursorStore) QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}

func (s *fakeCursorStore) QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error) {
	sorted := slices.Clone(s.events)
	slices.SortStableFunc(sorted, func(a, b *events.Event) int {
		if c := a.Timestamp.Compare(b.Timestamp); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})

	out := make([]*events.Event, 0, limit)
	for _, e := range sorted {
		if !afterCursor(e, after, afterID) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// afterCursor mirrors the store predicate: TimestampGT(after) or, when the
// caller passes an id, TimestampEQ(after) with a greater id.
func afterCursor(e *events.Event, at time.Time, id uuid.UUID) bool {
	if e.Timestamp.After(at) {
		return true
	}
	if id != uuid.Nil && e.Timestamp.Equal(at) {
		return strings.Compare(e.ID.String(), id.String()) > 0
	}
	return false
}

func pollOnce(t *testing.T, m Model, limit int) (Model, []*events.Event) {
	t.Helper()

	cmd := pollEvents(m.opts.Store, m.cursor(), m.agentFilter, limit)
	msg, ok := cmd().(newEventsMsg)
	require.True(t, ok)

	updated, _ := m.Update(msg)
	return updated.(Model), msg.events
}

// TestPollEvents_NonMatchingBatchThenMatch covers a full batch that the
// agent filter hides, followed by a matching event behind the batch.
func TestPollEvents_NonMatchingBatchThenMatch(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)

	all := make([]*events.Event, 0, 102)
	for i := 0; i < 101; i++ {
		e := events.NewEvent(uuid.New(), "other-agent", events.ActionFileWrite)
		e.Timestamp = base.Add(time.Duration(i) * time.Second)
		all = append(all, e)
	}
	match := events.NewEvent(uuid.New(), "target-agent", events.ActionToolUse)
	match.Timestamp = base.Add(150 * time.Second)
	all = append(all, match)

	store := &fakeCursorStore{events: all}
	m := New(Options{Store: store, AgentFilter: "target-agent"})
	m.lastPollAt = base.Add(-time.Second)

	var seen bool
	for i := 0; i < 4 && !seen; i++ {
		var shown []*events.Event
		m, shown = pollOnce(t, m, 100)
		for _, e := range shown {
			if e.ID == match.ID {
				seen = true
			}
		}
	}

	assert.True(t, seen, "the matching event must reach the display after a hidden batch")
}

// TestPollEvents_NonMatchingBatchAdvancesCursor checks that a batch with no
// matching event still moves the cursor to the last raw event.
func TestPollEvents_NonMatchingBatchAdvancesCursor(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)

	all := make([]*events.Event, 0, 100)
	for i := 0; i < 100; i++ {
		e := events.NewEvent(uuid.New(), "other-agent", events.ActionFileWrite)
		e.Timestamp = base.Add(time.Duration(i) * time.Second)
		all = append(all, e)
	}
	newest := all[len(all)-1]

	store := &fakeCursorStore{events: all}
	m := New(Options{Store: store, AgentFilter: "target-agent"})
	m.lastPollAt = base.Add(-time.Second)

	m, shown := pollOnce(t, m, 100)

	assert.Empty(t, shown, "the agent filter must hide every event")
	assert.True(t, m.lastPollAt.Equal(newest.Timestamp), "the cursor must advance to the last raw event")
	assert.Equal(t, newest.ID, m.lastPollID, "the cursor must keep the last raw event id")
}

// TestPollEvents_EmptyBatchKeepsCursor checks that an empty batch leaves the
// cursor untouched.
func TestPollEvents_EmptyBatchKeepsCursor(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	keptID := uuid.New()

	store := &fakeCursorStore{}
	m := New(Options{Store: store, AgentFilter: "target-agent"})
	m.lastPollAt = base
	m.lastPollID = keptID

	m, shown := pollOnce(t, m, 100)

	assert.Empty(t, shown)
	assert.True(t, m.lastPollAt.Equal(base), "an empty batch must not move the cursor")
	assert.Equal(t, keptID, m.lastPollID, "an empty batch must not change the cursor id")
}

// TestPollEvents_SameTimestampAcrossBatchBoundary checks that the compound
// cursor resumes inside a group of events that share one timestamp.
func TestPollEvents_SameTimestampAcrossBatchBoundary(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)
	tie := base.Add(time.Minute)

	// Fixed-width ids make the (timestamp, id) order deterministic.
	id := func(i int) uuid.UUID {
		return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}

	const total = 120
	all := make([]*events.Event, 0, total)
	for i := 0; i < total; i++ {
		agent := "other-agent"
		if i == 100 {
			agent = "target-agent"
		}
		e := events.NewEvent(uuid.New(), agent, events.ActionFileWrite)
		e.ID = id(i)
		e.Timestamp = tie
		all = append(all, e)
	}

	store := &fakeCursorStore{events: all}
	m := New(Options{Store: store, AgentFilter: "target-agent"})
	m.lastPollAt = base

	m, shown := pollOnce(t, m, 100)
	assert.Empty(t, shown, "the first batch must be hidden")
	require.True(t, m.lastPollAt.Equal(tie))
	assert.Equal(t, id(99), m.lastPollID, "the cursor must point at the last raw event of the batch")

	m, shown = pollOnce(t, m, 100)
	require.Len(t, shown, 1, "the second batch must resume after id 99")
	assert.Equal(t, id(100), shown[0].ID)
}

// TestPollEvents_SQLiteStoreAdvancesPastHiddenBatch uses the real store. A
// hidden batch must not stop the poll from reaching the later matching event.
func TestPollEvents_SQLiteStoreAdvancesPastHiddenBatch(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, &session.Session{
		ID:        sessID,
		AgentName: "other-agent",
		StartedAt: time.Now().UTC(),
	}))

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

	for i := 0; i < 101; i++ {
		e := events.NewEvent(sessID, "other-agent", events.ActionFileWrite)
		e.Sequence = i + 1
		e.Timestamp = base.Add(time.Duration(i) * time.Millisecond)
		require.NoError(t, store.SaveEvent(ctx, e))
	}

	match := events.NewEvent(sessID, "target-agent", events.ActionToolUse)
	match.Sequence = 102
	match.Timestamp = base.Add(150 * time.Millisecond)
	require.NoError(t, store.SaveEvent(ctx, match))

	model := New(Options{Store: store, AgentFilter: "target-agent"})
	model.lastPollAt = base.Add(-time.Millisecond)

	var seen bool
	for i := 0; i < 5 && !seen; i++ {
		var shown []*events.Event
		model, shown = pollOnce(t, model, 100)
		for _, e := range shown {
			if e.ID == match.ID {
				seen = true
			}
		}
	}

	assert.True(t, seen, "the real store poll must reach the matching event after a hidden batch")
}

// TestPollEvents_SQLiteStoreSameTimestamp uses the real store to check that
// the compound cursor reads every event that shares one timestamp.
func TestPollEvents_SQLiteStoreSameTimestamp(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, &session.Session{
		ID:        sessID,
		AgentName: "other-agent",
		StartedAt: time.Now().UTC(),
	}))

	tie := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)

	const total = 120
	for i := 0; i < total; i++ {
		e := events.NewEvent(sessID, "other-agent", events.ActionFileWrite)
		e.Sequence = i + 1
		e.Timestamp = tie
		require.NoError(t, store.SaveEvent(ctx, e))
	}

	model := New(Options{Store: store})
	model.lastPollAt = tie.Add(-time.Second)

	got := map[uuid.UUID]bool{}
	for i := 0; i < 10; i++ {
		var shown []*events.Event
		model, shown = pollOnce(t, model, 100)
		if len(shown) == 0 {
			break
		}
		for _, e := range shown {
			got[e.ID] = true
		}
	}

	assert.Len(t, got, total, "the compound cursor must reach every event that shares a timestamp")
}

// TestInitialLoad_SameTimestampCursorIsConsistent checks that the descending
// initial page, after reversal, yields the maximum (timestamp, id) as its
// cursor. That cursor matches the order that polling resumes with.
func TestInitialLoad_SameTimestampCursorIsConsistent(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, &session.Session{
		ID:        sessID,
		AgentName: "claude-code",
		StartedAt: time.Now().UTC(),
	}))

	tie := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)

	id := func(i int) uuid.UUID {
		return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}

	// Insert in reverse id order, so the row order cannot happen to match
	// the id order.
	const total = 120
	for i := total - 1; i >= 0; i-- {
		e := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
		e.ID = id(i)
		e.Sequence = i + 1
		e.Timestamp = tie
		require.NoError(t, store.SaveEvent(ctx, e))
	}

	// The initial page takes the newest events and cuts the tie group.
	const initialLimit = 50
	m := New(Options{Store: store, InitialLimit: initialLimit})
	cmd := loadInitialEvents(store, tie.Add(-time.Second), m.agentFilter, initialLimit)
	msg, ok := cmd().(newEventsMsg)
	require.True(t, ok)
	require.Len(t, msg.events, initialLimit)

	// The cursor must be the maximum (timestamp, id) of the page.
	require.True(t, msg.cursor.at.Equal(tie), "cursor must hold the shared timestamp")
	require.Equal(t, id(total-1), msg.cursor.id, "cursor must hold the largest id of the page")

	// The page must hold the largest ids in ascending order.
	for i, e := range msg.events {
		require.Equal(t, id(total-initialLimit+i), e.ID)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	seen := map[uuid.UUID]int{}
	for _, e := range msg.events {
		seen[e.ID]++
	}

	// Polling resumes after the cursor and must not repeat a shown event.
	for i := 0; i < 5; i++ {
		var shown []*events.Event
		m, shown = pollOnce(t, m, initialLimit)
		if len(shown) == 0 {
			break
		}
		for _, e := range shown {
			seen[e.ID]++
		}
	}

	for i := 0; i < total; i++ {
		assert.LessOrEqual(t, seen[id(i)], 1, "event %d must not be shown twice", i)
	}
}

package livelog

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingStore records the cursor of the last QueryEventsAfter call.
// It embeds storage.ReadStore. QueryEvents returns the preset initial
// list. The list is empty by default.
type recordingStore struct {
	storage.ReadStore
	after   time.Time
	initial []*events.Event
}

func (s *recordingStore) QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error) {
	return s.initial, nil
}

func (s *recordingStore) QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error) {
	s.after = after
	return nil, nil
}

func TestCycleAgentFilter(t *testing.T) {
	m := New(Options{AgentNames: []string{"claude-code", "cursor"}})
	assert.Equal(t, "", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "claude-code", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "cursor", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestCycleAgentFilter_UnknownFilterResets(t *testing.T) {
	m := New(Options{AgentNames: []string{"claude-code"}, AgentFilter: "gone-agent"})

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestCycleAgentFilter_EmptyNameList(t *testing.T) {
	m := New(Options{})

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestAgentBadge_UnknownAgentRendersRawName(t *testing.T) {
	assert.Contains(t, agentBadge("future-agent"), "future-agent")
}

func TestAgentBadge_KnownAgentRendersName(t *testing.T) {
	assert.Contains(t, agentBadge("claude-code"), "claude-code")
}

func TestUpdate_EmptyInitialLoadSetsLastPollAtInUTC(t *testing.T) {
	store := &recordingStore{}
	m := New(Options{Store: store, Since: time.Now().UTC().Add(-time.Hour)})

	initialCmd := loadInitialEvents(m.opts.Store, m.opts.Since, m.agentFilter, m.opts.initialLimit())
	require.NotNil(t, initialCmd)

	initialMsg, ok := initialCmd().(newEventsMsg)
	require.True(t, ok)
	require.Empty(t, initialMsg.events)

	updated, _ := m.Update(initialMsg)
	m = updated.(Model)

	require.False(t, m.lastPollAt.IsZero(), "cursor must be set after the initial load")
	assert.Same(t, time.UTC, m.lastPollAt.Location(), "cursor must use UTC")
	_, cursorOffset := m.lastPollAt.Zone()
	assert.Equal(t, 0, cursorOffset, "cursor offset must be zero")

	pollCmd := pollEvents(m.opts.Store, m.cursor(), m.agentFilter, 100)
	require.NotNil(t, pollCmd)
	pollCmd()

	require.False(t, store.after.IsZero(), "poll must use a real cursor")
	assert.Same(t, time.UTC, store.after.Location(), "poll cursor must use UTC")
	_, pollOffset := store.after.Zone()
	assert.Equal(t, 0, pollOffset, "poll cursor offset must be zero")
}

// TestUpdate_NonUTCTimestampCursorNormalizedToUTC checks that a stored event
// with a positive UTC offset does not set an offset cursor.
func TestUpdate_NonUTCTimestampCursorNormalizedToUTC(t *testing.T) {
	evt := events.NewEvent(uuid.New(), "claude-code", events.ActionFileWrite)
	evt.Timestamp = time.Now().In(time.FixedZone("IST", 5*3600+1800))

	store := &recordingStore{initial: []*events.Event{evt}}
	m := New(Options{Store: store, Since: time.Now().UTC().Add(-time.Hour)})

	initialCmd := loadInitialEvents(m.opts.Store, m.opts.Since, m.agentFilter, m.opts.initialLimit())
	require.NotNil(t, initialCmd)

	initialMsg, ok := initialCmd().(newEventsMsg)
	require.True(t, ok)
	require.NotEmpty(t, initialMsg.events)
	require.Same(t, evt, initialMsg.events[len(initialMsg.events)-1])

	updated, _ := m.Update(initialMsg)
	m = updated.(Model)

	require.False(t, m.lastPollAt.IsZero(), "cursor must be set after the initial load")
	assert.Same(t, time.UTC, m.lastPollAt.Location(), "cursor must use UTC")
	_, cursorOffset := m.lastPollAt.Zone()
	assert.Equal(t, 0, cursorOffset, "cursor offset must be zero")
	assert.True(t, m.lastPollAt.Equal(evt.Timestamp), "cursor must keep the newest event instant")

	pollCmd := pollEvents(m.opts.Store, m.cursor(), m.agentFilter, 100)
	require.NotNil(t, pollCmd)
	pollCmd()

	require.False(t, store.after.IsZero(), "poll must use a real cursor")
	assert.Same(t, time.UTC, store.after.Location(), "poll cursor must use UTC")
	_, pollOffset := store.after.Zone()
	assert.Equal(t, 0, pollOffset, "poll cursor offset must be zero")
	assert.True(t, store.after.Equal(evt.Timestamp), "poll cursor must keep the newest event instant")
}

func TestLoadInitialEvents_EmptyStoreReturnsNoEvents(t *testing.T) {
	store := &recordingStore{}
	cmd := loadInitialEvents(store, time.Now().UTC().Add(-time.Hour), "", 50)
	require.NotNil(t, cmd)

	msg, ok := cmd().(newEventsMsg)
	require.True(t, ok)
	assert.Empty(t, msg.events)
}

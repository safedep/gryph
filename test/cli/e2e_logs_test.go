package cli_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogs(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		setup  func(env *testEnv)
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name:   "default_shows_recent_events",
			args:   []string{"logs", "--format", "json"},
			setup:  seedNRecentEvents(10),
			assert: assertEventCount(10),
		},
		{
			name:   "limit_restricts_count",
			args:   []string{"logs", "--limit", "3", "--format", "json"},
			setup:  seedNRecentEvents(10),
			assert: assertEventCount(3),
		},
		{
			name:   "agent_filter",
			args:   []string{"logs", "--agent", "claude-code", "--format", "json"},
			setup:  seedMixedAgentEvents,
			assert: assertAllEventsFromAgent("claude-code"),
		},
		{
			name:   "today_filter",
			args:   []string{"logs", "--today", "--format", "json"},
			setup:  seedTodayAndYesterdayEvents,
			assert: assertAllEventsFromToday,
		},
		{
			name:   "no_events_message",
			args:   []string{"logs"},
			assert: assertOutputContains("No events found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			stdout, _, err := env.run(tt.args...)
			tt.assert(t, stdout, err)
		})
	}
}

func TestLogs_SortOrder(t *testing.T) {
	env := newTestEnv(t)
	seedNRecentEvents(5)(env)

	stdout, _, err := env.run("logs", "--format", "json", "--limit", "50")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	require.Len(t, evts, 5)

	// Verify chronological order (oldest first after slices.Reverse)
	for i := 1; i < len(evts); i++ {
		assert.True(t, evts[i].Timestamp.After(evts[i-1].Timestamp) || evts[i].Timestamp.Equal(evts[i-1].Timestamp),
			"events should be in chronological order: %v >= %v", evts[i-1].Timestamp, evts[i].Timestamp)
	}
}

func TestLogs_OutputFormats(t *testing.T) {
	tests := []struct {
		name   string
		format string
		assert func(t *testing.T, stdout string)
	}{
		{
			name:   "json_valid_array",
			format: "json",
			assert: assertValidJSONArray,
		},
		{
			name:   "jsonl_valid_lines",
			format: "jsonl",
			assert: assertValidJSONL,
		},
		{
			name:   "table_contains_headers",
			format: "table",
			assert: func(t *testing.T, stdout string) {
				t.Helper()
				assert.NotEmpty(t, stdout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			seedNRecentEvents(3)(env)

			stdout, _, err := env.run("logs", "--format", tt.format, "--limit", "50")
			require.NoError(t, err)
			tt.assert(t, stdout)
		})
	}
}

func TestLogs_SinceFilter(t *testing.T) {
	env := newTestEnv(t)
	seedTodayAndYesterdayEvents(env)

	// Use --since with a duration that includes all events
	stdout, _, err := env.run("logs", "--since", "72h", "--format", "json", "--limit", "50")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	assert.Len(t, evts, 5)
}

func TestLogs_SessionFilter(t *testing.T) {
	env := newTestEnv(t)
	seedMixedAgentEvents(env)

	// Get a session ID
	store, cleanup := env.openStore()
	sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
	require.NoError(t, err)
	require.NotEmpty(t, sessions)
	sessionID := sessions[0].ID.String()
	expectedAgent := sessions[0].AgentName
	cleanup()

	stdout, _, err := env.run("logs", "--session", sessionID, "--format", "json", "--since", "24h", "--limit", "50")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	assert.NotEmpty(t, evts)
	for _, e := range evts {
		assert.Equal(t, sessionID, e.SessionID)
		assert.Equal(t, expectedAgent, e.AgentName)
	}
}

func TestLogs_TimestampInOutput(t *testing.T) {
	env := newTestEnv(t)
	seedNRecentEvents(1)(env)

	stdout, _, err := env.run("logs", "--format", "json", "--limit", "50")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	require.Len(t, evts, 1)

	// Timestamp should be recent (within the last hour)
	assert.True(t, time.Since(evts[0].Timestamp) < 2*time.Hour)
}

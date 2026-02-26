package cli_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		setup  func(env *testEnv)
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name:   "by_action_file_write",
			args:   []string{"query", "--action", "file_write", "--format", "json"},
			setup:  seedMixedActions,
			assert: assertAllActionsAre("file_write"),
		},
		{
			name:   "by_multiple_actions",
			args:   []string{"query", "--action", "file_read", "--action", "file_write", "--format", "json"},
			setup:  seedMixedActions,
			assert: assertActionsIn("file_read", "file_write"),
		},
		{
			name:  "by_file_pattern",
			args:  []string{"query", "--file", "*.go", "--format", "json"},
			setup: seedWithPaths,
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.NotEmpty(t, evts)
				for _, e := range evts {
					assert.Contains(t, e.Path, ".go")
				}
			},
		},
		{
			name:  "by_command_pattern",
			args:  []string{"query", "--command", "npm *", "--format", "json"},
			setup: seedWithCommands,
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.NotEmpty(t, evts)
				for _, e := range evts {
					assert.Contains(t, e.Command, "npm")
				}
			},
		},
		{
			name:  "by_status_error",
			args:  []string{"query", "--status", "error", "--format", "json"},
			setup: seedWithErrors,
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.NotEmpty(t, evts)
				for _, e := range evts {
					assert.Equal(t, "error", e.ResultStatus)
				}
			},
		},
		{
			name:   "count_mode",
			args:   []string{"query", "--count"},
			setup:  seedNRecentEvents(15),
			assert: assertOutputContains("15 events"),
		},
		{
			name:   "no_results",
			args:   []string{"query", "--action", "file_delete"},
			setup:  seedMixedActions,
			assert: assertOutputContains("No events found"),
		},
		{
			name:  "csv_format",
			args:  []string{"query", "--format", "csv"},
			setup: seedNRecentEvents(5),
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				assertValidCSV(5)(t, stdout)
			},
		},
		{
			name:   "today_filter",
			args:   []string{"query", "--today", "--format", "json"},
			setup:  seedTodayAndYesterdayEvents,
			assert: assertAllEventsFromToday,
		},
		{
			name:   "yesterday_filter",
			args:   []string{"query", "--yesterday", "--format", "json"},
			setup:  seedTodayAndYesterdayEvents,
			assert: assertAllEventsFromYesterday,
		},
		{
			name:   "agent_filter",
			args:   []string{"query", "--agent", "cursor", "--format", "json"},
			setup:  seedMixedAgentEvents,
			assert: assertAllEventsFromAgent("cursor"),
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

func TestQuery_SortOrder(t *testing.T) {
	env := newTestEnv(t)
	seedNRecentEvents(5)(env)

	stdout, _, err := env.run("query", "--format", "json")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	require.Len(t, evts, 5)

	for i := 1; i < len(evts); i++ {
		assert.True(t, evts[i].Timestamp.After(evts[i-1].Timestamp) || evts[i].Timestamp.Equal(evts[i-1].Timestamp),
			"events should be in chronological order")
	}
}

func TestQuery_Pagination(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		offset int
		total  int
		expect int
	}{
		{"first_page", 5, 0, 20, 5},
		{"second_page", 5, 5, 20, 5},
		{"beyond_end", 5, 100, 20, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			seedNRecentEvents(tt.total)(env)

			args := []string{"query", "--format", "json",
				"--limit", itoa(tt.limit),
				"--offset", itoa(tt.offset)}
			stdout, _, err := env.run(args...)

			if tt.expect == 0 {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "No events found")
			} else {
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.Len(t, evts, tt.expect)
			}
		})
	}
}

func TestQuery_SessionPrefixMatch(t *testing.T) {
	env := newTestEnv(t)
	seedNRecentEvents(5)(env)

	// Get a session ID
	store, cleanup := env.openStore()
	sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
	require.NoError(t, err)
	require.NotEmpty(t, sessions)
	sessionID := sessions[0].ID.String()
	prefix := sessionID[:8]
	cleanup()

	stdout, _, err := env.run("query", "--session", prefix, "--format", "json")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	assert.Len(t, evts, 5)
	for _, e := range evts {
		assert.Equal(t, sessionID, e.SessionID)
	}
}

func TestQuery_FilterComposition(t *testing.T) {
	env := newTestEnv(t)
	seedMixedActions(env)

	// Combine agent + action filters
	stdout, _, err := env.run("query", "--agent", "claude-code", "--action", "file_write", "--format", "json")
	require.NoError(t, err)

	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	for _, e := range evts {
		assert.Equal(t, "claude-code", e.AgentName)
		assert.Equal(t, "file_write", e.ActionType)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

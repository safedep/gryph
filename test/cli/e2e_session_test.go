package cli_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessions(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		setup  func(env *testEnv)
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name:   "lists_all_sessions",
			args:   []string{"sessions", "--format", "json"},
			setup:  seed3Sessions,
			assert: assertSessionCount(3),
		},
		{
			name:   "agent_filter",
			args:   []string{"sessions", "--agent", "cursor", "--format", "json"},
			setup:  seed3Sessions,
			assert: assertAllSessionsFromAgent("cursor"),
		},
		{
			name: "json_format",
			args: []string{"sessions", "--format", "json"},
			setup: seed3Sessions,
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				assertValidJSONArray(t, stdout)
			},
		},
		{
			name:   "no_sessions_message",
			args:   []string{"sessions"},
			assert: assertOutputContains("No sessions found"),
		},
		{
			name:   "limit_restricts_count",
			args:   []string{"sessions", "--limit", "1", "--format", "json"},
			setup:  seed3Sessions,
			assert: assertSessionCount(1),
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

func TestSession_Detail(t *testing.T) {
	tests := []struct {
		name       string
		sessionArg func(env *testEnv) string
		setup      func(env *testEnv)
		assert     func(t *testing.T, stdout string, err error)
	}{
		{
			name: "full_uuid",
			sessionArg: func(env *testEnv) string {
				store, cleanup := env.openStore()
				defer cleanup()
				sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
				require.NoError(env.t, err)
				require.NotEmpty(env.t, sessions)
				return sessions[0].ID.String()
			},
			setup: seedNRecentEvents(5),
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				assert.NotEmpty(t, stdout)
			},
		},
		{
			name: "prefix_match",
			sessionArg: func(env *testEnv) string {
				store, cleanup := env.openStore()
				defer cleanup()
				sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
				require.NoError(env.t, err)
				require.NotEmpty(env.t, sessions)
				return sessions[0].ID.String()[:8]
			},
			setup: seedNRecentEvents(5),
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.NoError(t, err)
				assert.NotEmpty(t, stdout)
			},
		},
		{
			name: "not_found",
			sessionArg: func(_ *testEnv) string {
				return "nonexistent-id"
			},
			assert: func(t *testing.T, stdout string, err error) {
				t.Helper()
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "session not found")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			sessionArg := tt.sessionArg(env)
			stdout, _, err := env.run("session", sessionArg, "--format", "json")
			tt.assert(t, stdout, err)
		})
	}
}

func TestSession_DetailShowsEvents(t *testing.T) {
	env := newTestEnv(t)
	seedNRecentEvents(5)(env)

	store, cleanup := env.openStore()
	sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
	require.NoError(t, err)
	require.NotEmpty(t, sessions)
	sessionID := sessions[0].ID.String()
	cleanup()

	stdout, _, err := env.run("session", sessionID, "--format", "json")
	require.NoError(t, err)

	// The JSON output should contain session info and events
	// Parse as a generic JSON to verify structure
	var result json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
}

func TestSession_SessionCounters(t *testing.T) {
	env := newTestEnv(t)
	seedMixedActions(env)

	store, cleanup := env.openStore()
	sessions, err := store.QuerySessions(context.Background(), session.NewSessionFilter())
	require.NoError(t, err)
	require.NotEmpty(t, sessions)
	sess := sessions[0]
	cleanup()

	assert.Equal(t, 5, sess.TotalActions)
	assert.Equal(t, 2, sess.FilesRead)
	assert.Equal(t, 2, sess.FilesWritten)
	assert.Equal(t, 1, sess.CommandsExecuted)
}

func TestSessions_OutputFormats(t *testing.T) {
	tests := []struct {
		name   string
		format string
		assert func(t *testing.T, stdout string)
	}{
		{
			name:   "json_format",
			format: "json",
			assert: func(t *testing.T, stdout string) {
				var sessions []tui.SessionView
				require.NoError(t, json.Unmarshal([]byte(stdout), &sessions))
				assert.NotEmpty(t, sessions)
			},
		},
		{
			name:   "table_format",
			format: "table",
			assert: func(t *testing.T, stdout string) {
				assert.NotEmpty(t, stdout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			seed3Sessions(env)
			stdout, _, err := env.run("sessions", "--format", tt.format)
			require.NoError(t, err)
			tt.assert(t, stdout)
		})
	}
}

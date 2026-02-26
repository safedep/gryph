package cli_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHook_ClaudeCode(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "file_write_via_pre_tool_use",
			hookType: "PreToolUse",
			fixture:  "../../agent/claudecode/testdata/pre_tool_use_write.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "file_read_via_post_tool_use",
			hookType: "PostToolUse",
			fixture:  "../../agent/claudecode/testdata/post_tool_use_read.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
				assert.Equal(t, events.ResultSuccess, evts[0].ResultStatus)
			},
		},
		{
			name:     "command_exec_via_pre_tool_use",
			hookType: "PreToolUse",
			fixture:  "../../agent/claudecode/testdata/pre_tool_use_bash.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionCommandExec, evts[0].ActionType)
				p, pErr := evts[0].GetCommandExecPayload()
				require.NoError(t, pErr)
				assert.Contains(t, p.Command, "npm install")
			},
		},
		{
			name:     "session_start",
			hookType: "SessionStart",
			fixture:  "../../agent/claudecode/testdata/session_start.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionSessionStart, evts[0].ActionType)
			},
		},
		{
			name:     "session_end",
			hookType: "SessionEnd",
			fixture:  "../../agent/claudecode/testdata/session_end.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()

				sessions, sErr := store.QuerySessions(ctx, session.NewSessionFilter())
				require.NoError(t, sErr)
				require.Len(t, sessions, 1)
				assert.False(t, sessions[0].EndedAt.IsZero(), "EndedAt should be set")
			},
		},
		{
			name:     "failure_event",
			hookType: "PostToolUse",
			fixture:  "../../agent/claudecode/testdata/post_tool_use_failure.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ResultError, evts[0].ResultStatus)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("claude-code", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_UnknownAgent(t *testing.T) {
	env := newTestEnv(t)
	payload := []byte(`{"session_id": "test", "hook_event_name": "PostToolUse"}`)
	_, _, err := env.runHook("nonexistent-agent", "PostToolUse", payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent")
}

func TestHook_SessionLifecycle(t *testing.T) {
	env := newTestEnv(t)

	fixtures := []struct {
		hookType string
		fixture  string
	}{
		{"SessionStart", "../../agent/claudecode/testdata/session_start.json"},
		{"PreToolUse", "../../agent/claudecode/testdata/pre_tool_use_write.json"},
		{"PostToolUse", "../../agent/claudecode/testdata/post_tool_use_read.json"},
		{"SessionEnd", "../../agent/claudecode/testdata/session_end.json"},
	}

	for _, f := range fixtures {
		payload, err := os.ReadFile(f.fixture)
		require.NoError(t, err)
		_, _, err = env.runHook("claude-code", f.hookType, payload)
		require.NoError(t, err)
	}

	store, cleanup := env.openStore()
	defer cleanup()
	ctx := context.Background()

	evts, err := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	assert.Len(t, evts, 4)

	// DB returns newest first, so verify descending sequences
	for i := 0; i < len(evts)-1; i++ {
		assert.Greater(t, evts[i].Sequence, evts[i+1].Sequence,
			"events should be in newest-first order from DB")
	}

	// Verify all sequences 1-4 are present
	seqs := make(map[int]bool)
	for _, evt := range evts {
		seqs[evt.Sequence] = true
	}
	for i := 1; i <= 4; i++ {
		assert.True(t, seqs[i], "sequence %d should exist", i)
	}

	// Verify session ended
	sessions, err := store.QuerySessions(ctx, session.NewSessionFilter())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.False(t, sessions[0].EndedAt.IsZero())
	assert.Equal(t, 4, sessions[0].TotalActions)
}

func TestHook_DeterministicSessionID(t *testing.T) {
	env := newTestEnv(t)

	// Send two events with the same session_id
	payload, err := os.ReadFile("../../agent/claudecode/testdata/pre_tool_use_write.json")
	require.NoError(t, err)

	_, _, err = env.runHook("claude-code", "PreToolUse", payload)
	require.NoError(t, err)

	payload2, err := os.ReadFile("../../agent/claudecode/testdata/post_tool_use_read.json")
	require.NoError(t, err)

	_, _, err = env.runHook("claude-code", "PostToolUse", payload2)
	require.NoError(t, err)

	store, cleanup := env.openStore()
	defer cleanup()
	ctx := context.Background()

	evts, err := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	require.Len(t, evts, 2)

	// Both events should have the same session ID
	assert.Equal(t, evts[0].SessionID, evts[1].SessionID)

	// Verify it's deterministic based on "test-session-123"
	expected := uuid.NewSHA1(uuid.NameSpaceOID, []byte("test-session-123"))
	assert.Equal(t, expected, evts[0].SessionID)
}

func TestHook_SequenceNumbering(t *testing.T) {
	env := newTestEnv(t)

	// Send 3 events with correct hook types
	fixtures := []struct {
		hookType string
		fixture  string
	}{
		{"PreToolUse", "../../agent/claudecode/testdata/pre_tool_use_write.json"},
		{"PostToolUse", "../../agent/claudecode/testdata/post_tool_use_read.json"},
		{"PreToolUse", "../../agent/claudecode/testdata/pre_tool_use_bash.json"},
	}

	for _, f := range fixtures {
		payload, err := os.ReadFile(f.fixture)
		require.NoError(t, err)
		_, _, err = env.runHook("claude-code", f.hookType, payload)
		require.NoError(t, err)
	}

	store, cleanup := env.openStore()
	defer cleanup()
	ctx := context.Background()

	evts, err := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	require.Len(t, evts, 3)

	// Verify all sequences 1-3 are present
	seqs := make(map[int]bool)
	for _, evt := range evts {
		seqs[evt.Sequence] = true
	}
	for i := 1; i <= 3; i++ {
		assert.True(t, seqs[i], "sequence %d should exist", i)
	}
}

func TestHook_Cursor(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "before_read_file",
			hookType: "beforeReadFile",
			fixture:  "../../agent/cursor/testdata/before_read_file.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
			},
		},
		{
			name:     "after_file_edit",
			hookType: "afterFileEdit",
			fixture:  "../../agent/cursor/testdata/after_file_edit.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "stop_ends_session",
			hookType: "stop",
			fixture:  "../../agent/cursor/testdata/stop.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				sessions, sErr := store.QuerySessions(ctx, session.NewSessionFilter())
				require.NoError(t, sErr)
				require.Len(t, sessions, 1)
				assert.False(t, sessions[0].EndedAt.IsZero())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("cursor", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_Gemini(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "before_tool_read_file",
			hookType: "BeforeTool",
			fixture:  "../../agent/gemini/testdata/before_tool_read_file.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
			},
		},
		{
			name:     "before_tool_write_file",
			hookType: "BeforeTool",
			fixture:  "../../agent/gemini/testdata/before_tool_write_file.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "before_tool_shell",
			hookType: "BeforeTool",
			fixture:  "../../agent/gemini/testdata/before_tool_shell.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionCommandExec, evts[0].ActionType)
			},
		},
		{
			name:     "session_end",
			hookType: "SessionEnd",
			fixture:  "../../agent/gemini/testdata/session_end.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				sessions, sErr := store.QuerySessions(ctx, session.NewSessionFilter())
				require.NoError(t, sErr)
				require.Len(t, sessions, 1)
				assert.False(t, sessions[0].EndedAt.IsZero())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("gemini", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_OpenCode(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "tool_execute_before_read",
			hookType: "tool.execute.before",
			fixture:  "../../agent/opencode/testdata/tool_execute_before_read.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
			},
		},
		{
			name:     "tool_execute_before_write",
			hookType: "tool.execute.before",
			fixture:  "../../agent/opencode/testdata/tool_execute_before_write.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "tool_execute_before_bash",
			hookType: "tool.execute.before",
			fixture:  "../../agent/opencode/testdata/tool_execute_before_bash.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionCommandExec, evts[0].ActionType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("opencode", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_Windsurf(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "pre_read_code",
			hookType: "pre_read_code",
			fixture:  "../../agent/windsurf/testdata/pre_read_code.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
			},
		},
		{
			name:     "pre_write_code",
			hookType: "pre_write_code",
			fixture:  "../../agent/windsurf/testdata/pre_write_code.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "pre_run_command",
			hookType: "pre_run_command",
			fixture:  "../../agent/windsurf/testdata/pre_run_command.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionCommandExec, evts[0].ActionType)
				p, pErr := evts[0].GetCommandExecPayload()
				require.NoError(t, pErr)
				assert.Equal(t, "npm install", p.Command)
			},
		},
		{
			name:     "pre_mcp_tool_use",
			hookType: "pre_mcp_tool_use",
			fixture:  "../../agent/windsurf/testdata/pre_mcp_tool_use.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionToolUse, evts[0].ActionType)
			},
		},
		{
			name:     "post_cascade_response",
			hookType: "post_cascade_response",
			fixture:  "../../agent/windsurf/testdata/post_cascade_response.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionNotification, evts[0].ActionType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("windsurf", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_Windsurf_DeterministicSessionID(t *testing.T) {
	env := newTestEnv(t)

	payload1, err := os.ReadFile("../../agent/windsurf/testdata/pre_read_code.json")
	require.NoError(t, err)
	_, _, err = env.runHook("windsurf", "pre_read_code", payload1)
	require.NoError(t, err)

	payload2, err := os.ReadFile("../../agent/windsurf/testdata/pre_run_command.json")
	require.NoError(t, err)
	_, _, err = env.runHook("windsurf", "pre_run_command", payload2)
	require.NoError(t, err)

	store, cleanup := env.openStore()
	defer cleanup()
	ctx := context.Background()

	evts, err := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	require.Len(t, evts, 2)

	assert.Equal(t, evts[0].SessionID, evts[1].SessionID)

	expected := uuid.NewSHA1(uuid.NameSpaceOID, []byte("traj-test-123"))
	assert.Equal(t, expected, evts[0].SessionID)
}

func TestHook_LoggingLevel(t *testing.T) {
	tests := []struct {
		name          string
		loggingLevel  string
		expectContent bool
	}{
		{"minimal_strips_content", "minimal", false},
		{"standard_keeps_payloads", "standard", true},
		{"full_keeps_everything", "full", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnvWithConfig(t, "")
			// Override the config with the specific logging level
			configYAML := fmt.Sprintf(`logging:
  level: %s
  content_hash: true
storage:
  path: %s
  retention_days: 90
display:
  colors: never
`, tt.loggingLevel, env.dbPath)
			require.NoError(t, os.WriteFile(env.configPath, []byte(configYAML), 0600))

			payload, err := os.ReadFile("../../agent/claudecode/testdata/post_tool_use_read.json")
			require.NoError(t, err)

			_, _, err = env.runHook("claude-code", "PostToolUse", payload)
			require.NoError(t, err)

			store, cleanup := env.openStore()
			defer cleanup()
			ctx := context.Background()
			evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
			require.NoError(t, qErr)
			require.Len(t, evts, 1)

			if tt.expectContent {
				assert.NotEmpty(t, evts[0].Payload, "payload should be present for level %s", tt.loggingLevel)
			}
			// For minimal, raw event and conversation context should be stripped
			if tt.loggingLevel == "minimal" {
				assert.Empty(t, evts[0].RawEvent)
				assert.Empty(t, evts[0].ConversationContext)
			}
		})
	}
}

func TestHook_PiAgent(t *testing.T) {
	tests := []struct {
		name     string
		hookType string
		fixture  string
		assert   func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:     "session_start",
			hookType: "session_start",
			fixture:  "../../agent/piagent/testdata/session_start.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionSessionStart, evts[0].ActionType)
			},
		},
		{
			name:     "session_shutdown",
			hookType: "session_shutdown",
			fixture:  "../../agent/piagent/testdata/session_shutdown.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				sessions, sErr := store.QuerySessions(ctx, session.NewSessionFilter())
				require.NoError(t, sErr)
				require.Len(t, sessions, 1)
				assert.False(t, sessions[0].EndedAt.IsZero(), "EndedAt should be set")
			},
		},
		{
			name:     "tool_call_read",
			hookType: "tool_call",
			fixture:  "../../agent/piagent/testdata/tool_call_read.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
			},
		},
		{
			name:     "tool_call_write",
			hookType: "tool_call",
			fixture:  "../../agent/piagent/testdata/tool_call_write.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileWrite, evts[0].ActionType)
			},
		},
		{
			name:     "tool_call_bash",
			hookType: "tool_call",
			fixture:  "../../agent/piagent/testdata/tool_call_bash.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionCommandExec, evts[0].ActionType)
				p, pErr := evts[0].GetCommandExecPayload()
				require.NoError(t, pErr)
				assert.Contains(t, p.Command, "npm install")
			},
		},
		{
			name:     "tool_result_success",
			hookType: "tool_result",
			fixture:  "../../agent/piagent/testdata/tool_result_success.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ActionFileRead, evts[0].ActionType)
				assert.Equal(t, events.ResultSuccess, evts[0].ResultStatus)
			},
		},
		{
			name:     "tool_result_error",
			hookType: "tool_result",
			fixture:  "../../agent/piagent/testdata/tool_result_error.json",
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				store, cleanup := env.openStore()
				defer cleanup()
				ctx := context.Background()
				evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 1)
				assert.Equal(t, events.ResultError, evts[0].ResultStatus)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			payload, err := os.ReadFile(tt.fixture)
			require.NoError(t, err)
			stdout, stderr, runErr := env.runHook("pi-agent", tt.hookType, payload)
			tt.assert(t, env, stdout, stderr, runErr)
		})
	}
}

func TestHook_PiAgent_DeterministicSessionID(t *testing.T) {
	env := newTestEnv(t)

	sessionStartPayload, err := os.ReadFile("../../agent/piagent/testdata/session_start.json")
	require.NoError(t, err)

	toolCallPayload, err := os.ReadFile("../../agent/piagent/testdata/tool_call_read.json")
	require.NoError(t, err)

	_, _, err = env.runHook("pi-agent", "session_start", sessionStartPayload)
	require.NoError(t, err)

	_, _, err = env.runHook("pi-agent", "tool_call", toolCallPayload)
	require.NoError(t, err)

	store, cleanup := env.openStore()
	defer cleanup()
	ctx := context.Background()
	evts, qErr := store.QueryEvents(ctx, events.NewEventFilter())
	require.NoError(t, qErr)
	require.Len(t, evts, 2)

	assert.Equal(t, evts[0].SessionID, evts[1].SessionID,
		"events with same session_id should have same UUID")
}

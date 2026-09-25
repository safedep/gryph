package cli_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport(t *testing.T) {
	tests := []struct {
		name   string
		args   func(env *testEnv) []string
		setup  func(env *testEnv)
		assert func(t *testing.T, env *testEnv, stdout, stderr string, err error)
	}{
		{
			name:  "jsonl_format",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedNRecentEvents(10),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 10)
				for _, line := range lines {
					var evt events.Event
					require.NoError(t, json.Unmarshal([]byte(line), &evt))
					assert.NotEmpty(t, evt.ID)
					assert.NotEmpty(t, evt.AgentName)
				}
				assert.Contains(t, stderr, "Exported 10 events")
			},
		},
		{
			name:  "all_events_no_limit",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedNRecentEvents(50),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 50)
				assert.Contains(t, stderr, "Exported 50 events")
			},
		},
		{
			name:  "agent_filter",
			args:  func(_ *testEnv) []string { return []string{"export", "--agent", "claude-code"} },
			setup: seedMixedAgentEvents,
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.NotEmpty(t, lines)
				for _, line := range lines {
					var evt events.Event
					require.NoError(t, json.Unmarshal([]byte(line), &evt))
					assert.Equal(t, "claude-code", evt.AgentName)
				}
			},
		},
		{
			name: "to_file",
			args: func(env *testEnv) []string {
				outPath := filepath.Join(env.tmpDir, "export.jsonl")
				return []string{"export", "-o", outPath}
			},
			setup: seedNRecentEvents(5),
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				outPath := filepath.Join(env.tmpDir, "export.jsonl")
				data, readErr := os.ReadFile(outPath)
				require.NoError(t, readErr)
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				assert.Len(t, lines, 5)
				for _, line := range lines {
					var evt events.Event
					require.NoError(t, json.Unmarshal([]byte(line), &evt))
					assert.NotEmpty(t, evt.ID)
				}
				assert.Contains(t, stderr, "Exported 5 events")
			},
		},
		{
			name: "empty_db",
			args: func(_ *testEnv) []string { return []string{"export"} },
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				assert.Empty(t, strings.TrimSpace(stdout))
				assert.Contains(t, stderr, "No events")
			},
		},
		{
			name:  "sensitive_events_exported_with_default_profile",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedSensitiveEvents(3, 2),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 5, "the default profile drops content, not events")
				assert.Contains(t, stderr, "Exported 5 events")
				assert.NotContains(t, stdout, "SENSITIVE_MARKER")
				assert.NotContains(t, stdout, `"raw_event"`)
			},
		},
		{
			name:  "full_profile",
			args:  func(_ *testEnv) []string { return []string{"export", "--export-profile", "full"} },
			setup: seedSensitiveEvents(3, 2),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 5)
				assert.Contains(t, stderr, "Exported 5 events")
				assert.Contains(t, stdout, "SENSITIVE_MARKER in the error")
				assert.Contains(t, stdout, "SENSITIVE_MARKER in the raw event")
			},
		},
		{
			name:  "sensitive_means_full",
			args:  func(_ *testEnv) []string { return []string{"export", "--sensitive"} },
			setup: seedSensitiveEvents(1, 1),
			assert: func(t *testing.T, _ *testEnv, stdout, _ string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "SENSITIVE_MARKER in the raw event")
			},
		},
		{
			name:  "sensitive_with_profile",
			args:  func(_ *testEnv) []string { return []string{"export", "--sensitive", "--export-profile", "metadata"} },
			setup: seedSensitiveEvents(1, 1),
			assert: func(t *testing.T, _ *testEnv, _, _ string, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "--sensitive cannot be combined with --export-profile")
			},
		},
		{
			name:  "unknown_profile",
			args:  func(_ *testEnv) []string { return []string{"export", "--export-profile", "nope"} },
			setup: seedSensitiveEvents(1, 0),
			assert: func(t *testing.T, _ *testEnv, _, _ string, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown export profile")
			},
		},
		{
			name:   "sensitive_write_hash_default_profile",
			args:   func(_ *testEnv) []string { return []string{"export"} },
			setup:  hookSensitiveWrite,
			assert: assertNoPlainDigest(sensitiveWriteContent, true),
		},
		{
			name:   "sensitive_write_hash_metadata_profile",
			args:   func(_ *testEnv) []string { return []string{"export", "--export-profile", "metadata"} },
			setup:  hookSensitiveWrite,
			assert: assertNoPlainDigest(sensitiveWriteContent, true),
		},
		{
			name:  "short_prompt_digest_default_profile",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: hookShortPrompt,
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assertNoPlainDigest(shortPrompt, false)(t, env, stdout, stderr, err)
				assert.Contains(t, stdout, `"digest":"hmac-sha256:`)
			},
		},
		{
			name:  "short_prompt_digest_metadata_profile",
			args:  func(_ *testEnv) []string { return []string{"export", "--export-profile", "metadata"} },
			setup: hookShortPrompt,
			assert: func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
				assertNoPlainDigest(shortPrompt, false)(t, env, stdout, stderr, err)
				assert.Contains(t, stdout, `"digest":"hmac-sha256:`)
			},
		},
		{
			name:  "default_since",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedEventsOlderThan1h(5),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				assert.Empty(t, strings.TrimSpace(stdout))
				assert.Contains(t, stderr, "No events")
			},
		},
		{
			name:  "schema_field",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedNRecentEvents(3),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 3)
				for _, line := range lines {
					var raw map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(line), &raw))
					schemaVal, ok := raw["$schema"]
					require.True(t, ok, "missing $schema field")
					var schemaURL string
					require.NoError(t, json.Unmarshal(schemaVal, &schemaURL))
					assert.Equal(t, events.EventSchemaURL, schemaURL)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			args := tt.args(env)
			stdout, stderr, err := env.run(args...)
			tt.assert(t, env, stdout, stderr, err)
		})
	}
}

const (
	sensitiveWriteContent = "TOKEN=hunter2"
	shortPrompt           = "yes"
)

func hookSensitiveWrite(env *testEnv) {
	payload, err := json.Marshal(map[string]any{
		"session_id":      "s-export-hash",
		"cwd":             env.tmpDir,
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": filepath.Join(env.tmpDir, ".env"), "content": sensitiveWriteContent},
		"tool_response":   map[string]any{"success": true},
		"tool_use_id":     "tu-env",
	})
	require.NoError(env.t, err)
	_, _, err = env.runHook("claude-code", "PostToolUse", payload)
	require.NoError(env.t, err)
}

func hookShortPrompt(env *testEnv) {
	payload, err := json.Marshal(map[string]any{
		"session_id":      "s-export-prompt",
		"cwd":             env.tmpDir,
		"hook_event_name": "UserPromptSubmit",
		"prompt":          shortPrompt,
	})
	require.NoError(env.t, err)
	_, _, err = env.runHook("claude-code", "UserPromptSubmit", payload)
	require.NoError(env.t, err)
}

// assertNoPlainDigest checks that the export holds no plain sha256 of the
// content, in any field, and holds the event.
func assertNoPlainDigest(content string, sensitive bool) func(*testing.T, *testEnv, string, string, error) {
	return func(t *testing.T, env *testEnv, stdout, stderr string, err error) {
		require.NoError(t, err)
		assert.Contains(t, stderr, "Exported 1 events")
		sum := sha256.Sum256([]byte(content))
		assert.NotContains(t, stdout, hex.EncodeToString(sum[:]))
		assert.NotContains(t, stdout, content)

		store, cleanup := env.openStore()
		defer cleanup()
		evts, err := store.QueryEvents(context.Background(), events.NewEventFilter())
		require.NoError(t, err)
		require.Len(t, evts, 1)
		assert.Equal(t, sensitive, evts[0].IsSensitive)
		assert.Contains(t, string(evts[0].Payload), hex.EncodeToString(sum[:]), "the local store keeps the plain digest")
	}
}

package cli_test

import (
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
			name:  "sensitive_excluded",
			args:  func(_ *testEnv) []string { return []string{"export"} },
			setup: seedSensitiveEvents(3, 2),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 3)
				for _, line := range lines {
					var evt events.Event
					require.NoError(t, json.Unmarshal([]byte(line), &evt))
					assert.False(t, evt.IsSensitive)
				}
			},
		},
		{
			name:  "sensitive_included",
			args:  func(_ *testEnv) []string { return []string{"export", "--sensitive"} },
			setup: seedSensitiveEvents(3, 2),
			assert: func(t *testing.T, _ *testEnv, stdout, stderr string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 5)
				assert.Contains(t, stderr, "Exported 5 events")
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

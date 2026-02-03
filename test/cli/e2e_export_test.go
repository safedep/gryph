package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport(t *testing.T) {
	tests := []struct {
		name   string
		args   func(env *testEnv) []string
		setup  func(env *testEnv)
		assert func(t *testing.T, env *testEnv, stdout string, err error)
	}{
		{
			name: "json_format",
			args: func(_ *testEnv) []string { return []string{"export", "--format", "json"} },
			setup: seedNRecentEvents(10),
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.Len(t, evts, 10)
			},
		},
		{
			name: "jsonl_format",
			args: func(_ *testEnv) []string { return []string{"export", "--format", "jsonl"} },
			setup: seedNRecentEvents(10),
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				assert.Len(t, lines, 10)
				for _, line := range lines {
					var obj json.RawMessage
					assert.NoError(t, json.Unmarshal([]byte(line), &obj))
				}
			},
		},
		{
			name: "csv_format",
			args: func(_ *testEnv) []string { return []string{"export", "--format", "csv"} },
			setup: seedNRecentEvents(10),
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				assertValidCSV(10)(t, stdout)
			},
		},
		{
			name: "all_events_no_limit",
			args: func(_ *testEnv) []string { return []string{"export", "--format", "json"} },
			setup: seedNRecentEvents(50),
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.Len(t, evts, 50)
			},
		},
		{
			name: "agent_filter",
			args: func(_ *testEnv) []string { return []string{"export", "--agent", "claude-code", "--format", "json"} },
			setup: seedMixedAgentEvents,
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
				assert.NotEmpty(t, evts)
				for _, e := range evts {
					assert.Equal(t, "claude-code", e.AgentName)
				}
			},
		},
		{
			name: "to_file",
			args: func(env *testEnv) []string {
				outPath := filepath.Join(env.tmpDir, "export.json")
				return []string{"export", "--format", "json", "-o", outPath}
			},
			setup: seedNRecentEvents(5),
			assert: func(t *testing.T, env *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				outPath := filepath.Join(env.tmpDir, "export.json")
				data, readErr := os.ReadFile(outPath)
				require.NoError(t, readErr)
				var evts []tui.EventView
				require.NoError(t, json.Unmarshal(data, &evts))
				assert.Len(t, evts, 5)
			},
		},
		{
			name: "empty_db",
			args: func(_ *testEnv) []string { return []string{"export"} },
			assert: func(t *testing.T, _ *testEnv, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "No events")
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
			stdout, _, err := env.run(args...)
			tt.assert(t, env, stdout, err)
		})
	}
}

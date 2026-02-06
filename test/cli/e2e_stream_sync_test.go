package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStreamTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := fmt.Sprintf(`logging:
  level: full
  content_hash: true
storage:
  path: %s
  retention_days: 90
display:
  colors: never
streams:
  targets:
    - name: test-nop
      type: nop
      enabled: true
`, dbPath)
	err := os.WriteFile(configPath, []byte(cfg), 0o600)
	require.NoError(t, err)
	return &testEnv{t: t, tmpDir: tmpDir, dbPath: dbPath, configPath: configPath}
}

func newNoTargetsTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := fmt.Sprintf(`logging:
  level: full
  content_hash: true
storage:
  path: %s
  retention_days: 90
display:
  colors: never
streams:
  targets: []
`, dbPath)
	err := os.WriteFile(configPath, []byte(cfg), 0o600)
	require.NoError(t, err)
	return &testEnv{t: t, tmpDir: tmpDir, dbPath: dbPath, configPath: configPath}
}

func parseStreamSyncJSON(t *testing.T, stdout string) tui.StreamSyncView {
	t.Helper()
	var view tui.StreamSyncView
	require.NoError(t, json.Unmarshal([]byte(stdout), &view))
	return view
}

func TestStreamSync(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		newEnv func(t *testing.T) *testEnv
		setup  func(env *testEnv)
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name:   "no_targets_configured",
			args:   []string{"stream", "sync"},
			newEnv: newNoTargetsTestEnv,
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "No enabled stream targets")
			},
		},
		{
			name:   "empty_db",
			args:   []string{"stream", "sync", "--format", "json"},
			newEnv: newStreamTestEnv,
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				view := parseStreamSyncJSON(t, stdout)
				assert.Equal(t, 0, view.TotalEvents)
				assert.Equal(t, 0, view.TotalAudits)
			},
		},
		{
			name:   "syncs_all_events",
			args:   []string{"stream", "sync", "--format", "json"},
			newEnv: newStreamTestEnv,
			setup:  seedNRecentEvents(10),
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				view := parseStreamSyncJSON(t, stdout)
				assert.Equal(t, 10, view.TotalEvents)
				assert.Len(t, view.TargetResults, 1)
				assert.Equal(t, "test-nop", view.TargetResults[0].TargetName)
			},
		},
		{
			name:   "drain_with_small_batch_size",
			args:   []string{"stream", "sync", "--format", "json", "--batch-size", "3"},
			newEnv: newStreamTestEnv,
			setup:  seedNRecentEvents(10),
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				view := parseStreamSyncJSON(t, stdout)
				assert.Equal(t, 10, view.TotalEvents)
			},
		},
		{
			name:   "iterations_limits_batches",
			args:   []string{"stream", "sync", "--format", "json", "--batch-size", "3", "--iterations", "2"},
			newEnv: newStreamTestEnv,
			setup:  seedNRecentEvents(10),
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				view := parseStreamSyncJSON(t, stdout)
				assert.Equal(t, 6, view.TotalEvents)
			},
		},
		{
			name:   "quiet_suppresses_output",
			args:   []string{"stream", "sync", "--quiet"},
			newEnv: newStreamTestEnv,
			setup:  seedNRecentEvents(5),
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Empty(t, stdout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.newEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			stdout, _, err := env.run(tt.args...)
			tt.assert(t, stdout, err)
		})
	}
}

func TestStreamSyncDrainLoop(t *testing.T) {
	env := newStreamTestEnv(t)
	seedNRecentEvents(10)(env)

	// First sync: batch-size=4, iterations=1 → exactly 4 events
	stdout, _, err := env.run("stream", "sync", "--format", "json", "--batch-size", "4", "--iterations", "1")
	require.NoError(t, err)
	view := parseStreamSyncJSON(t, stdout)
	assert.Equal(t, 4, view.TotalEvents)

	// Second sync: drain the rest (6 remaining)
	stdout, _, err = env.run("stream", "sync", "--format", "json")
	require.NoError(t, err)
	view = parseStreamSyncJSON(t, stdout)
	assert.Equal(t, 6, view.TotalEvents)

	// Third sync: nothing left
	stdout, _, err = env.run("stream", "sync", "--format", "json")
	require.NoError(t, err)
	view = parseStreamSyncJSON(t, stdout)
	assert.Equal(t, 0, view.TotalEvents)
}

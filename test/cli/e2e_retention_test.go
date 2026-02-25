package cli_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetention(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		setup  func(env *testEnv)
		assert func(t *testing.T, env *testEnv, stdout string, err error)
	}{
		{
			name:  "cleanup_dry_run_no_error",
			args:  []string{"retention", "cleanup", "--dry-run"},
			setup: seedOldEvents,
			assert: func(t *testing.T, _ *testEnv, _ string, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "cleanup_deletes_old",
			args:  []string{"retention", "cleanup"},
			setup: seedOldAndRecentEvents,
			assert: func(t *testing.T, env *testEnv, _ string, err error) {
				assert.NoError(t, err)

				// Verify old events are gone, recent remain
				store, cleanup := env.openStore()
				defer cleanup()
				evts, qErr := store.QueryEvents(context.Background(), events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 3, "only recent events should remain")
			},
		},
		{
			name: "status_no_error",
			args: []string{"retention", "status"},
			assert: func(t *testing.T, _ *testEnv, _ string, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "cleanup_nothing_to_delete",
			args:  []string{"retention", "cleanup"},
			setup: seedNRecentEvents(3),
			assert: func(t *testing.T, env *testEnv, _ string, err error) {
				assert.NoError(t, err)
				// Verify all events still present
				store, cleanup := env.openStore()
				defer cleanup()
				evts, qErr := store.QueryEvents(context.Background(), events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 3)
			},
		},
		{
			name:  "dry_run_preserves_data",
			args:  []string{"retention", "cleanup", "--dry-run"},
			setup: seedOldEvents,
			assert: func(t *testing.T, env *testEnv, _ string, err error) {
				assert.NoError(t, err)
				// Verify all events still present after dry run
				store, cleanup := env.openStore()
				defer cleanup()
				evts, qErr := store.QueryEvents(context.Background(), events.NewEventFilter())
				require.NoError(t, qErr)
				assert.Len(t, evts, 5, "dry run should not delete anything")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			stdout, _, err := env.run(tt.args...)
			tt.assert(t, env, stdout, err)
		})
	}
}

func TestRetention_DisabledPolicy(t *testing.T) {
	env := newTestEnv(t)
	configYAML := fmt.Sprintf(`logging:
  level: full
storage:
  path: %s
  retention_days: 0
display:
  colors: never
`, env.dbPath)
	require.NoError(t, os.WriteFile(env.configPath, []byte(configYAML), 0600))

	seedOldEvents(env)

	_, _, err := env.run("retention", "cleanup")
	// Should succeed without error (policy disabled, nothing to do)
	assert.NoError(t, err)

	// Old events should still be present
	store, cleanup := env.openStore()
	defer cleanup()
	evts, qErr := store.QueryEvents(context.Background(), events.NewEventFilter())
	require.NoError(t, qErr)
	assert.Len(t, evts, 5, "disabled policy should not delete events")
}

package cli_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/cli"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEnv struct {
	t          *testing.T
	tmpDir     string
	dbPath     string
	configPath string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithConfig(t, "")
}

func newTestEnvWithConfig(t *testing.T, configYAML string) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join(tmpDir, "config.yaml")

	if configYAML == "" {
		configYAML = fmt.Sprintf(`logging:
  level: full
  content_hash: true
storage:
  path: %s
  retention_days: 90
display:
  colors: never
`, dbPath)
	}

	err := os.WriteFile(configPath, []byte(configYAML), 0o600)
	require.NoError(t, err)

	return &testEnv{
		t:          t,
		tmpDir:     tmpDir,
		dbPath:     dbPath,
		configPath: configPath,
	}
}

func (env *testEnv) run(args ...string) (stdout, stderr string, err error) {
	env.t.Helper()

	var outBuf, errBuf bytes.Buffer
	rootCmd := cli.NewRootCmd()
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&errBuf)

	fullArgs := append([]string{"--config", env.configPath, "--no-color"}, args...)
	rootCmd.SetArgs(fullArgs)
	err = rootCmd.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

func (env *testEnv) runHook(agentName, hookType string, payload []byte) (stdout, stderr string, err error) {
	env.t.Helper()

	oldStdin := os.Stdin
	r, w, pipeErr := os.Pipe()
	require.NoError(env.t, pipeErr)

	os.Stdin = r
	go func() {
		_, _ = w.Write(payload)
		_ = w.Close()
	}()
	defer func() { os.Stdin = oldStdin }()

	return env.run("_hook", agentName, hookType)
}

func (env *testEnv) openStore() (storage.Store, func()) {
	env.t.Helper()

	store, err := storage.NewSQLiteStore(env.dbPath)
	require.NoError(env.t, err)
	err = store.Init(context.Background())
	require.NoError(env.t, err)

	return store, func() {
		err := store.Close()
		require.NoError(env.t, err)
	}
}

func (env *testEnv) seedStore(fn func(ctx context.Context, store storage.Store)) {
	env.t.Helper()

	store, cleanup := env.openStore()
	defer cleanup()

	fn(context.Background(), store)
}

// seedNRecentEvents returns a seed function that creates n events with recent timestamps.
func seedNRecentEvents(n int) func(env *testEnv) {
	return func(env *testEnv) {
		env.seedStore(func(ctx context.Context, store storage.Store) {
			sessID := uuid.New()
			sess := session.NewSessionWithID(sessID, "claude-code")
			sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
			sess.WorkingDirectory = "/tmp/project"
			require.NoError(env.t, store.SaveSession(ctx, sess))

			for i := 0; i < n; i++ {
				evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
				evt.Sequence = i + 1
				evt.Timestamp = time.Now().UTC().Add(-time.Duration(n-i) * time.Minute)
				evt.ResultStatus = events.ResultSuccess
				evt.ToolName = "Read"
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
				require.NoError(env.t, store.SaveEvent(ctx, evt))
			}

			sess.TotalActions = n
			sess.FilesRead = n
			require.NoError(env.t, store.UpdateSession(ctx, sess))
		})
	}
}

// seedMixedAgentEvents seeds events from both claude-code and cursor.
func seedMixedAgentEvents(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		for _, agent := range []string{"claude-code", "cursor"} {
			sessID := uuid.New()
			sess := session.NewSessionWithID(sessID, agent)
			sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
			sess.WorkingDirectory = "/tmp/project"
			require.NoError(env.t, store.SaveSession(ctx, sess))

			for i := 0; i < 3; i++ {
				evt := events.NewEvent(sessID, agent, events.ActionFileRead)
				evt.Sequence = i + 1
				evt.Timestamp = time.Now().UTC().Add(-time.Duration(6-i) * time.Minute)
				evt.ResultStatus = events.ResultSuccess
				evt.ToolName = "Read"
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
				require.NoError(env.t, store.SaveEvent(ctx, evt))
			}

			sess.TotalActions = 3
			require.NoError(env.t, store.UpdateSession(ctx, sess))
		}
	})
}

// seedTodayAndYesterdayEvents seeds events split between today and yesterday.
func seedTodayAndYesterdayEvents(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-48 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()

		// Yesterday events
		for i := 0; i < 3; i++ {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 1
			evt.Timestamp = midnight.Add(-time.Duration(3-i) * time.Hour)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Read"
			payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/old%d.go", i)}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		// Today events
		for i := 0; i < 2; i++ {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 4
			evt.Timestamp = midnight.Add(time.Duration(i+1) * time.Hour)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Read"
			payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/new%d.go", i)}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = 5
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedWithPaths seeds events with known file paths.
func seedWithPaths(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		paths := []string{
			"/tmp/project/main.go",
			"/tmp/project/utils.go",
			"/tmp/project/readme.md",
			"/tmp/project/config.yaml",
		}
		for i, p := range paths {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(len(paths)-i) * time.Minute)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Read"
			payload := &events.FileReadPayload{Path: p}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = len(paths)
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedWithCommands seeds command_exec events with known commands.
func seedWithCommands(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		commands := []string{
			"npm install",
			"npm test",
			"go build ./...",
			"git status",
		}
		for i, cmd := range commands {
			evt := events.NewEvent(sessID, "claude-code", events.ActionCommandExec)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(len(commands)-i) * time.Minute)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Bash"
			payload := &events.CommandExecPayload{Command: cmd}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = len(commands)
		sess.CommandsExecuted = len(commands)
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedWithErrors seeds events with error statuses.
func seedWithErrors(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		statuses := []events.ResultStatus{
			events.ResultSuccess,
			events.ResultError,
			events.ResultSuccess,
			events.ResultError,
		}
		for i, st := range statuses {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(len(statuses)-i) * time.Minute)
			evt.ResultStatus = st
			evt.ToolName = "Read"
			if st == events.ResultError {
				evt.ErrorMessage = "file not found"
			}
			payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = len(statuses)
		sess.Errors = 2
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedOldEvents seeds events older than the default retention cutoff (90 days).
func seedOldEvents(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-120 * 24 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		for i := 0; i < 5; i++ {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(100+i) * 24 * time.Hour)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Read"
			payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/old%d.go", i)}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = 5
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedOldAndRecentEvents seeds both old and recent events.
func seedOldAndRecentEvents(env *testEnv) {
	seedOldEvents(env)

	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		for i := 0; i < 3; i++ {
			evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(3-i) * time.Minute)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = "Read"
			payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/recent%d.go", i)}
			require.NoError(env.t, evt.SetPayload(payload))
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = 3
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seedMixedActions seeds events with different action types.
func seedMixedActions(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		actions := []struct {
			actionType events.ActionType
			toolName   string
		}{
			{events.ActionFileRead, "Read"},
			{events.ActionFileWrite, "Write"},
			{events.ActionCommandExec, "Bash"},
			{events.ActionFileRead, "Read"},
			{events.ActionFileWrite, "Write"},
		}

		for i, a := range actions {
			evt := events.NewEvent(sessID, "claude-code", a.actionType)
			evt.Sequence = i + 1
			evt.Timestamp = time.Now().UTC().Add(-time.Duration(len(actions)-i) * time.Minute)
			evt.ResultStatus = events.ResultSuccess
			evt.ToolName = a.toolName

			switch a.actionType {
			case events.ActionFileRead:
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
			case events.ActionFileWrite:
				payload := &events.FileWritePayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i), LinesAdded: 10, LinesRemoved: 2}
				require.NoError(env.t, evt.SetPayload(payload))
			case events.ActionCommandExec:
				payload := &events.CommandExecPayload{Command: "go build"}
				require.NoError(env.t, evt.SetPayload(payload))
			}
			require.NoError(env.t, store.SaveEvent(ctx, evt))
		}

		sess.TotalActions = len(actions)
		sess.FilesRead = 2
		sess.FilesWritten = 2
		sess.CommandsExecuted = 1
		require.NoError(env.t, store.UpdateSession(ctx, sess))
	})
}

// seed3Sessions seeds 3 sessions from different agents.
func seed3Sessions(env *testEnv) {
	env.seedStore(func(ctx context.Context, store storage.Store) {
		agents := []string{"claude-code", "cursor", "claude-code"}
		for idx, agentName := range agents {
			sessID := uuid.New()
			sess := session.NewSessionWithID(sessID, agentName)
			sess.StartedAt = time.Now().UTC().Add(-time.Duration(3-idx) * time.Hour)
			sess.WorkingDirectory = "/tmp/project"
			sess.TotalActions = idx + 1
			require.NoError(env.t, store.SaveSession(ctx, sess))

			for i := 0; i < idx+1; i++ {
				evt := events.NewEvent(sessID, agentName, events.ActionFileRead)
				evt.Sequence = i + 1
				evt.Timestamp = sess.StartedAt.Add(time.Duration(i) * time.Minute)
				evt.ResultStatus = events.ResultSuccess
				evt.ToolName = "Read"
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
				require.NoError(env.t, store.SaveEvent(ctx, evt))
			}
		}
	})
}

// seedSensitiveEvents seeds n normal events and n sensitive events within the last hour.
func seedSensitiveEvents(normal, sensitive int) func(env *testEnv) {
	return func(env *testEnv) {
		env.seedStore(func(ctx context.Context, store storage.Store) {
			sessID := uuid.New()
			sess := session.NewSessionWithID(sessID, "claude-code")
			sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
			sess.WorkingDirectory = "/tmp/project"
			require.NoError(env.t, store.SaveSession(ctx, sess))

			total := normal + sensitive
			for i := 0; i < total; i++ {
				evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
				evt.Sequence = i + 1
				evt.Timestamp = time.Now().UTC().Add(-time.Duration(total-i) * time.Minute)
				evt.ResultStatus = events.ResultSuccess
				evt.ToolName = "Read"
				if i >= normal {
					evt.IsSensitive = true
				}
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/file%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
				require.NoError(env.t, store.SaveEvent(ctx, evt))
			}

			sess.TotalActions = total
			require.NoError(env.t, store.UpdateSession(ctx, sess))
		})
	}
}

// seedEventsOlderThan1h seeds events that are older than 1 hour (outside default --since window).
func seedEventsOlderThan1h(n int) func(env *testEnv) {
	return func(env *testEnv) {
		env.seedStore(func(ctx context.Context, store storage.Store) {
			sessID := uuid.New()
			sess := session.NewSessionWithID(sessID, "claude-code")
			sess.StartedAt = time.Now().UTC().Add(-3 * time.Hour)
			sess.WorkingDirectory = "/tmp/project"
			require.NoError(env.t, store.SaveSession(ctx, sess))

			for i := 0; i < n; i++ {
				evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
				evt.Sequence = i + 1
				evt.Timestamp = time.Now().UTC().Add(-2*time.Hour - time.Duration(n-i)*time.Minute)
				evt.ResultStatus = events.ResultSuccess
				evt.ToolName = "Read"
				payload := &events.FileReadPayload{Path: fmt.Sprintf("/tmp/project/old%d.go", i)}
				require.NoError(env.t, evt.SetPayload(payload))
				require.NoError(env.t, store.SaveEvent(ctx, evt))
			}

			sess.TotalActions = n
			require.NoError(env.t, store.UpdateSession(ctx, sess))
		})
	}
}

// --- Assertion helpers ---

func assertEventCount(n int) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var evts []tui.EventView
		require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
		assert.Len(t, evts, n)
	}
}

func assertAllEventsFromAgent(agent string) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var evts []tui.EventView
		require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
		assert.NotEmpty(t, evts)
		for _, e := range evts {
			assert.Equal(t, agent, e.AgentName)
		}
	}
}

func assertAllActionsAre(action string) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var evts []tui.EventView
		require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
		assert.NotEmpty(t, evts)
		for _, e := range evts {
			assert.Equal(t, action, e.ActionType)
		}
	}
}

func assertActionsIn(actions ...string) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var evts []tui.EventView
		require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
		assert.NotEmpty(t, evts)
		allowed := make(map[string]bool)
		for _, a := range actions {
			allowed[a] = true
		}
		for _, e := range evts {
			assert.True(t, allowed[e.ActionType], "unexpected action: %s", e.ActionType)
		}
	}
}

func assertOutputContains(substr string) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		assert.Contains(t, stdout, substr)
	}
}

func assertValidJSONArray(t *testing.T, stdout string) {
	t.Helper()
	var arr []json.RawMessage
	assert.NoError(t, json.Unmarshal([]byte(stdout), &arr))
}

func assertValidJSONL(t *testing.T, stdout string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	assert.NotEmpty(t, lines)
	for _, line := range lines {
		var obj json.RawMessage
		assert.NoError(t, json.Unmarshal([]byte(line), &obj), "invalid JSONL line: %s", line)
	}
}

func assertValidCSV(expectedRows int) func(*testing.T, string) {
	return func(t *testing.T, stdout string) {
		t.Helper()
		r := csv.NewReader(strings.NewReader(stdout))
		records, err := r.ReadAll()
		assert.NoError(t, err)
		// +1 for header row
		assert.Len(t, records, expectedRows+1)
	}
}

func assertSessionCount(n int) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var sessions []tui.SessionView
		require.NoError(t, json.Unmarshal([]byte(stdout), &sessions))
		assert.Len(t, sessions, n)
	}
}

func assertAllSessionsFromAgent(agent string) func(*testing.T, string, error) {
	return func(t *testing.T, stdout string, err error) {
		t.Helper()
		assert.NoError(t, err)
		var sessions []tui.SessionView
		require.NoError(t, json.Unmarshal([]byte(stdout), &sessions))
		assert.NotEmpty(t, sessions)
		for _, s := range sessions {
			assert.Equal(t, agent, s.AgentName)
		}
	}
}

func assertAllEventsFromToday(t *testing.T, stdout string, err error) {
	t.Helper()
	assert.NoError(t, err)
	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	assert.NotEmpty(t, evts)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, e := range evts {
		assert.True(t, e.Timestamp.After(todayStart) || e.Timestamp.Equal(todayStart),
			"event timestamp %v is before today %v", e.Timestamp, todayStart)
	}
}

func assertAllEventsFromYesterday(t *testing.T, stdout string, err error) {
	t.Helper()
	assert.NoError(t, err)
	var evts []tui.EventView
	require.NoError(t, json.Unmarshal([]byte(stdout), &evts))
	assert.NotEmpty(t, evts)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.Add(-24 * time.Hour)
	for _, e := range evts {
		ts := e.Timestamp.In(now.Location())
		assert.True(t, (ts.After(yesterdayStart) || ts.Equal(yesterdayStart)) && ts.Before(todayStart),
			"event timestamp %v is not from yesterday [%v, %v)", ts, yesterdayStart, todayStart)
	}
}

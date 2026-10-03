package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/platform/procs"
	"github.com/safedep/gryph/selfprotect"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// censusAdapter is an adapter whose program is the test binary, so the
// census finds a live process of it.
type censusAdapter struct {
	agent.Adapter
	name  string
	names []string
}

func (a censusAdapter) Name() string           { return a.name }
func (a censusAdapter) ProcessNames() []string { return a.names }

func censusRuntime(t *testing.T, window time.Duration, adapters ...agent.Adapter) *Runtime {
	t.Helper()
	cfg := config.Default()
	cfg.Policy.SelfProtection.CensusWindow = window
	rt, err := New(cfg)
	require.NoError(t, err)
	rt.Store = storagetest.NewStore(t)
	rt.Registry = agent.NewRegistry()
	for _, a := range adapters {
		rt.Registry.Register(a)
	}
	return rt
}

func TestCensus(t *testing.T) {
	ctx := context.Background()
	exe, err := os.Executable()
	require.NoError(t, err)
	me := filepath.Base(exe)
	live := censusAdapter{Adapter: claudecode.New(nil, config.LoggingStandard, false), name: "test-agent", names: []string{me}}
	absent := censusAdapter{Adapter: claudecode.New(nil, config.LoggingStandard, false), name: "absent-agent", names: []string{"no-such-program-for-gryph"}}

	t.Run("no process, no row", func(t *testing.T) {
		rt := censusRuntime(t, time.Minute, absent)
		statuses, err := rt.Census(ctx)
		require.NoError(t, err)
		assert.Empty(t, statuses)
	})

	t.Run("process inside the window has no drift", func(t *testing.T) {
		rt := censusRuntime(t, 24*time.Hour, live, absent)
		statuses, err := rt.Census(ctx)
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		s := statuses[0]
		assert.Equal(t, selfprotect.AssetHookTraffic, s.Asset)
		assert.Equal(t, "test-agent", s.Agent)
		assert.Equal(t, selfprotect.LevelDetect, s.Level)
		assert.Equal(t, CensusProviderName, s.Provider)
		assert.Empty(t, s.Drift)
		assert.Contains(t, s.Detail, "started inside the census window")
	})

	t.Run("old process without traffic is silent", func(t *testing.T) {
		rt := censusRuntime(t, time.Nanosecond, live)
		statuses, err := rt.Census(ctx)
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		assert.Contains(t, statuses[0].Drift, "silent agent")
		assert.Contains(t, statuses[0].Detail, "1 process(es), pid")

		recorded, err := rt.TamperRecorderForTest().RecordChanges(ctx, statuses)
		require.NoError(t, err)
		require.Len(t, recorded, 1)
		var p events.TamperPayload
		require.NoError(t, decodePayload(recorded[0], &p))
		assert.Equal(t, events.TamperSilentAgent, p.Operation)
	})

	t.Run("recent traffic clears the drift", func(t *testing.T) {
		rt := censusRuntime(t, time.Minute, live)
		sess := session.NewSessionWithID(uuid.New(), "test-agent")
		require.NoError(t, rt.Store.SaveSession(ctx, sess))
		event := events.NewEvent(sess.ID, "test-agent", events.ActionFileRead)
		require.NoError(t, rt.Store.RecordEvent(ctx, event, session.EventCounts(event)))

		statuses, err := rt.Census(ctx)
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		assert.Empty(t, statuses[0].Drift)
		assert.Contains(t, statuses[0].Detail, "last hook event")
	})
}

func TestAnyOlderThan(t *testing.T) {
	now := time.Now()
	assert.True(t, anyOlderThan([]procs.Process{{Started: now.Add(-time.Hour)}}, now.Add(-time.Minute)))
	assert.False(t, anyOlderThan([]procs.Process{{Started: now}}, now.Add(-time.Minute)))
	assert.True(t, anyOlderThan([]procs.Process{{}}, now), "an unknown start counts as old")
	assert.False(t, anyOlderThan(nil, now))
}

func TestTimeoutNote(t *testing.T) {
	assert.Equal(t, "hook timeout not documented by the vendor", timeoutNote(nil))
	assert.Equal(t, "hook timeout 600 s", timeoutNote([]events.HookSpec{{Timeout: 30 * time.Second}, {Timeout: 600 * time.Second}}))
}

func TestPosture_AgentRows(t *testing.T) {
	rt := censusRuntime(t, time.Minute)
	items := rt.Posture(context.Background())
	for _, item := range items {
		assert.NotContains(t, item.Name, "claude-code", "no agent is present in the test home")
	}
	rows := agentPosture(context.Background(), claudecode.New(nil, config.LoggingStandard, false))
	require.Len(t, rows, 1)
	assert.Equal(t, "claude-code on a hook error or timeout", rows[0].Name)
	assert.Equal(t, "lets the action through", rows[0].Value)
	assert.Equal(t, PostureInfo, rows[0].Status)
	assert.Equal(t, "hook timeout 600 s", rows[0].Note)
}

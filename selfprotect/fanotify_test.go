package selfprotect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeState(t *testing.T, dir string, state FanotifyState) string {
	t.Helper()
	data, err := json.Marshal(state)
	require.NoError(t, err)
	path := filepath.Join(dir, "fanotify.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestFanotifyProvider_Assess(t *testing.T) {
	dir := t.TempDir()
	assets := FanotifyAssets{
		Binary:      "/opt/safedep/gryph/bin/gryph",
		ConfigFile:  "/etc/safedep/gryph/config.yml",
		PolicyFile:  "/etc/safedep/gryph/policy.yaml",
		PoliciesDir: "/etc/safedep/gryph/policies",
		HookConfigs: map[string]string{"claude-code": "/etc/claude-code/managed-settings.d/50-gryph.json"},
	}
	state := FanotifyState{
		PID:   4242,
		Files: []string{assets.Binary, assets.ConfigFile, assets.PolicyFile, assets.HookConfigs["claude-code"]},
		Dirs:  []string{"/etc/safedep/gryph", assets.PoliciesDir},
	}
	alive := func(pid int) bool { return pid == 4242 }

	t.Run("a live watcher reports prevent_same_user for every covered asset", func(t *testing.T) {
		p := NewFanotifyProvider(writeState(t, dir, state), alive, assets)
		rows := p.Assess(context.Background())
		require.Len(t, rows, 4)
		for _, r := range rows {
			assert.Equal(t, LevelPreventSameUser, r.Level)
			assert.Equal(t, FanotifyProviderName, r.Provider)
			assert.Empty(t, r.Drift)
		}
		assert.Equal(t, AssetHookConfig, rows[0].Asset)
		assert.Equal(t, "claude-code", rows[0].Agent)
	})

	t.Run("a dead watcher reports nothing", func(t *testing.T) {
		p := NewFanotifyProvider(writeState(t, dir, state), func(int) bool { return false }, assets)
		assert.Empty(t, p.Assess(context.Background()))
	})

	t.Run("no state file reports nothing", func(t *testing.T) {
		p := NewFanotifyProvider(filepath.Join(dir, "missing.json"), alive, assets)
		assert.Empty(t, p.Assess(context.Background()))
	})

	t.Run("an uncovered asset gets no row", func(t *testing.T) {
		partial := state
		partial.Files = []string{assets.ConfigFile}
		partial.Dirs = nil
		p := NewFanotifyProvider(writeState(t, dir, partial), alive, assets)
		rows := p.Assess(context.Background())
		require.Len(t, rows, 1)
		assert.Equal(t, AssetConfig, rows[0].Asset)
	})

	t.Run("a rename of a protected file is the drift of its row", func(t *testing.T) {
		moved := state
		moved.Changes = []FanotifyChange{
			{Time: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), Op: "modify", Path: assets.PolicyFile, UID: 0},
			{Time: time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC), Op: "moved", Path: assets.PolicyFile, PID: 7, UID: 0},
		}
		p := NewFanotifyProvider(writeState(t, dir, moved), alive, assets)
		for _, r := range p.Assess(context.Background()) {
			if r.Asset == AssetPolicy {
				assert.Equal(t, "/etc/safedep/gryph/policy.yaml moved by uid 0 at 2026-10-03T12:01:00Z", r.Drift)
			} else {
				assert.Empty(t, r.Drift)
			}
		}
	})
}

func TestStrongest(t *testing.T) {
	rows := []AssetStatus{
		{Asset: AssetHookConfig, Agent: "claude-code", Level: LevelDetect, Provider: "user", Drift: "hooks not installed"},
		{Asset: AssetPolicy, Level: LevelMediated, Provider: "user"},
		{Asset: AssetConfig, Level: LevelPreventSameUser, Provider: "user"},
		{Asset: AssetPolicy, Level: LevelPreventSameUser, Provider: "fanotify", Drift: "policy.yaml moved by uid 0 at t"},
		{Asset: AssetConfig, Level: LevelPreventSameUser, Provider: "fanotify"},
		{Asset: AssetHookConfig, Agent: "claude-code", Level: LevelPreventSameUser, Provider: "fanotify"},
	}
	out := Strongest(rows)
	require.Len(t, out, 3)
	assert.Equal(t, "fanotify", out[0].Provider)
	assert.Equal(t, "hooks not installed", out[0].Drift)
	assert.Equal(t, "fanotify", out[1].Provider)
	assert.Equal(t, "policy.yaml moved by uid 0 at t", out[1].Drift)
	assert.Equal(t, "user", out[2].Provider, "a tie keeps the first row")
	assert.Equal(t, ProfileLocked, ProfileOf(out))
}

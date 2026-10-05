package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstall_ClaudeCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-code", "managed-settings.d", "50-gryph.json")
	opts := agent.ManagedInstallOptions{Command: "/opt/safedep/gryph/bin/gryph"}

	res, err := installManagedAt(path, agent.ManagedInstallOptions{Command: opts.Command, DryRun: true})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "a dry run writes nothing")

	res, err = installManagedAt(path, opts)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.False(t, res.Locked)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	_, hasLock := doc["allowManagedHooksOnly"]
	assert.False(t, hasLock, "the lock is off by default")
	hooks := doc["hooks"].(map[string]any)
	assert.Len(t, hooks, len(HookTypes))
	assert.Contains(t, string(data), `/opt/safedep/gryph/bin/gryph _hook claude-code PreToolUse`)

	res, err = installManagedAt(path, opts)
	require.NoError(t, err)
	assert.False(t, res.Changed, "a repeated run changes nothing")

	res, err = installManagedAt(path, agent.ManagedInstallOptions{Command: opts.Command, Lock: true})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.True(t, res.Locked)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"allowManagedHooksOnly": true`)

	res, err = uninstallManagedAt(path, agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
	res, err = uninstallManagedAt(path, agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.False(t, res.Changed)

	_, err = installManagedAt(path, agent.ManagedInstallOptions{Command: "gryph"})
	assert.Error(t, err, "a relative command is refused")
}

func TestManagedHookPath_ClaudeCode(t *testing.T) {
	a := New(nil, config.LoggingFull, false)
	assert.Equal(t, filepath.Join(managedSettingsDir(), "managed-settings.d", "50-gryph.json"), a.ManagedHookPath())
}

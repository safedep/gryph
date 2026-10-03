package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstall_Codex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex", "requirements.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`model = "gpt-5"

[features]
web_search = false

[[hooks.PreToolUse]]
matcher = "^Bash$"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "/enterprise/hooks/check.py"
timeout = 10
`), 0o644))
	opts := agent.ManagedInstallOptions{Command: "/usr/libexec/safedep/gryph/gryph"}

	res, err := installManagedAt(path, opts)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.False(t, res.Locked)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, toml.Unmarshal(data, &doc))
	assert.Equal(t, "gpt-5", doc["model"], "other keys stay")
	features := doc["features"].(map[string]any)
	assert.Equal(t, false, features["web_search"])
	assert.Equal(t, true, features["hooks"], "hooks are pinned on")
	_, hasLock := doc[lockKey]
	assert.False(t, hasLock)
	pre := entryList(doc["hooks"].(map[string]any)["PreToolUse"])
	require.Len(t, pre, 2, "the enterprise entry stays, the Gryph entry follows")
	assert.Equal(t, "^Bash$", pre[0]["matcher"])
	assert.Equal(t, "*", pre[1]["matcher"])
	gryph := entryList(pre[1]["hooks"])[0]
	assert.Equal(t, "/usr/libexec/safedep/gryph/gryph _hook codex PreToolUse", gryph["command"])
	assert.Equal(t, int64(30), gryph["timeout"])
	assert.Len(t, entryList(doc["hooks"].(map[string]any)["Stop"]), 1)

	res, err = installManagedAt(path, opts)
	require.NoError(t, err)
	assert.False(t, res.Changed, "a repeated run changes nothing")

	res, err = installManagedAt(path, agent.ManagedInstallOptions{Command: opts.Command, Lock: true})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.True(t, res.Locked)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "allow_managed_hooks_only = true")

	res, err = installManagedAt(path, agent.ManagedInstallOptions{Command: "/opt/other/gryph"})
	require.NoError(t, err)
	assert.True(t, res.Changed, "a new binary path replaces the Gryph entries")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "/usr/libexec/safedep/gryph/gryph")
	assert.NotContains(t, string(data), "allow_managed_hooks_only", "lock off removes the key")
	assert.Contains(t, string(data), "/enterprise/hooks/check.py")

	res, err = uninstallManagedAt(path, agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "_hook codex")
	assert.Contains(t, string(data), "/enterprise/hooks/check.py", "the enterprise entry stays")
	assert.Contains(t, string(data), "hooks = true", "the feature stays pinned")
	after := map[string]any{}
	require.NoError(t, toml.Unmarshal(data, &after))
	_, hasStop := after["hooks"].(map[string]any)["Stop"]
	assert.False(t, hasStop, "an event with Gryph entries only goes away")
}

func TestManagedInstall_Codex_NewFileAndBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex", "requirements.toml")
	res, err := installManagedAt(path, agent.ManagedInstallOptions{Command: "/usr/libexec/safedep/gryph/gryph", DryRun: true})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)

	res, err = installManagedAt(path, agent.ManagedInstallOptions{Command: "/usr/libexec/safedep/gryph/gryph"})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[features]")
	assert.Contains(t, string(data), "[[hooks.PreToolUse]]")

	res, err = uninstallManagedAt(filepath.Join(t.TempDir(), "none.toml"), agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.False(t, res.Changed)

	require.NoError(t, os.WriteFile(path, []byte("= not toml"), 0o644))
	_, err = installManagedAt(path, agent.ManagedInstallOptions{Command: "/usr/libexec/safedep/gryph/gryph"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
}

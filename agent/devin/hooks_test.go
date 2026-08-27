package devin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDevinHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	devinDir := filepath.Join(home, ".config", "devin")
	require.NoError(t, os.MkdirAll(devinDir, 0700))
	return filepath.Join(devinDir, "config.json")
}

func TestInstallHooks_RefusesMalformedConfig(t *testing.T) {
	configFile := setupDevinHome(t)
	require.NoError(t, os.WriteFile(configFile, []byte("{not-json"), 0600))

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.Error(t, err)
	assert.False(t, result.Success)

	data, readErr := os.ReadFile(configFile)
	require.NoError(t, readErr)
	assert.Equal(t, "{not-json", string(data), "malformed config must stay untouched")
}

func TestInstallHooks_PreservesUnrelatedSettings(t *testing.T) {
	configFile := setupDevinHome(t)
	require.NoError(t, os.WriteFile(configFile, []byte(`{"agent": "sonnet", "theme_mode": "dark"}`), 0600))

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.NoError(t, err)
	require.True(t, result.Success)

	raw, err := readDevinConfig(configFile)
	require.NoError(t, err)
	assert.Contains(t, raw, "agent")
	assert.Contains(t, raw, "theme_mode")
	assert.Contains(t, raw, "hooks")
}

func TestInstallHooks_RepairsPartialInstall(t *testing.T) {
	configFile := setupDevinHome(t)

	partial := map[string]any{
		"agent": "sonnet",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": ".*",
					"hooks": []any{
						map[string]any{"type": "command", "command": gryphHookCommand("PreToolUse"), "timeout": 30},
					},
				},
			},
		},
	}
	data, err := json.Marshal(partial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configFile, data, 0600))

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.NoError(t, err)
	require.True(t, result.Success)

	status, err := GetHookStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.True(t, status.Valid, "repair install must add every missing hook type")
	assert.Len(t, status.Hooks, len(HookTypes))
}

func TestInstallThenUninstall_RoundTrip(t *testing.T) {
	configFile := setupDevinHome(t)
	require.NoError(t, os.WriteFile(configFile, []byte(`{"agent": "sonnet"}`), 0600))

	_, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.NoError(t, err)

	uninstall, err := UninstallHooks(context.Background(), agent.UninstallOptions{})
	require.NoError(t, err)
	require.True(t, uninstall.Success)

	raw, err := readDevinConfig(configFile)
	require.NoError(t, err)
	assert.Contains(t, raw, "agent")

	hooks, err := readHooksFromConfig(raw)
	require.NoError(t, err)
	for hookType, matchers := range hooks {
		assert.Empty(t, matchers, "hook type %s must carry no gryph entries", hookType)
	}
}

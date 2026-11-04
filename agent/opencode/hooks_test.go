package opencode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/internal/testpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessedPlugin_ReplacesPlaceholder(t *testing.T) {
	processed := processedPlugin("")

	assert.NotContains(t, string(processed), utils.GryphCommandPlaceholder)
	assert.Contains(t, string(processed), utils.GryphCommand())
}

// OpenCode 1.3.3 and earlier run the named export. OpenCode 1.3.4 and later
// read the default export: 1.x calls server and 2.x calls setup.
func TestProcessedPlugin_ServesEveryRuntime(t *testing.T) {
	processed := string(processedPlugin(""))

	assert.Contains(t, processed, "export const GryphPlugin = server;")
	assert.Contains(t, processed, `export default { id: "gryph", server, setup };`)
}

func legacyPlugin(t *testing.T) []byte {
	t.Helper()
	legacy, err := os.ReadFile(filepath.Join("testdata", "legacy", "gryph-v0.9.0.js"))
	require.NoError(t, err)
	return legacy
}

func pluginTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	testpath.SetHome(t, home)
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// No opencode binary on PATH: the plugin does not depend on its version.
	t.Setenv("PATH", t.TempDir())
	return filepath.Join(dir, pluginFileName)
}

func TestGetHookStatus_PluginContent(t *testing.T) {
	cases := []struct {
		name      string
		content   []byte
		wantValid bool
	}{
		{"current plugin", processedPlugin(""), true},
		{"v0.9.0 plugin, which OpenCode 2.x does not load", legacyPlugin(t), false},
		{"current plugin without the block throw", bytes.Replace(processedPlugin(""), []byte("throw new Error"), []byte("void new Error"), 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(pluginTestHome(t), tc.content, 0o644))

			status, err := GetHookStatus(context.Background())
			require.NoError(t, err)
			assert.True(t, status.Installed)
			assert.Equal(t, tc.wantValid, status.Valid)
		})
	}
}

func TestInstallHooks_WritesTheCurrentPlugin(t *testing.T) {
	cases := []struct {
		name        string
		existing    []byte
		wantWarning bool
	}{
		{"no plugin", nil, false},
		{"v0.9.0 plugin", legacyPlugin(t), false},
		{"current plugin", processedPlugin(""), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pluginFile := pluginTestHome(t)
			if tc.existing != nil {
				require.NoError(t, os.WriteFile(pluginFile, tc.existing, 0o644))
			}

			result, err := InstallHooks(context.Background(), agent.InstallOptions{})
			require.NoError(t, err)
			assert.True(t, result.Success)
			if tc.wantWarning {
				assert.Contains(t, result.Warnings, "gryph plugin already installed (use --force to overwrite)")
			} else {
				assert.Empty(t, result.Warnings)
			}

			content, err := os.ReadFile(pluginFile)
			require.NoError(t, err)
			assert.Equal(t, processedPlugin(""), content)
		})
	}
}

func TestInstallHooks_BacksUpTheReplacedPlugin(t *testing.T) {
	pluginFile := pluginTestHome(t)
	legacy := legacyPlugin(t)
	require.NoError(t, os.WriteFile(pluginFile, legacy, 0o644))

	result, err := InstallHooks(context.Background(), agent.InstallOptions{Backup: true, BackupDir: t.TempDir()})
	require.NoError(t, err)
	require.Contains(t, result.BackupPaths, pluginFileName)

	backup, err := os.ReadFile(result.BackupPaths[pluginFileName])
	require.NoError(t, err)
	assert.Equal(t, legacy, backup)
}

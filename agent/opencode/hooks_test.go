package opencode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func TestProcessedPluginV2_ReplacesPlaceholder(t *testing.T) {
	processed := processedPluginV2("")

	assert.NotContains(t, string(processed), utils.GryphCommandPlaceholder)
	assert.Contains(t, string(processed), utils.GryphCommand())
	assert.Contains(t, string(processed), "export default {")
	assert.Contains(t, string(processed), `"execute.before"`)
}

func TestPluginContent_SelectsByVersion(t *testing.T) {
	cases := []struct {
		version string
		wantV2  bool
		wantOK  bool
	}{
		{"2.1.2", true, true},
		{"2.0.0", true, true},
		{"v2.1.2", true, true},
		{"opencode v2.1.2", true, true},
		{"1.19.3", false, true},
		{"opencode v1.19.3", false, true},
		{"0.15.2", false, true},
		{"unknown", false, false},
		{"", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			content, ok := pluginContent("", tc.version)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantV2, isV2Plugin(content))
			}
		})
	}
}

func TestInstalledForVersion(t *testing.T) {
	assert.True(t, installedForVersion(processedPluginV2(""), "2.1.2"))
	assert.False(t, installedForVersion(processedPlugin(""), "2.1.2"))
	assert.True(t, installedForVersion(processedPlugin(""), "1.19.3"))
	assert.False(t, installedForVersion(processedPluginV2(""), "1.19.3"))
	assert.True(t, installedForVersion(processedPlugin(""), "0.15.2"))
	assert.False(t, installedForVersion(processedPluginV2(""), "0.15.2"))
	assert.True(t, installedForVersion(processedPlugin(""), "unknown"))
	assert.True(t, installedForVersion(processedPluginV2(""), "unknown"))
	assert.False(t, installedForVersion([]byte(`export default { id: "other" }`), "2.1.2"))
	assert.False(t, installedForVersion([]byte(`export default { id: "other" }`), "unknown"))
}

func TestInstallHooks_WritesPluginForDetectedMajor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}

	tests := []struct {
		name    string
		version string
		wantV2  bool
	}{
		{name: "2.x writes the V2 plugin", version: "opencode v2.1.2", wantV2: true},
		{name: "0.x keeps the V1 plugin", version: "opencode 0.15.2", wantV2: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			testpath.SetHome(t, home)
			require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755))

			bin := t.TempDir()
			script := filepath.Join(bin, "opencode")
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '"+tt.version+"'\n"), 0o755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			result, err := InstallHooks(context.Background(), agent.InstallOptions{})
			require.NoError(t, err)
			require.True(t, result.Success)

			pluginFile := filepath.Join(home, ".config", "opencode", "plugins", pluginFileName)
			content, err := os.ReadFile(pluginFile)
			require.NoError(t, err)
			assert.Equal(t, tt.wantV2, isV2Plugin(content))
			assert.Contains(t, string(content), "invokeGryph")
			if tt.wantV2 {
				assert.Contains(t, string(content), `"prompt"`)
			} else {
				assert.Contains(t, string(content), `"chat.message"`)
			}

			status, err := GetHookStatus(context.Background())
			require.NoError(t, err)
			assert.True(t, status.Installed)
			assert.True(t, status.Valid)
			assert.Contains(t, status.Hooks, "chat.message")
			assert.Contains(t, status.Hooks, "tool.execute.before")
			assert.Contains(t, status.Hooks, "session.error")
		})
	}
}

func TestInstallHooks_UnknownVersionErrors(t *testing.T) {
	home := t.TempDir()
	testpath.SetHome(t, home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755))
	t.Setenv("PATH", t.TempDir())

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.Error(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, err.Error(), "could not determine the OpenCode version")

	pluginFile := filepath.Join(home, ".config", "opencode", "plugins", pluginFileName)
	_, statErr := os.Stat(pluginFile)
	assert.True(t, os.IsNotExist(statErr))
}

func TestInstallHooks_UpgradesV1ToV2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}

	home := t.TempDir()
	testpath.SetHome(t, home)
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	pluginFile := filepath.Join(dir, pluginFileName)
	require.NoError(t, os.WriteFile(pluginFile, processedPlugin(""), 0o644))

	bin := t.TempDir()
	script := filepath.Join(bin, "opencode")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'opencode v2.1.2'\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.NoError(t, err)
	assert.True(t, result.Success)

	content, err := os.ReadFile(pluginFile)
	require.NoError(t, err)
	assert.True(t, isV2Plugin(content))
}

func TestInstallHooks_UnknownVersionKeepsExistingPlugin(t *testing.T) {
	home := t.TempDir()
	testpath.SetHome(t, home)
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	pluginFile := filepath.Join(dir, pluginFileName)
	existing := processedPluginV2("")
	require.NoError(t, os.WriteFile(pluginFile, existing, 0o644))
	t.Setenv("PATH", t.TempDir())

	result, err := InstallHooks(context.Background(), agent.InstallOptions{})
	require.NoError(t, err)
	assert.True(t, result.Success)

	content, err := os.ReadFile(pluginFile)
	require.NoError(t, err)
	assert.Equal(t, existing, content)
}

func TestGetHookStatus_UnknownVersionIsUnverified(t *testing.T) {
	home := t.TempDir()
	testpath.SetHome(t, home)
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, pluginFileName), processedPlugin(""), 0o644))
	t.Setenv("PATH", t.TempDir())

	status, err := GetHookStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.True(t, status.Valid)
	assert.True(t, status.CompatibilityUnverified)
	assert.Contains(t, status.Issues, "OpenCode version could not be detected; cannot verify plugin compatibility")
}

func TestGetHookStatus_PluginContent(t *testing.T) {
	// Keep the OpenCode version unknown so the V1 fixtures stay current.
	t.Setenv("PATH", t.TempDir())

	legacy, err := os.ReadFile(filepath.Join("testdata", "legacy", "gryph-v0.9.0.js"))
	require.NoError(t, err)
	cases := []struct {
		name        string
		content     []byte
		wantValid   bool
		wantMissing string
	}{
		{"current plugin", processedPlugin(""), true, ""},
		{"v0.9.0 plugin without the chat.message hook", legacy, true, "chat.message"},
		{"v0.9.0 plugin without the block throw", bytes.Replace(legacy, []byte("throw new Error"), []byte("void new Error"), 1), false, ""},
		{"current plugin without the block throw", bytes.Replace(processedPlugin(""), []byte("throw new Error"), []byte("void new Error"), 1), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			testpath.SetHome(t, home)
			dir := filepath.Join(home, ".config", "opencode", "plugins")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, pluginFileName), tc.content, 0o644))

			status, err := GetHookStatus(context.Background())
			require.NoError(t, err)
			assert.True(t, status.Installed)
			assert.Equal(t, tc.wantValid, status.Valid)
			if tc.wantMissing != "" {
				assert.NotContains(t, status.Hooks, tc.wantMissing)
			}
		})
	}
}

package piagent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginTS_HasAllHooks(t *testing.T) {
	pluginContent := string(pluginTS)
	for _, hookType := range HookTypes {
		assert.Contains(t, pluginContent, hookType,
			"extension should handle %s hook", hookType)
	}
}

func TestPluginTS_SpawnsGryph(t *testing.T) {
	pluginContent := string(pluginTS)
	assert.Contains(t, pluginContent, "__GRYPH_COMMAND__")
	assert.Contains(t, pluginContent, "_hook")
	assert.Contains(t, pluginContent, "pi-agent")
}

func TestPluginTS_UsesPiEvents(t *testing.T) {
	pluginContent := string(pluginTS)
	assert.Contains(t, pluginContent, "pi.on")
	assert.Contains(t, pluginContent, "session_start")
	assert.Contains(t, pluginContent, "session_shutdown")
	assert.Contains(t, pluginContent, "tool_call")
	assert.Contains(t, pluginContent, "tool_result")
}

func TestProcessedPlugin_ReplacesPlaceholder(t *testing.T) {
	processed := string(processedPlugin())
	assert.True(t, strings.Contains(processed, `"gryph"`),
		"processed plugin should have gryph command replaced")
	assert.True(t, strings.Contains(processed, `["_hook", "pi-agent"`),
		"processed plugin should pass correct args to gryph")
}

func TestGetHookStatus_PluginContent(t *testing.T) {
	legacy := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("testdata", "legacy", name))
		require.NoError(t, err)
		return data
	}
	cases := []struct {
		name        string
		content     []byte
		wantValid   bool
		wantMissing string
	}{
		{"current extension", processedPlugin(), true, ""},
		{"v0.9.0 extension without the input hook", legacy("gryph-hooks-v0.9.0.ts"), true, "input"},
		{"v0.3.6 extension that never blocks", legacy("gryph-hooks-v0.3.6.ts"), false, ""},
		{"current extension that ignores exit code 2", bytes.ReplaceAll(processedPlugin(), []byte("result.exitCode === 2"), []byte("false")), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := filepath.Join(home, ".pi", "agent", "extensions")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, hookFileName), tc.content, 0o644))

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

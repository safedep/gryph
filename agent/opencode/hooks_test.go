package opencode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessedPlugin_ReplacesPlaceholder(t *testing.T) {
	processed := processedPlugin()

	assert.NotContains(t, string(processed), utils.GryphCommandPlaceholder)
	assert.Contains(t, string(processed), utils.GryphCommand())
}

func TestGetHookStatus_PluginContent(t *testing.T) {
	legacy, err := os.ReadFile(filepath.Join("testdata", "legacy", "gryph-v0.9.0.js"))
	require.NoError(t, err)
	cases := []struct {
		name        string
		content     []byte
		wantValid   bool
		wantMissing string
	}{
		{"current plugin", processedPlugin(), true, ""},
		{"v0.9.0 plugin without the chat.message hook", legacy, true, "chat.message"},
		{"v0.9.0 plugin without the block throw", bytes.Replace(legacy, []byte("throw new Error"), []byte("void new Error"), 1), false, ""},
		{"current plugin without the block throw", bytes.Replace(processedPlugin(), []byte("throw new Error"), []byte("void new Error"), 1), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
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

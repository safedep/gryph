package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestDetect_NotInstalled(t *testing.T) {
	setHome(t, t.TempDir())

	result, err := Detect(context.Background())
	require.NoError(t, err)
	assert.False(t, result.Installed)
}

// TestDetect_WithoutProgramExecution shows that detection reads the version
// from settings.json when it may not run the claude binary.
func TestDetect_WithoutProgramExecution(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"version":"9.9.9"}`), 0o644))

	result, err := Detect(utils.WithoutProgramExecution(context.Background()))
	require.NoError(t, err)
	assert.True(t, result.Installed)
	assert.Equal(t, "9.9.9", result.Version)
	assert.Equal(t, claudeDir, result.ConfigPath)
}

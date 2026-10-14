package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/internal/testpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_NotInstalled(t *testing.T) {
	testpath.SetHome(t, t.TempDir())

	result, err := Detect(context.Background())
	require.NoError(t, err)
	assert.False(t, result.Installed)
}

// TestDetect_WithoutProgramExecution shows that detection reads the version
// from settings.json when it may not run the claude binary.
func TestDetect_WithoutProgramExecution(t *testing.T) {
	home := t.TempDir()
	testpath.SetHome(t, home)
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"version":"9.9.9"}`), 0o644))

	result, err := Detect(utils.WithoutProgramExecution(context.Background()))
	require.NoError(t, err)
	assert.True(t, result.Installed)
	assert.Equal(t, "9.9.9", result.Version)
	assert.Equal(t, claudeDir, result.ConfigPath)
}

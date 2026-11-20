package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/internal/testpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A package upgrade can remove the program that the hook entries name. The
// agent then runs every action with no Gryph hook, so status must not
// report the hooks as active.
func TestGetHookStatus_ProgramRemoved(t *testing.T) {
	home := t.TempDir()
	testpath.SetHome(t, home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))

	gone := filepath.Join(home, "Cellar", "gryph", "0.9.0", "bin", "gryph")
	result, err := InstallHooks(context.Background(), agent.InstallOptions{Command: gone})
	require.NoError(t, err)
	require.True(t, result.Success)

	status, err := GetHookStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.False(t, status.Valid)
	assert.Equal(t, []string{"no hook program at " + gone + ". Run gryph install --force to name the current program"}, status.Issues)

	present := filepath.Join(home, "bin", "gryph")
	require.NoError(t, os.MkdirAll(filepath.Dir(present), 0o755))
	require.NoError(t, os.WriteFile(present, nil, 0o755))
	_, err = InstallHooks(context.Background(), agent.InstallOptions{Command: present, Force: true})
	require.NoError(t, err)

	status, err = GetHookStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Valid)
	assert.Empty(t, status.Issues)
}

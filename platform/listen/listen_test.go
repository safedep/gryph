//go:build !windows

package listen

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "hook.sock")
	ln, err := Open(path)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o666), info.Mode().Perm(), "every account may connect")
	require.NoError(t, ln.Close())

	// A stale socket file goes, a plain file stays and refuses.
	ln, err = Open(path)
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	_, err = Open(path)
	assert.ErrorContains(t, err, "is not a socket")
}

func TestActivated_NotForThisProcess(t *testing.T) {
	t.Setenv("LISTEN_PID", "1")
	t.Setenv("LISTEN_FDS", "1")
	lns, err := Activated()
	require.NoError(t, err)
	assert.Nil(t, lns)
	_, set := os.LookupEnv("LISTEN_PID")
	assert.False(t, set, "the variables are unset after the read")
}

func TestActivated_Unset(t *testing.T) {
	lns, err := Activated()
	require.NoError(t, err)
	assert.Nil(t, lns)
	var _ net.Listener
}

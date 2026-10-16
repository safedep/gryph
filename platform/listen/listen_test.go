//go:build !windows

package listen

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
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

// On macOS this asks launchd, which has no socket for a test process.
func TestActivated_Unset(t *testing.T) {
	lns, err := Activated()
	require.NoError(t, err)
	assert.Nil(t, lns)
}

// listeners owns the descriptors it gets, so the test passes copies and
// closes its own files.
func TestListeners_TakeADescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	f, err := ln.(*net.UnixListener).File()
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	fd, err := syscall.Dup(int(f.Fd()))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	lns, err := listeners([]int{fd})
	require.NoError(t, err)
	require.Len(t, lns, 1)
	defer func() { _ = lns[0].Close() }()
	dialed := make(chan error, 1)
	go func() {
		c, err := net.Dial("unix", path)
		if err == nil {
			_ = c.Close()
		}
		dialed <- err
	}()
	require.NoError(t, <-dialed)
	conn, err := lns[0].Accept()
	require.NoError(t, err, "the listener serves the socket of the descriptor")
	_ = conn.Close()
}

func TestListeners_RefuseAPipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	fd, err := syscall.Dup(int(r.Fd()))
	require.NoError(t, err)
	require.NoError(t, r.Close())

	_, err = listeners([]int{fd})
	assert.Error(t, err, "a pipe is no listener")
}

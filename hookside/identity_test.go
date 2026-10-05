//go:build linux || darwin

package hookside

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveOnce(t *testing.T, path string) net.Conn {
	t.Helper()
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			buf := make([]byte, 1)
			_, _ = conn.Read(buf)
			_ = conn.Close()
		}
	}()
	conn, err := net.Dial("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestVerifyServer_PathChain(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("the chain check needs a root-owned temporary directory")
	}
	dir := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	socket := filepath.Join(dir, "hook.sock")
	conn := serveOnce(t, socket)
	assert.NoError(t, VerifyServer(conn, socket, "_gryph"), "a root peer behind a root-owned chain is the service")

	require.NoError(t, os.Chmod(dir, 0o777))
	conn = serveOnce(t, filepath.Join(dir, "other.sock"))
	err := VerifyServer(conn, filepath.Join(dir, "other.sock"), "_gryph")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writable by group or other")
}

func TestServiceUID(t *testing.T) {
	uid, err := serviceUID("0")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), uid)
	uid, err = serviceUID("root")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), uid)
	_, err = serviceUID("no-such-account-gryph")
	assert.Error(t, err)
}

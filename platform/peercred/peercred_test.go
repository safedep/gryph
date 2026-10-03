//go:build linux || darwin

package peercred

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_SameProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	done := make(chan *Peer, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		defer func() { _ = conn.Close() }()
		peer, err := Open(conn)
		if err != nil {
			done <- nil
			return
		}
		done <- peer
	}()
	client, err := net.Dial("unix", path)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	peer := <-done
	require.NotNil(t, peer)
	defer func() { _ = peer.Close() }()
	assert.Equal(t, uint32(os.Getuid()), peer.UID)
	assert.Equal(t, uint32(os.Getgid()), peer.GID)
	assert.Equal(t, int32(os.Getpid()), peer.PID)
}

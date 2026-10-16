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

// openSelf connects to a socket of the test and returns the peer that the
// server side reads: the test process itself.
func openSelf(t *testing.T) *Peer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	type result struct {
		peer *Peer
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()
		peer, err := Open(conn)
		done <- result{peer: peer, err: err}
	}()
	client, err := net.Dial("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	r := <-done
	require.NoError(t, r.err)
	t.Cleanup(func() { _ = r.peer.Close() })
	return r.peer
}

func TestOpen_SameProcess(t *testing.T) {
	peer := openSelf(t)
	assert.Equal(t, uint32(os.Getuid()), peer.UID)
	assert.Equal(t, uint32(os.Getgid()), peer.GID)
	assert.Equal(t, int32(os.Getpid()), peer.PID)
}

func TestOpen_StartStamp(t *testing.T) {
	peer := openSelf(t)
	assert.NotZero(t, peer.StartTime, "the start stamp is read at accept")
	assert.True(t, peer.SameProcess(), "the peer still runs")

	other := *peer
	other.StartTime++
	assert.False(t, other.SameProcess(), "another start stamp is another process")
}

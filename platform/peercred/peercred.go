// Package peercred identifies the process at the other end of a local
// socket from the kernel: its uid, gid and pid. The decision service keys
// every partition on this uid and never on a field of a request, so a
// client cannot claim another user's partition.
package peercred

import (
	"errors"
	"net"
	"syscall"
)

// ErrUnsupported says the platform cannot identify a socket peer.
var ErrUnsupported = errors.New("peercred: not supported on this platform")

// Peer is the account and the process behind a connection.
type Peer struct {
	UID uint32
	GID uint32
	PID int32
	// pidfd is a handle on the peer process on Linux, so a later check of
	// the process does not race with pid reuse. Zero elsewhere.
	pidfd int
}

// Open reads the credentials of the peer of conn. The caller closes the
// Peer when the connection ends.
func Open(conn net.Conn) (*Peer, error) {
	return open(conn)
}

// Close releases the process handle, when the platform gave one.
func (p *Peer) Close() error {
	if p == nil {
		return nil
	}
	return p.close()
}

// rawFD runs fn with the descriptor of a socket connection.
func rawFD(conn net.Conn, fn func(fd uintptr) error) error {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return ErrUnsupported
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := raw.Control(func(fd uintptr) { inner = fn(fd) }); err != nil {
		return err
	}
	return inner
}

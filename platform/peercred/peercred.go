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
	// StartTime is the start stamp of the peer process, read at accept,
	// in the unit of the platform: clock ticks since boot on Linux,
	// microseconds since the epoch on macOS. A later read of the process
	// compares against it, so a process that took the same pid is not
	// mistaken for the peer. Zero where the platform gives none.
	StartTime uint64
	// pidfd is a handle on the peer process on Linux, so a later check of
	// the process does not race with pid reuse. Zero elsewhere.
	pidfd int
	// audit is the audit login uid of the peer, read at accept: from
	// /proc on Linux, from the audit token of the connection on macOS.
	// Empty when no login session set it, or on another platform.
	audit string
}

// LoginIdentity returns the audit login identity of the peer process: the
// uid of the login session it came from, which sudo does not change. It
// reports false when the platform has none for the process.
func (p *Peer) LoginIdentity() (string, bool) {
	if p == nil || p.audit == "" {
		return "", false
	}
	return p.audit, true
}

// SameProcess reports whether pid still names the process seen at
// accept. Without a start time there is nothing to compare, and the
// answer is true.
func (p *Peer) SameProcess() bool {
	if p == nil || p.StartTime == 0 {
		return true
	}
	return sameProcess(p)
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

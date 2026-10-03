package peercred

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func open(conn net.Conn) (*Peer, error) {
	var peer Peer
	err := rawFD(conn, func(fd uintptr) error {
		ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			return fmt.Errorf("peercred: SO_PEERCRED: %w", err)
		}
		peer = Peer{UID: ucred.Uid, GID: ucred.Gid, PID: ucred.Pid}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A pidfd pins the identity of the peer process. A later check through
	// it cannot land on another process that took the same pid. A kernel
	// without pidfd_open leaves the handle out.
	if fd, err := unix.PidfdOpen(int(peer.PID), 0); err == nil {
		peer.pidfd = fd
	}
	return &peer, nil
}

func (p *Peer) close() error {
	if p.pidfd > 0 {
		err := unix.Close(p.pidfd)
		p.pidfd = 0
		return err
	}
	return nil
}

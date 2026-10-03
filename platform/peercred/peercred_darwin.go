package peercred

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func open(conn net.Conn) (*Peer, error) {
	var peer Peer
	err := rawFD(conn, func(fd uintptr) error {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			return fmt.Errorf("peercred: LOCAL_PEERCRED: %w", err)
		}
		peer.UID = cred.Uid
		if cred.Ngroups > 0 {
			peer.GID = cred.Groups[0]
		}
		pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if err != nil {
			return fmt.Errorf("peercred: LOCAL_PEERPID: %w", err)
		}
		peer.PID = int32(pid)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &peer, nil
}

func (p *Peer) close() error { return nil }

func sameProcess(*Peer) bool { return true }

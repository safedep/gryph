package peercred

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

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
	if start, err := startTimeOf(peer.PID); err == nil {
		peer.StartTime = start
	}
	return &peer, nil
}

// startTimeOf reads field 22 of /proc/<pid>/stat.
func startTimeOf(pid int32) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/stat")
	if err != nil {
		return 0, err
	}
	stat := string(data)
	end := strings.LastIndex(stat, ")")
	if end < 0 {
		return 0, errors.New("peercred: stat without a command name")
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 20 {
		return 0, errors.New("peercred: stat with too few fields")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

func sameProcess(p *Peer) bool {
	start, err := startTimeOf(p.PID)
	return err == nil && start == p.StartTime
}

func (p *Peer) close() error {
	if p.pidfd > 0 {
		err := unix.Close(p.pidfd)
		p.pidfd = 0
		return err
	}
	return nil
}

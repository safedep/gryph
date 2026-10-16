package peercred

import (
	"fmt"
	"net"
	"strconv"

	"github.com/safedep/gryph/platform/procs"
	"golang.org/x/sys/unix"
)

// auditToken is the audit_token_t of a process: eight words that the
// kernel fills at connect.
type auditToken [8]uint32

// The words of an audit token that the service reads.
const (
	tokenAUID = 0
	tokenPID  = 5
)

// auditUIDUnset is AU_DEFAUDITID: no login session set the audit uid.
const auditUIDUnset = ^uint32(0)

// x/sys has no getter for LOCAL_PEERTOKEN. ICMPv6Filter is eight uint32
// words, the size and the layout of an audit token, so its getter reads
// the token. The compiler checks the size here.
var _ [unix.SizeofICMPv6Filter]byte = [len(auditToken{}) * 4]byte{}

func open(conn net.Conn) (*Peer, error) {
	var peer Peer
	var token auditToken
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
		filter, err := unix.GetsockoptICMPv6Filter(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERTOKEN)
		if err != nil {
			return fmt.Errorf("peercred: LOCAL_PEERTOKEN: %w", err)
		}
		token = filter.Filt
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The token and LOCAL_PEERPID can name different processes, for
	// example on a socket that launchd holds for a daemon. The login
	// identity then stays empty: an answer with no identity is checked on
	// the strict side, and the connection itself goes on.
	if auid := token[tokenAUID]; token[tokenPID] == uint32(peer.PID) && auid != auditUIDUnset {
		peer.audit = strconv.FormatUint(uint64(auid), 10)
	}
	// macOS has no pidfd. The start stamp is read after accept, so a pid
	// that a new process took in that short window gives the stamp of the
	// new process. The token holds the pid version of the kernel, but no
	// interface of x/sys reads the version of a live pid to compare.
	if started, err := procs.StartedAt(int(peer.PID)); err == nil {
		peer.StartTime = uint64(started.UnixMicro())
	}
	return &peer, nil
}

func (p *Peer) close() error { return nil }

func sameProcess(p *Peer) bool {
	started, err := procs.StartedAt(int(p.PID))
	return err == nil && uint64(started.UnixMicro()) == p.StartTime
}

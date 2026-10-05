package hookside

import (
	"fmt"
	"net"
	"os/user"
	"strconv"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/platform/peercred"
)

// VerifyServer checks that the socket a hook client connected to is the
// system's decision service, and not one that a same-user process bound
// to answer every request with allow. Two checks, both from the kernel
// and the file system, never from the peer: the peer of the socket runs
// as root or as the service account, and the socket path with every
// directory above it is root-owned and not writable by group or other.
// With socket activation the peer is the service manager, so the path
// check is the one that proves the socket is the system's.
func VerifyServer(conn net.Conn, socket, account string) error {
	if err := config.VerifyManagedSocket(socket); err != nil {
		return fmt.Errorf("socket path: %w", err)
	}
	peer, err := peercred.Open(conn)
	if err != nil {
		return fmt.Errorf("socket peer: %w", err)
	}
	defer func() { _ = peer.Close() }()
	if peer.UID == 0 {
		return nil
	}
	uid, err := serviceUID(account)
	if err != nil {
		return fmt.Errorf("socket peer runs as uid %d, and the service account %s is unknown: %w", peer.UID, account, err)
	}
	if peer.UID != uid {
		return fmt.Errorf("socket peer runs as uid %d, not as root or %s", peer.UID, account)
	}
	return nil
}

// serviceUID resolves the service account, by name or as a number.
func serviceUID(account string) (uint32, error) {
	if n, err := strconv.ParseUint(account, 10, 32); err == nil {
		return uint32(n), nil
	}
	u, err := user.Lookup(account)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}

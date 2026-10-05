// Package listen opens the local socket of the decision service: from the
// service manager through socket activation, or at a path for a run in the
// foreground. The socket lives in a root-owned directory and every account
// may connect, because the service identifies each peer from the kernel.
package listen

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
)

// Activated returns the listeners the service manager passed to this
// process, or nil when it started on its own. Only systemd passes sockets
// this way today.
func Activated() ([]net.Listener, error) {
	return activated()
}

// Open listens at path. It makes the directory, removes a stale socket
// file, and opens the socket to every account. The directory, not the
// socket, carries the trust: the client checks that root owns the whole
// path.
func Open(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&fs.ModeSocket == 0:
		return nil, fmt.Errorf("listen: %s exists and is not a socket", path)
	case err == nil:
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

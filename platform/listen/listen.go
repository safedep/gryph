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
	"strconv"
)

// Activated returns the listeners the service manager passed to this
// process, or nil when it started on its own: systemd on Linux, launchd
// on macOS.
func Activated() ([]net.Listener, error) {
	return activated()
}

// listeners takes the socket descriptors that the service manager gave
// this process. A listener holds a copy of each descriptor, so the
// original closes. On an error no listener and no descriptor stays open.
func listeners(fds []int) ([]net.Listener, error) {
	var out []net.Listener
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "listen-fd-"+strconv.Itoa(fd))
		ln, err := net.FileListener(f)
		_ = f.Close()
		if err != nil {
			for _, l := range out {
				_ = l.Close()
			}
			for _, rest := range fds[i+1:] {
				_ = os.NewFile(uintptr(rest), "listen-fd-"+strconv.Itoa(rest)).Close()
			}
			return nil, fmt.Errorf("listen: descriptor %d: %w", fd, err)
		}
		out = append(out, ln)
	}
	return out, nil
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

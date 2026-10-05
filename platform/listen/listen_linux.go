package listen

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// listenFDsStart is the first descriptor systemd passes, after stdin,
// stdout and stderr.
const listenFDsStart = 3

// activated reads LISTEN_PID and LISTEN_FDS as systemd sets them. The
// variables are unset after the read, so a child process does not take
// the sockets as its own.
func activated() ([]net.Listener, error) {
	pid := os.Getenv("LISTEN_PID")
	count := os.Getenv("LISTEN_FDS")
	defer func() {
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
	}()
	if pid == "" || count == "" {
		return nil, nil
	}
	if p, err := strconv.Atoi(pid); err != nil || p != os.Getpid() {
		return nil, nil
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("listen: LISTEN_FDS=%q is not a count", count)
	}
	var out []net.Listener
	for i := 0; i < n; i++ {
		f := os.NewFile(uintptr(listenFDsStart+i), "listen-fd-"+strconv.Itoa(i))
		ln, err := net.FileListener(f)
		_ = f.Close()
		if err != nil {
			for _, l := range out {
				_ = l.Close()
			}
			return nil, fmt.Errorf("listen: descriptor %d: %w", listenFDsStart+i, err)
		}
		out = append(out, ln)
	}
	return out, nil
}

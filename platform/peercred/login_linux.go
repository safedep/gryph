package peercred

import (
	"os"
	"strconv"
	"strings"
)

// loginUIDUnset is what the kernel reports for a process that no login
// session started, for example one in a container or under a service.
const loginUIDUnset = "4294967295"

// LoginIdentity returns the audit login identity of the peer process: the
// uid of the login session it came from, which sudo does not change. It
// reports false when the kernel has none for the process, or when the
// process is gone.
func (p *Peer) LoginIdentity() (string, bool) {
	if p == nil || p.PID <= 0 {
		return "", false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(p.PID)) + "/loginuid")
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	if id == "" || id == loginUIDUnset {
		return "", false
	}
	return id, true
}

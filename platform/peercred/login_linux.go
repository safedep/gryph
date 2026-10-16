package peercred

import (
	"os"
	"strconv"
	"strings"
)

// loginUIDUnset is what the kernel reports for a process that no login
// session started, for example one in a container or under a service.
const loginUIDUnset = "4294967295"

// loginUID reads the audit login uid of pid, or empty when the kernel has
// none for the process.
func loginUID(pid int32) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/loginuid")
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	if id == loginUIDUnset {
		return ""
	}
	return id
}

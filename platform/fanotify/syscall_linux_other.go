//go:build linux && !amd64

package fanotify

import "golang.org/x/sys/unix"

// openFlagsIndex returns the argument index of the flags of an open
// syscall, or -1 for a call whose flags are not in the arguments.
func openFlagsIndex(nr int) int {
	if nr == unix.SYS_OPENAT {
		return 2
	}
	return -1
}

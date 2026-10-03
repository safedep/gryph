package fanotify

import "golang.org/x/sys/unix"

// openFlagsIndex returns the argument index of the flags of an open
// syscall, or -1 for a call whose flags are not in the arguments: openat2
// keeps them in a struct, and a 32-bit process uses other numbers.
func openFlagsIndex(nr int) int {
	switch nr {
	case unix.SYS_OPEN:
		return 1
	case unix.SYS_OPENAT:
		return 2
	}
	return -1
}

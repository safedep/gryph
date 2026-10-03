//go:build !linux

package procs

import (
	"os"
)

// running asks the kernel for the process. Without /proc, a process that
// is not reaped yet still counts as alive.
func running(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return signalZero(p)
}

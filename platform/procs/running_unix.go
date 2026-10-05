//go:build !linux && !windows

package procs

import (
	"os"
	"syscall"
)

func signalZero(p *os.Process) bool {
	return p.Signal(syscall.Signal(0)) == nil
}

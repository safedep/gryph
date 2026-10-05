package procs

import (
	"errors"
	"time"
)

// ErrUnsupported says the platform gives no parent chain.
var ErrUnsupported = errors.New("procs: the parent chain is not supported on this platform")

// Ancestors returns the parents of pid, nearest first, up to the first
// process. It reads what the kernel reports at the time of the call, so a
// caller that needs the same process it saw before checks the start time
// of pid itself.
func Ancestors(pid int) ([]Process, error) {
	return ancestors(pid)
}

// StartedAt returns the start time of pid, or an error when the process
// is gone.
func StartedAt(pid int) (time.Time, error) {
	return startedAt(pid)
}

package procs

import "os"

// signalZero is not a probe on Windows. FindProcess opens the process, so
// a handle means it exists.
func signalZero(*os.Process) bool {
	return true
}

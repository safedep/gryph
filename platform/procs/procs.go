// Package procs lists the processes of the current operating-system account.
// Gryph uses the list to find a live agent that sends no hook traffic. Each
// platform has its own source: /proc on Linux, the kern.proc sysctl on
// macOS, the toolhelp snapshot on Windows.
package procs

import (
	"strings"
	"time"
)

// Process is one process of the current account.
type Process struct {
	PID int
	// Name is the short name of the program, as the kernel reports it.
	Name string
	// Path is the executable, when the platform gives it.
	Path string
	// Started is when the process started. It is zero when unknown.
	Started time.Time
}

// List returns the processes of the current account.
func List() ([]Process, error) {
	return list()
}

// Matches reports whether the process runs the program name. The match is
// on the short name, without a Windows .exe suffix, and ignores case.
func (p Process) Matches(name string) bool {
	return strings.EqualFold(strings.TrimSuffix(strings.ToLower(p.Name), ".exe"), strings.ToLower(name))
}

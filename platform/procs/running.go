package procs

// Running reports whether a process with the pid is alive. A process that
// has exited and waits for its parent to reap it is not alive.
func Running(pid int) bool {
	return running(pid)
}

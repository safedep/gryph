package procs

// running reads the state of the process. A zombie runs nothing and
// takes no signal, so it does not count.
func running(pid int) bool {
	kp, err := kinfo(pid)
	return err == nil && kp.Proc.P_stat != sZomb
}

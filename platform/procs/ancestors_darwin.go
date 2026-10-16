package procs

import "time"

// ancestors walks the parents through the kern.proc.pid sysctl. The
// record of each process names its parent, its short name, its real uid
// and its start time. The walk has no executable path: the sysctl that
// gives it answers only for the account of the process or root.
func ancestors(pid int) ([]Process, error) {
	var out []Process
	kp, err := kinfo(pid)
	if err != nil {
		return nil, err
	}
	for i := 0; i < maxAncestors && kp.Proc.P_pid > 1; i++ {
		ppid := int(kp.Eproc.Ppid)
		if ppid <= 0 {
			return out, nil
		}
		if kp, err = kinfo(ppid); err != nil {
			return out, err
		}
		out = append(out, fromKinfo(kp))
	}
	return out, nil
}

func startedAt(pid int) (time.Time, error) {
	kp, err := kinfo(pid)
	if err != nil {
		return time.Time{}, err
	}
	return fromKinfo(kp).Started, nil
}

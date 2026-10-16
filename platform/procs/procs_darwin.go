package procs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func list() ([]Process, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	uid := uint32(os.Getuid())
	var out []Process
	for i := range procs {
		if procs[i].Eproc.Ucred.Uid != uid {
			continue
		}
		out = append(out, fromKinfo(&procs[i]))
	}
	return out, nil
}

// kinfo reads one process from the kern.proc.pid sysctl. Every account
// may read it. A pid with no process gives no record, which x/sys reports
// as EIO.
func kinfo(pid int) (*unix.KinfoProc, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.EIO) {
		return nil, fmt.Errorf("procs: no process %d: %w", pid, os.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("procs: process %d: %w", pid, err)
	}
	return kp, nil
}

// fromKinfo takes the process fields of a record. The name is the short
// name the kernel keeps, at most 16 bytes. UID is the real uid.
func fromKinfo(kp *unix.KinfoProc) Process {
	name := kp.Proc.P_comm[:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return Process{
		PID:     int(kp.Proc.P_pid),
		Name:    string(name),
		Started: time.Unix(kp.Proc.P_starttime.Sec, int64(kp.Proc.P_starttime.Usec)*1000),
		UID:     int(kp.Eproc.Pcred.P_ruid),
	}
}

// sZomb is SZOMB, the state of a process that exited and that its parent
// has not waited for.
const sZomb = 5

// listUID lists the processes whose real uid is owner. A zombie runs
// nothing and takes no signal, so it is not listed.
func listUID(owner int) ([]Process, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.ruid", owner)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var out []Process
	for i := range procs {
		if procs[i].Proc.P_stat == sZomb {
			continue
		}
		out = append(out, fromKinfo(&procs[i]))
	}
	return out, nil
}

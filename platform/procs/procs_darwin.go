package procs

import (
	"bytes"
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
	for _, kp := range procs {
		if kp.Eproc.Ucred.Uid != uid {
			continue
		}
		name := kp.Proc.P_comm[:]
		if i := bytes.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		out = append(out, Process{
			PID:     int(kp.Proc.P_pid),
			Name:    string(name),
			Started: time.Unix(kp.Proc.P_starttime.Sec, int64(kp.Proc.P_starttime.Usec)*1000),
			UID:     os.Getuid(),
		})
	}
	return out, nil
}

package procs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxAncestors bounds the walk. A chain longer than this is a loop in
// what the kernel reports, which does not happen, or a container with a
// very deep tree.
const maxAncestors = 128

func ancestors(pid int) ([]Process, error) {
	var out []Process
	boot := bootTime()
	for i := 0; i < maxAncestors && pid > 1; i++ {
		ppid, name, err := parentOf(pid)
		if err != nil {
			return out, err
		}
		if ppid <= 0 {
			return out, nil
		}
		dir := filepath.Join("/proc", strconv.Itoa(ppid))
		p := Process{PID: ppid, Name: name, UID: realUID(dir)}
		if comm, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
			p.Name = strings.TrimSpace(string(comm))
		}
		if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
			p.Path = exe
		}
		p.Started = startTime(dir, boot)
		out = append(out, p)
		pid = ppid
	}
	return out, nil
}

// realUID reads the real uid from /proc/<pid>/status, or -1.
func realUID(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "Uid:" {
			if uid, err := strconv.Atoi(fields[1]); err == nil {
				return uid
			}
			return -1
		}
	}
	return -1
}

// parentOf reads the parent pid of pid from field 4 of /proc/<pid>/stat.
// The command name in field 2 can hold spaces and parentheses, so the
// fields start after the last closing parenthesis.
func parentOf(pid int) (int, string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, "", err
	}
	stat := string(data)
	end := strings.LastIndex(stat, ")")
	start := strings.Index(stat, "(")
	if end < 0 || start < 0 || end < start {
		return 0, "", fmt.Errorf("procs: stat of %d without a command name", pid)
	}
	name := stat[start+1 : end]
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 2 {
		return 0, "", fmt.Errorf("procs: stat of %d with too few fields", pid)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", fmt.Errorf("procs: stat of %d: %w", pid, err)
	}
	return ppid, name, nil
}

func startedAt(pid int) (time.Time, error) {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	if _, err := os.Stat(filepath.Join(dir, "stat")); err != nil {
		return time.Time{}, err
	}
	started := startTime(dir, bootTime())
	if started.IsZero() {
		return time.Time{}, fmt.Errorf("procs: no start time for %d", pid)
	}
	return started, nil
}

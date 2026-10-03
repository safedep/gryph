package procs

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// userHZ is the clock of the start time in /proc/<pid>/stat. The kernel
// reports it in USER_HZ, which is 100 on every Linux architecture.
const userHZ = 100

func list() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	uid := strconv.Itoa(os.Getuid())
	boot := bootTime()
	var out []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		if !ownedBy(dir, uid) {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil {
			continue
		}
		p := Process{PID: pid, Name: strings.TrimSpace(string(comm))}
		if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
			p.Path = exe
		}
		p.Started = startTime(dir, boot)
		out = append(out, p)
	}
	return out, nil
}

// ownedBy reports whether the real uid in /proc/<pid>/status is uid.
func ownedBy(dir, uid string) bool {
	f, err := os.Open(filepath.Join(dir, "status"))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) >= 2 && fields[0] == "Uid:" {
			return fields[1] == uid
		}
	}
	return false
}

// startTime reads the start time of the process from field 22 of
// /proc/<pid>/stat, in clock ticks after boot. The program name in field 2
// can hold spaces, so the parse starts after its closing parenthesis.
func startTime(dir string, boot time.Time) time.Time {
	if boot.IsZero() {
		return time.Time{}
	}
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return time.Time{}
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return time.Time{}
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return boot.Add(time.Duration(ticks) * time.Second / userHZ)
}

// bootTime reads btime from /proc/stat.
func bootTime() time.Time {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) == 2 && fields[0] == "btime" {
			if sec, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return time.Unix(sec, 0)
			}
		}
	}
	return time.Time{}
}

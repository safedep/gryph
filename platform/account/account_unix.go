//go:build !windows

package account

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func currentID() (string, error) {
	return strconv.Itoa(os.Getuid()), nil
}

// The first uid of a human account. Linux distributions start at 1000,
// macOS at 501. An account above the nobody range is not human either.
const (
	minHumanUIDLinux  = 1000
	minHumanUIDDarwin = 500
	maxHumanUID       = 60000
)

func minHumanUID() int {
	if runtime.GOOS == "darwin" {
		return minHumanUIDDarwin
	}
	return minHumanUIDLinux
}

// list reads /etc/passwd on Linux. On macOS the local accounts live in the
// directory service, not in /etc/passwd, so list walks /Users and asks the
// account database for each name.
func list() ([]Account, error) {
	if runtime.GOOS == "darwin" {
		return listDarwin()
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parsePasswd(f, minHumanUID(), dirExists)
}

// parsePasswd keeps the human accounts of a passwd file whose home exists.
func parsePasswd(r io.Reader, minUID int, homeExists func(string) bool) ([]Account, error) {
	var out []Account
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < minUID || uid >= maxHumanUID {
			continue
		}
		home := fields[5]
		if home == "" || !homeExists(home) {
			continue
		}
		out = append(out, Account{ID: fields[2], Name: fields[0], Home: home})
	}
	return out, scanner.Err()
}

func listDarwin() ([]Account, error) {
	entries, err := os.ReadDir("/Users")
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		u, err := user.Lookup(entry.Name())
		if err != nil {
			continue
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil || uid < minHumanUID() || uid >= maxHumanUID {
			continue
		}
		home := u.HomeDir
		if home == "" {
			home = filepath.Join("/Users", entry.Name())
		}
		out = append(out, Account{ID: u.Uid, Name: u.Username, Home: home})
	}
	return out, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// command builds the process with the account's credentials. The kernel
// applies them before the program runs, so no file opens as root.
func command(ctx context.Context, acct Account, program string, args ...string) (*exec.Cmd, error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("run a command as %s: only root can change the account", acct.Name)
	}
	u, err := user.LookupId(acct.ID)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("account %s: uid %q: %w", acct.Name, u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("account %s: gid %q: %w", acct.Name, u.Gid, err)
	}
	if uid == 0 {
		return nil, fmt.Errorf("account %s is root", acct.Name)
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	cmd.Env = []string{
		"HOME=" + acct.Home,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"PATH=/usr/bin:/bin",
	}
	cmd.Dir = "/"
	return cmd, nil
}

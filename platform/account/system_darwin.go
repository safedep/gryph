package account

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// The range of the ids of a system account. macOS keeps ids below 500
// for accounts that do not log in, and its own daemons use the low part
// of that range.
const (
	systemIDFirst = 300
	systemIDLast  = 499
)

// dscl runs the directory service command line and returns its output.
// Tests replace it.
var dscl = func(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "dscl", append([]string{"."}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dscl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// createSystem makes a group and a user of the same name and id in the
// local directory service. macOS has no useradd. The user is hidden from
// the login window and cannot log in. A failure after the first record
// removes what the call made, so a later run starts again from nothing.
func createSystem(ctx context.Context, name, home string) error {
	users, err := dscl(ctx, "-list", "/Users", "UniqueID")
	if err != nil {
		return err
	}
	groups, err := dscl(ctx, "-list", "/Groups", "PrimaryGroupID")
	if err != nil {
		return err
	}
	id, err := freeSystemID(users, groups)
	if err != nil {
		return err
	}
	ids := strconv.Itoa(id)
	group, user := "/Groups/"+name, "/Users/"+name
	steps := [][]string{
		{"-create", group},
		{"-create", group, "PrimaryGroupID", ids},
		{"-create", group, "RealName", name},
		{"-create", group, "Password", "*"},
		{"-create", user},
		{"-create", user, "UniqueID", ids},
		{"-create", user, "PrimaryGroupID", ids},
		{"-create", user, "RealName", name},
		{"-create", user, "NFSHomeDirectory", home},
		{"-create", user, "UserShell", "/usr/bin/false"},
		{"-create", user, "Password", "*"},
		{"-create", user, "IsHidden", "1"},
	}
	for _, step := range steps {
		if _, err := dscl(ctx, step...); err != nil {
			for _, record := range []string{user, group} {
				_, _ = dscl(ctx, "-delete", record)
			}
			return fmt.Errorf("account %s: %w", name, err)
		}
	}
	return nil
}

// freeSystemID returns the lowest id in the system range that no user and
// no group has. The input is the output of dscl -list: a name and an id
// on each line.
func freeSystemID(users, groups string) (int, error) {
	taken := map[int]bool{}
	for _, listing := range []string{users, groups} {
		for _, line := range strings.Split(listing, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if id, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				taken[id] = true
			}
		}
	}
	for id := systemIDFirst; id <= systemIDLast; id++ {
		if !taken[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no free id between %d and %d for a system account", systemIDFirst, systemIDLast)
}

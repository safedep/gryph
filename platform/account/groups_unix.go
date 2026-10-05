//go:build !windows

package account

import (
	"os/user"
)

// memberOf reports whether the account with id is in group, by name,
// through the primary and the supplementary groups of the OS database.
func memberOf(id, group string) (bool, error) {
	u, err := user.LookupId(id)
	if err != nil {
		return false, err
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return false, err
	}
	if u.Gid == g.Gid {
		return true, nil
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false, err
	}
	for _, gid := range ids {
		if gid == g.Gid {
			return true, nil
		}
	}
	return false, nil
}

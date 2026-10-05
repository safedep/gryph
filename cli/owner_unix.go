//go:build !windows

package cli

import (
	"io/fs"
	"syscall"
)

// ownerOf returns the uid and gid of the file of info.
func ownerOf(info fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

// reloadSignal asks the process pid to reload its keys.
func reloadSignal(pid int) error {
	return syscall.Kill(pid, syscall.SIGHUP)
}

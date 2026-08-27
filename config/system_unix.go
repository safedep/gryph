//go:build !windows

package config

import (
	"os"
	"syscall"
)

// verifyManagedFileTrust accepts only a root owned file that group and other
// cannot write. Anything else would let a user plant a fake managed config
// that governs every user on the machine.
func verifyManagedFileTrust(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return st.Uid == 0 && info.Mode().Perm()&0o022 == 0
}

//go:build !windows

package utils

import "os"

// isPrivileged reports a root process.
func isPrivileged() bool {
	return os.Geteuid() == 0
}

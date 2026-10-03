//go:build windows

package utils

import "golang.org/x/sys/windows"

// isPrivileged reports an elevated process.
func isPrivileged() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

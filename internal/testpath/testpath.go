// Package testpath gives tests absolute paths that hold on every OS.
package testpath

import (
	"runtime"
	"testing"
)

// Abs returns the slash path p as an absolute path of the running OS. On
// Windows a path without a drive is not absolute, so Abs adds one and keeps
// the slashes. A test that matches the slash form in the output still finds
// it.
func Abs(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

// SetHome makes dir the home directory for the rest of the test. Go reads
// HOME on Unix and USERPROFILE on Windows, so a test that sets only HOME
// reads the home of the runner on Windows.
func SetHome(t testing.TB, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

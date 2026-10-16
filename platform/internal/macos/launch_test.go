//go:build darwin

package macos

import (
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A test process is no launchd job with a socket: launchd answers ESRCH
// for a process it did not start, and ENOENT for a job without the name.
func TestActivateSocket_NotAJob(t *testing.T) {
	fds, err := ActivateSocket("Listeners")
	assert.Nil(t, fds)
	assert.True(t, errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT), "launchd answers with an errno: %v", err)
}

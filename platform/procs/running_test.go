package procs

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunning(t *testing.T) {
	assert.True(t, Running(os.Getpid()))
	assert.False(t, Running(1<<30))
	if runtime.GOOS != "linux" {
		t.Skip("the not-reaped case needs /proc")
	}
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	// The child exits on its own. Until Wait reaps it, the kernel keeps
	// the pid as a zombie, which Running does not count as alive.
	for Running(pid) {
	}
	assert.False(t, Running(pid))
	require.NoError(t, cmd.Wait())
	assert.False(t, Running(pid))
}

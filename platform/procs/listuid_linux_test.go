package procs

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListUID_SkipsAZombie(t *testing.T) {
	// A child that exited and is not yet waited for is a zombie.
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && running(cmd.Process.Pid) {
		time.Sleep(10 * time.Millisecond)
	}
	require.False(t, running(cmd.Process.Pid), "the child must be a zombie by now")

	list, err := ListUID(os.Getuid())
	require.NoError(t, err)
	for _, p := range list {
		assert.NotEqual(t, cmd.Process.Pid, p.PID, "a zombie is not listed")
	}
}

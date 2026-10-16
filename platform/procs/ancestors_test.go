//go:build linux || darwin

package procs

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAncestors_ThisProcess(t *testing.T) {
	chain, err := Ancestors(os.Getpid())
	require.NoError(t, err)
	require.NotEmpty(t, chain)
	assert.Equal(t, os.Getppid(), chain[0].PID, "the nearest ancestor is the parent")
	assert.NotEmpty(t, chain[0].Name)
	assert.Equal(t, os.Getuid(), chain[0].UID, "the test runner and its parent share the account")
	assert.False(t, chain[0].Started.IsZero())
}

func TestStartedAt(t *testing.T) {
	started, err := StartedAt(os.Getpid())
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), started, time.Hour)

	parent, err := StartedAt(os.Getppid())
	require.NoError(t, err)
	chain, err := Ancestors(os.Getpid())
	require.NoError(t, err)
	assert.True(t, parent.Equal(chain[0].Started), "the walk and StartedAt read the same start time")

	_, err = StartedAt(2147483000)
	assert.Error(t, err)
}

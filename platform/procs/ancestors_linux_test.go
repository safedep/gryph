package procs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAncestors_ThisProcess(t *testing.T) {
	chain, err := Ancestors(os.Getpid())
	require.NoError(t, err)
	require.NotEmpty(t, chain)
	assert.Equal(t, os.Getppid(), chain[0].PID, "the nearest ancestor is the parent")
	assert.NotEmpty(t, chain[0].Name)
	// The chain ends at a process with no parent: pid 1, or the top of a
	// pid namespace that does not show it.
	last := chain[len(chain)-1]
	if ppid, _, err := parentOf(last.PID); err == nil {
		assert.Equal(t, 0, ppid, "the chain ends at a process with no parent")
	}
}

func TestParentOf(t *testing.T) {
	ppid, name, err := parentOf(os.Getpid())
	require.NoError(t, err)
	assert.Equal(t, os.Getppid(), ppid)
	assert.NotEmpty(t, name)
	_, _, err = parentOf(2147483000)
	assert.Error(t, err)
}

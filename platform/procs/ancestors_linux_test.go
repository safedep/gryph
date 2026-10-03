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
	assert.Equal(t, 1, chain[len(chain)-1].PID, "the chain ends at the first process")
}

func TestParentOf(t *testing.T) {
	ppid, name, err := parentOf(os.Getpid())
	require.NoError(t, err)
	assert.Equal(t, os.Getppid(), ppid)
	assert.NotEmpty(t, name)
	_, _, err = parentOf(2147483000)
	assert.Error(t, err)
}

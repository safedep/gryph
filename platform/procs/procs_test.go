package procs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestList_FindsThisProcess(t *testing.T) {
	procs, err := List()
	require.NoError(t, err)

	exe, err := os.Executable()
	require.NoError(t, err)
	var me *Process
	for i := range procs {
		if procs[i].PID == os.Getpid() {
			me = &procs[i]
		}
	}
	require.NotNil(t, me, "the list holds the current process")
	assert.True(t, me.Matches(filepath.Base(exe)) || len(me.Name) >= 15,
		"the name is the program, or a kernel-truncated prefix of it: %q", me.Name)
	assert.False(t, me.Started.IsZero())
	assert.True(t, me.Started.Before(time.Now().Add(time.Second)))
	assert.True(t, me.Started.After(time.Now().Add(-24*time.Hour)))
}

func TestProcess_Matches(t *testing.T) {
	assert.True(t, Process{Name: "claude"}.Matches("claude"))
	assert.True(t, Process{Name: "Claude.exe"}.Matches("claude"))
	assert.True(t, Process{Name: "Cursor"}.Matches("cursor"))
	assert.False(t, Process{Name: "claude-helper"}.Matches("claude"))
	assert.False(t, Process{Name: "gryph"}.Matches("claude"))
}

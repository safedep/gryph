package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSupervisorEnabled(t *testing.T) {
	in := "# the decision service of this host\npolicy:\n  enabled: true\nsupervisor:\n  enabled: true\n  socket: /run/x.sock\n"
	off, err := SetSupervisorEnabled([]byte(in), false)
	require.NoError(t, err)
	assert.Contains(t, string(off), "# the decision service of this host")
	assert.Contains(t, string(off), "  enabled: false\n  socket: /run/x.sock\n")
	cfg, err := Parse(off)
	require.NoError(t, err)
	assert.False(t, cfg.Supervisor.Enabled)
	assert.Equal(t, "/run/x.sock", cfg.Supervisor.Socket)

	on, err := SetSupervisorEnabled(off, true)
	require.NoError(t, err)
	cfg, err = Parse(on)
	require.NoError(t, err)
	assert.True(t, cfg.Supervisor.Enabled)

	added, err := SetSupervisorEnabled([]byte("policy:\n  enabled: true\n"), true)
	require.NoError(t, err)
	cfg, err = Parse(added)
	require.NoError(t, err)
	assert.True(t, cfg.Supervisor.Enabled, "a missing section is added")

	empty, err := SetSupervisorEnabled(nil, false)
	require.NoError(t, err)
	cfg, err = Parse(empty)
	require.NoError(t, err)
	assert.False(t, cfg.Supervisor.Enabled)

	_, err = SetSupervisorEnabled([]byte("- a list\n"), false)
	assert.Error(t, err)
}

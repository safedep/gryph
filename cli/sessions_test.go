package cli

import (
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/stretchr/testify/assert"
)

func TestGetAgentDisplayName(t *testing.T) {
	reg := agent.NewRegistry()
	reg.Register(claudecode.New(nil, "", false))

	assert.Equal(t, "Claude Code", getAgentDisplayName(reg, "claude-code"))
	assert.Equal(t, "future-agent", getAgentDisplayName(reg, "future-agent"))
}

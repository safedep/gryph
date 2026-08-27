package livelog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCycleAgentFilter(t *testing.T) {
	m := New(Options{AgentNames: []string{"claude-code", "cursor"}})
	assert.Equal(t, "", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "claude-code", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "cursor", m.agentFilter)

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestCycleAgentFilter_UnknownFilterResets(t *testing.T) {
	m := New(Options{AgentNames: []string{"claude-code"}, AgentFilter: "gone-agent"})

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestCycleAgentFilter_EmptyNameList(t *testing.T) {
	m := New(Options{})

	m.cycleAgentFilter()
	assert.Equal(t, "", m.agentFilter)
}

func TestAgentBadge_UnknownAgentRendersRawName(t *testing.T) {
	assert.Contains(t, agentBadge("future-agent"), "future-agent")
}

func TestAgentBadge_KnownAgentRendersName(t *testing.T) {
	assert.Contains(t, agentBadge("claude-code"), "claude-code")
}

package piagent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGryphExtensionContent_HasAllHooks(t *testing.T) {
	for _, hookType := range HookTypes {
		assert.Contains(t, gryphExtensionContent, hookType,
			"extension should handle %s hook", hookType)
	}
}

func TestGryphExtensionContent_SpawnsGryph(t *testing.T) {
	assert.Contains(t, gryphExtensionContent, "gryph")
	assert.Contains(t, gryphExtensionContent, "_hook")
	assert.Contains(t, gryphExtensionContent, "pi-agent")
}

func TestGryphExtensionContent_UsesPiEvents(t *testing.T) {
	assert.Contains(t, gryphExtensionContent, "pi.on")
	assert.Contains(t, gryphExtensionContent, "session_start")
	assert.Contains(t, gryphExtensionContent, "session_shutdown")
	assert.Contains(t, gryphExtensionContent, "tool_call")
	assert.Contains(t, gryphExtensionContent, "tool_result")
}

func TestGryphExtensionContent_GryphCommand(t *testing.T) {
	assert.True(t, strings.Contains(gryphExtensionContent, `spawn("gryph"`),
		"extension should spawn gryph command")
	assert.True(t, strings.Contains(gryphExtensionContent, `["_hook", "pi-agent"`),
		"extension should pass correct args to gryph")
}

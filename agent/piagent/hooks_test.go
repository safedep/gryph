package piagent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPluginTS_HasAllHooks(t *testing.T) {
	pluginContent := string(pluginTS)
	for _, hookType := range HookTypes {
		assert.Contains(t, pluginContent, hookType,
			"extension should handle %s hook", hookType)
	}
}

func TestPluginTS_SpawnsGryph(t *testing.T) {
	pluginContent := string(pluginTS)
	assert.Contains(t, pluginContent, "__GRYPH_COMMAND__")
	assert.Contains(t, pluginContent, "_hook")
	assert.Contains(t, pluginContent, "pi-agent")
}

func TestPluginTS_UsesPiEvents(t *testing.T) {
	pluginContent := string(pluginTS)
	assert.Contains(t, pluginContent, "pi.on")
	assert.Contains(t, pluginContent, "session_start")
	assert.Contains(t, pluginContent, "session_shutdown")
	assert.Contains(t, pluginContent, "tool_call")
	assert.Contains(t, pluginContent, "tool_result")
}

func TestProcessedPlugin_ReplacesPlaceholder(t *testing.T) {
	processed := string(processedPlugin())
	assert.True(t, strings.Contains(processed, `"gryph"`),
		"processed plugin should have gryph command replaced")
	assert.True(t, strings.Contains(processed, `["_hook", "pi-agent"`),
		"processed plugin should pass correct args to gryph")
}

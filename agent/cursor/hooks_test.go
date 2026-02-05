package cursor

import (
	"strings"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
)

func TestGenerateHooksConfig_CommandFormat(t *testing.T) {
	config := GenerateHooksConfig()
	expectedPrefix := utils.GryphCommand() + " _hook cursor "

	for _, hookType := range HookTypes {
		commands, ok := config.Hooks[hookType]
		assert.True(t, ok, "hook type %s should exist in config", hookType)
		assert.Len(t, commands, 1, "hook type %s should have exactly one command", hookType)

		cmd := commands[0].Command
		assert.True(t, strings.HasPrefix(cmd, expectedPrefix), "command should start with %q, got %q", expectedPrefix, cmd)
		assert.True(t, strings.HasSuffix(cmd, hookType), "command should end with %q, got %q", hookType, cmd)
	}
}

package claudecode

import (
	"strings"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
)

func TestGenerateHooksConfig_CommandFormat(t *testing.T) {
	config := GenerateHooksConfig()
	expectedPrefix := utils.GryphCommand() + " _hook claude-code "

	for _, hookType := range HookTypes {
		matchers, ok := config[hookType]
		assert.True(t, ok, "hook type %s should exist in config", hookType)
		assert.Len(t, matchers, 1, "hook type %s should have exactly one matcher", hookType)
		assert.Len(t, matchers[0].Hooks, 1, "hook type %s matcher should have exactly one hook command", hookType)

		cmd := matchers[0].Hooks[0].Command
		assert.True(t, strings.HasPrefix(cmd, expectedPrefix), "command should start with %q, got %q", expectedPrefix, cmd)
		assert.True(t, strings.HasSuffix(cmd, hookType), "command should end with %q, got %q", hookType, cmd)
	}
}

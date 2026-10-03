package cursor

import (
	"strings"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHooksConfig_CommandFormat(t *testing.T) {
	config := GenerateHooksConfig("")
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

func TestFailClosedIn(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"every gryph entry sets it", `{"version":1,"hooks":{"beforeShellExecution":[{"command":"/opt/gryph _hook cursor beforeShellExecution","failClosed":true}],"stop":[{"command":"/opt/gryph _hook cursor stop","failClosed":true},{"command":"/usr/bin/other","failClosed":false}]}}`, true},
		{"one gryph entry without it", `{"version":1,"hooks":{"beforeShellExecution":[{"command":"/opt/gryph _hook cursor beforeShellExecution","failClosed":true}],"stop":[{"command":"gryph _hook cursor stop"}]}}`, false},
		{"no gryph entry", `{"version":1,"hooks":{"stop":[{"command":"/usr/bin/other","failClosed":true}]}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := failClosedIn([]byte(tc.data))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := failClosedIn([]byte(`{not json`))
	assert.Error(t, err)
}

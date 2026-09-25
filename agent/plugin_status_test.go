package agent

import (
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func TestSetPluginStatus(t *testing.T) {
	specs := []events.HookSpec{
		{Type: "chat.message", Prompt: true},
		{Type: "tool.execute.before"},
		{Type: "tool.execute.after"},
	}
	current := []byte(`"chat.message" "tool.execute.before" "tool.execute.after"`)
	marker := func(t string) string { return `"` + t + `"` }

	cases := []struct {
		name      string
		content   string
		wantValid bool
		wantHooks []string
	}{
		{"current plugin", string(current), true, []string{"chat.message", "tool.execute.before", "tool.execute.after"}},
		{"old plugin without the prompt hook", `"tool.execute.before" "tool.execute.after"`, true, []string{"tool.execute.before", "tool.execute.after"}},
		{"plugin without a required hook", `"chat.message" "tool.execute.before"`, false, []string{"chat.message", "tool.execute.before"}},
		{"edited plugin with every hook", string(current) + " // edited", false, []string{"chat.message", "tool.execute.before", "tool.execute.after"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := &HookStatus{}
			SetPluginStatus(status, []byte(tc.content), current, specs, marker, "stale")
			assert.True(t, status.Installed)
			assert.Equal(t, tc.wantValid, status.Valid)
			assert.Equal(t, tc.wantHooks, status.Hooks)
			assert.Equal(t, !tc.wantValid, len(status.Issues) == 1)
		})
	}
}

package agent

import (
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func TestSetPluginStatus(t *testing.T) {
	specs := []events.HookSpec{
		{Type: "chat.message", Prompt: true},
		{Type: "tool.execute.before"},
		{Type: "tool.execute.after"},
	}
	current := `"chat.message" "tool.execute.before" "tool.execute.after"`
	legacy := `"tool.execute.before" "tool.execute.after"`
	legacyDigests := []string{utils.HashContent(legacy)}
	marker := func(t string) string { return `"` + t + `"` }

	cases := []struct {
		name      string
		content   string
		wantValid bool
		wantHooks []string
	}{
		{"current plugin", current, true, []string{"chat.message", "tool.execute.before", "tool.execute.after"}},
		{"known legacy plugin without the prompt hook", legacy, true, []string{"tool.execute.before", "tool.execute.after"}},
		{"changed legacy plugin without the prompt hook", legacy + " // edited", false, []string{"tool.execute.before", "tool.execute.after"}},
		{"markers in a comment only", `// "tool.execute.before" "tool.execute.after"`, false, []string{"tool.execute.before", "tool.execute.after"}},
		{"plugin without a required hook", `"chat.message" "tool.execute.before"`, false, []string{"chat.message", "tool.execute.before"}},
		{"edited plugin with every hook", current + " // edited", false, []string{"chat.message", "tool.execute.before", "tool.execute.after"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := &HookStatus{}
			SetPluginStatus(status, []byte(tc.content), []byte(current), legacyDigests, specs, marker, "stale")
			assert.True(t, status.Installed)
			assert.Equal(t, tc.wantValid, status.Valid)
			assert.Equal(t, tc.wantHooks, status.Hooks)
			assert.Equal(t, !tc.wantValid, len(status.Issues) == 1)
		})
	}
}

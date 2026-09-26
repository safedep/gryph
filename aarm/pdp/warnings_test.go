package pdp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyWarnings_FormerPromptTools(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int
	}{
		{"cursor tool name", "match:\n      tool_names: [beforeSubmitPrompt]", 1},
		{"windsurf tool name in scope", "scope:\n      tools: [pre_user_prompt]", 1},
		{"codex tool name in another case", "match:\n      tool_names: [userpromptsubmit, Bash]", 1},
		{"user_prompt action type", "match:\n      action_types: [user_prompt]", 0},
		{"other tool", "match:\n      tool_names: [Bash]", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy([]byte("version: \"1\"\nrules:\n  - id: r\n    action: block\n    " + tc.yaml + "\n"))
			require.NoError(t, err)
			warnings := p.Warnings()
			require.Len(t, warnings, tc.want)
			for _, w := range warnings {
				assert.Contains(t, w, `rule "r"`)
				assert.Contains(t, w, "user_prompt")
			}
		})
	}
}

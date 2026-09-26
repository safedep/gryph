package pdp

import (
	"fmt"
	"slices"
	"strings"
)

// formerPromptTools are the tool names that prompts carried before prompts
// became user_prompt actions. A prompt now has no tool name, so a rule that
// names one of them no longer matches a prompt.
var formerPromptTools = []string{"beforeSubmitPrompt", "pre_user_prompt", "UserPromptSubmit"}

// Warnings returns the problems in a policy that do not make it invalid.
func (p *Policy) Warnings() []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, rule := range p.Rules {
		for _, tool := range slices.Concat(rule.Match.ToolNames, rule.Scope.Tools) {
			if !containsFold(formerPromptTools, tool) {
				continue
			}
			out = append(out, fmt.Sprintf(
				"rule %q names the tool %q. A prompt is now a user_prompt action with no tool name, so this rule does not match prompts. Use action_types: [user_prompt] to match prompts.",
				rule.ID, strings.TrimSpace(tool)))
		}
	}
	return out
}

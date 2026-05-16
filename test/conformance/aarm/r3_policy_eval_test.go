package aarmconformance_test

import (
	"context"
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
	"github.com/safedep/gryph/aarm/model"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR3_ForbiddenActionsBlocked(t *testing.T) {
	aarm.Requires(t, aarm.R3, aarm.MUST, "Policy denies forbidden actions")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_destructive")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.Contains(t, res.MatchedRuleIDs, "r1-block-rm-rf")
}

func TestR3_ContextDependentEvaluation(t *testing.T) {
	aarm.Requires(t, aarm.R3, aarm.MUST, "Policy evaluation uses session context")

	// guidance-on-large-edit: same action shape, but the condition keys on
	// action.params.lines_added. Drive PDP directly so we exercise the CEL
	// path with both a passing and failing payload.
	ref := aarm.NewReferenceMediator(t)
	small := loadActionFixture(t, "file_write_prod")
	small.Parameters.LinesAdded = 10
	small.HumanPrincipal = "test"
	d1 := mustEvaluate(t, ref, small, nil)
	assert.NotEqual(t, "guidance", string(d1.Decision), "small edit must not trigger large-edit guidance")

	large := loadActionFixture(t, "file_write_prod")
	large.HumanPrincipal = "test"
	d2 := mustEvaluate(t, ref, large, nil)
	assert.Contains(t, d2.MatchedRuleIDs, "r4-guidance-large-edit",
		"large edit must trigger guidance rule when lines_added > 100")
}

func TestR3_DeferOnInsufficientContext(t *testing.T) {
	aarm.Requires(t, aarm.R3, aarm.MUST, "Defer when context is insufficient for a confident decision")

	ref := aarm.NewReferenceMediator(t, aarm.WithPolicy(fixturePath(t, "policies", "defer_trigger")))
	action := loadActionFixture(t, "network_request_external")
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "defer", string(dec.Decision))
}

func TestR3_ConflictDefer(t *testing.T) {
	aarm.Requires(t, aarm.R3, aarm.MUST, "Synthetic defer on tier conflict")

	// Two block rules at the same precedence tier with differing severity
	// form a structural conflict. The PDP's ConflictTriggersDefer trigger
	// (Phase 5b) must collapse the matched set to a synthetic defer with
	// reason=conflicting_policies, rather than picking one rule's severity
	// arbitrarily. The bundle below is in-memory so this test stays
	// independent of the closed reference.yaml fixture.
	policy := []byte(`version: "1"
rules:
  - id: block-a
    action: block
    severity: high
    match:
      action_types: [file_read]
    message: block-a
  - id: block-b
    action: block
    severity: critical
    match:
      action_types: [file_read]
    message: block-b
`)
	ref := aarm.NewReferenceMediator(t, aarm.WithPolicyBody(policy))
	action := loadActionFixture(t, "file_read_safe")
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "defer", string(dec.Decision),
		"two block rules with different severity must collapse to synthetic defer")
	assert.Equal(t, "conflicting_policies", dec.DeferReason,
		"conflict-defer must surface the conflicting_policies reason")
	assert.ElementsMatch(t, []string{"block-a", "block-b"}, dec.MatchedRuleIDs,
		"synthetic conflict-defer must expose both contributing rule IDs")
}

func TestR3_ParameterValidation(t *testing.T) {
	aarm.Requires(t, aarm.R3, aarm.MUST, "Policy validates parameter shapes")

	ref := aarm.NewReferenceMediator(t)
	action := loadActionFixture(t, "tool_use_injection")
	action.InjectionScore = 0.9
	action.Type = model.ActionToolUse
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "escalate", string(dec.Decision),
		"action.injection_score CEL binding must drive escalate rule when > 0.5")
}

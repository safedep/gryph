package aarmconformance_test

import (
	"context"
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR1_DeniedActionsDoNotExecute(t *testing.T) {
	aarm.Requires(t, aarm.R1, aarm.MUST, "Denied actions do not execute")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_destructive")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision, "rm -rf must be blocked")
	assert.NotEmpty(t, res.MatchedRuleIDs)
	assert.Contains(t, res.Reason, "destructive")
}

func TestR1_DeferredActionsDoNotExecute(t *testing.T) {
	aarm.Requires(t, aarm.R1, aarm.MUST, "Deferred actions do not execute")

	ref := aarm.NewReferenceMediator(t, aarm.WithPolicy(fixturePath(t, "policies", "defer_trigger")))
	action := loadActionFixture(t, "network_request_external")

	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "defer", string(dec.Decision), "PDP must defer on cold-session network request")
}

func TestR1_NoBypassMode(t *testing.T) {
	aarm.Requires(t, aarm.R1, aarm.MUST, "No bypass mode in production")
	// The Mediator and security.Evaluator do not expose a "disable" knob.
	// Configuration drives whether the check is registered (cli/root.go); once
	// registered, it cannot be bypassed at evaluation time. The fail_mode
	// option only controls behavior on internal engine errors, not normal
	// rule-driven blocks. A purely structural assertion: the Mediator's
	// Check method has no skip parameter and produces a decision for every
	// invocation.
	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_destructive")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	require.NotNil(t, res, "mediator must return a decision; there is no bypass path")
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
}

func TestR1_DecisionsRecordedWithPolicyAndReason(t *testing.T) {
	aarm.Requires(t, aarm.R1, aarm.MUST, "Decisions recorded with matched policy + reason")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_destructive")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.NotEmpty(t, res.MatchedRuleIDs, "block decision must carry a matched rule id")
	assert.NotEmpty(t, res.Reason, "block decision must carry an operator-facing reason")
}

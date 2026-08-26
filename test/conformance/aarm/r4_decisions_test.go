package aarmconformance_test

import (
	"context"
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR4_AllowDecision(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "ALLOW decision permits action")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_safe")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision,
		"benign command must allow; warn from role-scope rule is mapped to allow at the PEP boundary")
}

func TestR4_DenyDecision(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "DENY decision blocks action (Gryph: block)")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_destructive")
	res, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
}

func TestR4_ModifyDecision(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "MODIFY decision rewrites the action before execution")
	aarm.Skip(t, aarm.NotImplemented, "MODIFY decision pending")
}

func TestR4_StepUpDecision(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "STEP_UP decision routes through approval (Gryph: escalate)")

	ref := aarm.NewReferenceMediator(t)
	action := loadActionFixture(t, "tool_use_injection")
	action.InjectionScore = 0.9
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "escalate", string(dec.Decision),
		"PDP must surface escalate; the Mediator routes it through the Approval Service to either allow or block based on operator decision")
}

func TestR4_DeferDecision(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "DEFER decision pauses pending operator resolution")

	ref := aarm.NewReferenceMediator(t, aarm.WithPolicy(fixturePath(t, "policies", "defer_trigger")))
	action := loadActionFixture(t, "network_request_external")
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "defer", string(dec.Decision))
}

func TestR4_StepUpDetailsRecorded(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "STEP_UP details (approver identity, decision, timestamp) recorded on receipt")
	aarm.Skip(t, aarm.NotImplemented,
		"approver-identity assertion requires end-to-end approval driver wired into the suite; placeholder for the production approval audit path")
}

func TestR4_DeferDetailsRecorded(t *testing.T) {
	aarm.Requires(t, aarm.R4, aarm.MUST, "DEFER details (reason, resolution method, resolution timestamp) recorded on receipt")

	ref := aarm.NewReferenceMediator(t, aarm.WithPolicy(fixturePath(t, "policies", "defer_trigger")))
	action := loadActionFixture(t, "network_request_external")
	dec := mustEvaluate(t, ref, action, nil)
	require.Equal(t, "defer", string(dec.Decision))
	assert.NotEmpty(t, dec.DeferReason, "defer decision must carry a DeferReason that is persisted on the receipt")
}

package accumulator_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const manyCommandsPolicy = `
version: "1"
rules:
  - id: warn-many-commands
    action: warn
    severity: medium
    match:
      action_types: [command_exec]
    condition: "context.commands_executed >= 3"
    message: "session has run too many commands"
`

// pipeline runs events through the decision service with the Mediator as
// the policy check, as a hook does. The decision service owns the session
// counters, and the accumulator reads them.
func pipeline(t *testing.T, store storage.Store) decision.Service {
	t.Helper()
	policy, err := pdp.ParsePolicy([]byte(manyCommandsPolicy))
	require.NoError(t, err)
	med, err := aarmsec.NewMediator(policy, aarmsec.WithAccumulator(accumulator.NewSQLite(store)))
	require.NoError(t, err)
	evaluator := coresecurity.New(&coresecurity.Config{FailOpen: false})
	evaluator.RegisterCheck(med)
	full := func(string) config.LoggingLevel { return config.LoggingFull }
	return decision.NewLocal(store, evaluator, nil, full, decision.WithHookSpecs(testHookSpecs))
}

func testHookSpecs(_ string, hook events.HookType) (events.HookSpec, bool) {
	switch hook {
	case "PreToolUse":
		return events.HookSpec{Type: hook, Phase: events.PhasePre, Blocking: true}, true
	case "PostToolUse":
		return events.HookSpec{Type: hook, Phase: events.PhasePost}, true
	}
	return events.HookSpec{}, false
}

func bash(t *testing.T, sessionID uuid.UUID, hook events.HookType, callID string) *decision.HookRequest {
	t.Helper()
	e := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
	e.ToolName = "Bash"
	e.HookType = hook
	e.ToolCallID = callID
	require.NoError(t, e.SetPayload(events.CommandExecPayload{Command: privacy.NewText("ls")}))
	return decision.NewHookRequest(e)
}

func TestPipeline_CommandsExecutedThreshold(t *testing.T) {
	store := storagetest.NewStore(t)
	svc := pipeline(t, store)
	ctx := context.Background()
	sessionID := uuid.New()

	for i := range 2 {
		resp, err := svc.Handle(ctx, bash(t, sessionID, "PreToolUse", uuid.NewString()))
		require.NoError(t, err)
		assert.Equal(t, decision.VerdictOf(coresecurity.DecisionAllow), resp.Decision, "command %d must not trip the threshold", i+1)
	}
	resp, err := svc.Handle(ctx, bash(t, sessionID, "PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	assert.Equal(t, decision.VerdictOf(coresecurity.DecisionGuidance), resp.Decision, "the third command trips commands_executed >= 3")

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 3, state.CommandsExecuted)
	assert.Equal(t, 3, state.TotalActions)
}

func TestPipeline_PrePostPairCountsOnce(t *testing.T) {
	store := storagetest.NewStore(t)
	svc := pipeline(t, store)
	ctx := context.Background()
	sessionID := uuid.New()

	for range 3 {
		call := uuid.NewString()
		_, err := svc.Handle(ctx, bash(t, sessionID, "PreToolUse", call))
		require.NoError(t, err)
		_, err = svc.Handle(ctx, bash(t, sessionID, "PostToolUse", call))
		require.NoError(t, err)
	}

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, 3, state.CommandsExecuted)
	assert.Equal(t, 3, state.TotalActions)

	rows, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sessionID, Limit: -1, Ascending: true})
	require.NoError(t, err)
	require.Len(t, rows, 6)
	for i, r := range rows {
		want := "action"
		if i%2 == 1 {
			want = "observation"
		}
		assert.Equal(t, want, r.Kind, "entry %d", i+1)
	}
	assert.Equal(t, []string{"Bash"}, state.ToolsUsed)
}

func TestPipeline_DistinctSessionsIsolated(t *testing.T) {
	store := storagetest.NewStore(t)
	svc := pipeline(t, store)
	ctx := context.Background()

	sessA := uuid.New()
	for range 5 {
		_, err := svc.Handle(ctx, bash(t, sessA, "PreToolUse", uuid.NewString()))
		require.NoError(t, err)
	}
	resp, err := svc.Handle(ctx, bash(t, uuid.New(), "PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	assert.Equal(t, decision.VerdictOf(coresecurity.DecisionAllow), resp.Decision, "a new session must not inherit the counters of another session")
}

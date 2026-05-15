package accumulator_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediator_WithSQLiteAccumulator_CommandsExecutedThreshold(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: warn-many-commands
    action: warn
    severity: medium
    match:
      action_types: [command_exec]
    condition: "context.commands_executed >= 3"
    message: "session has run too many commands"
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy, aarmsec.WithAccumulator(accumulator.NewSQLite(store)))
	require.NoError(t, err)

	sessID := uuid.New()
	makeEvent := func() *events.Event {
		return &events.Event{
			ID:         uuid.New(),
			SessionID:  sessID,
			Timestamp:  time.Now().UTC(),
			ActionType: events.ActionCommandExec,
			AgentName:  "claude-code",
			ToolName:   "Bash",
			Payload:    []byte(`{"command":"ls"}`),
		}
	}

	ctx := context.Background()

	for i := 0; i < 2; i++ {
		res, err := med.Check(ctx, makeEvent())
		require.NoError(t, err)
		assert.Equal(t, coresecurity.DecisionAllow, res.Decision,
			"first %d commands must not trip the >=3 threshold", i+1)
	}

	res, err := med.Check(ctx, makeEvent())
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionGuidance, res.Decision,
		"third command_exec must trip context.commands_executed >= 3")

	res, err = med.Check(ctx, makeEvent())
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionGuidance, res.Decision)

	state, err := store.GetContextState(ctx, sessID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 4, state.CommandsExecuted)
	assert.Equal(t, 4, state.TotalActions)
}

func TestMediator_WithSQLiteAccumulator_DistinctSessionsIsolated(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: warn-many-commands
    action: warn
    match:
      action_types: [command_exec]
    condition: "context.commands_executed >= 3"
    message: "many commands"
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy, aarmsec.WithAccumulator(accumulator.NewSQLite(store)))
	require.NoError(t, err)

	sessA := uuid.New()
	sessB := uuid.New()
	for i := 0; i < 5; i++ {
		_, err := med.Check(context.Background(), &events.Event{
			ID:         uuid.New(),
			SessionID:  sessA,
			Timestamp:  time.Now().UTC(),
			ActionType: events.ActionCommandExec,
			AgentName:  "claude-code",
			ToolName:   "Bash",
			Payload:    []byte(`{"command":"ls"}`),
		})
		require.NoError(t, err)
	}

	res, err := med.Check(context.Background(), &events.Event{
		ID:         uuid.New(),
		SessionID:  sessB,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionCommandExec,
		AgentName:  "claude-code",
		ToolName:   "Bash",
		Payload:    []byte(`{"command":"ls"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision,
		"new session must not inherit other session's counters")
}

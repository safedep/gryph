package aarm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediator_UsesSessionFromContext(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: block-on-project
    action: block
    severity: high
    scope:
      projects: [payments]
    match:
      action_types: [file_write]
    message: "blocked write on {{.Action.Project}}"
`))
	require.NoError(t, err)

	med, err := NewMediator(policy)
	require.NoError(t, err)

	sessID := uuid.New()
	sess := &session.Session{
		ID:               sessID,
		AgentName:        "claude-code",
		AgentSessionID:   "agent-1",
		WorkingDirectory: "/work/payments",
		ProjectName:      "payments",
	}

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  sessID,
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/work/payments/app.go"}`),
	}

	t.Run("without session on ctx, scope misses", func(t *testing.T) {
		res, err := med.Check(context.Background(), event)
		require.NoError(t, err)
		assert.Equal(t, coresecurity.DecisionAllow, res.Decision)
	})

	t.Run("with session on ctx, project scope matches and blocks", func(t *testing.T) {
		ctx := session.WithSession(context.Background(), sess)
		res, err := med.Check(ctx, event)
		require.NoError(t, err)
		assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
		assert.Contains(t, res.Reason, "payments")
		assert.Equal(t, []string{"block-on-project"}, res.MatchedRuleIDs)
	})
}

type spyAccumulator struct {
	appendCalls   int
	snapshotCalls int
	lastAction    *model.Action
	lastSessionID uuid.UUID
	snapshot      *model.ContextSnapshot
}

func (s *spyAccumulator) Append(_ context.Context, a *model.Action) error {
	s.appendCalls++
	s.lastAction = a
	return nil
}

func (s *spyAccumulator) RecordResult(context.Context, uuid.UUID, model.Result) error {
	return nil
}

func (s *spyAccumulator) Snapshot(_ context.Context, id uuid.UUID) (*model.ContextSnapshot, error) {
	s.snapshotCalls++
	s.lastSessionID = id
	if s.snapshot != nil {
		return s.snapshot, nil
	}
	return &model.ContextSnapshot{}, nil
}

func TestMediator_InvokesAccumulator(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: guide-many-writes
    action: guidance
    match: { action_types: [file_write] }
    condition: "context.files_written >= 10"
    message: "high write volume"
`))
	require.NoError(t, err)

	spy := &spyAccumulator{snapshot: &model.ContextSnapshot{FilesWritten: 15}}
	med, err := NewMediator(policy, WithAccumulator(spy))
	require.NoError(t, err)

	sessID := uuid.New()
	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  sessID,
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"main.go"}`),
	}

	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)

	assert.Equal(t, 1, spy.appendCalls)
	assert.Equal(t, 1, spy.snapshotCalls)
	require.NotNil(t, spy.lastAction)
	assert.Equal(t, sessID, spy.lastSessionID, "Snapshot must be queried by the action's session id")
	assert.Equal(t, coresecurity.DecisionGuidance, res.Decision,
		"PDP should observe the injected snapshot (files_written=15) and match the rule")
}

var _ accumulator.Accumulator = (*spyAccumulator)(nil)

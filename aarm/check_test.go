package aarm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
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

type fakeApprovalService struct {
	outcome *approval.Outcome
	calls   int
}

func (f *fakeApprovalService) Request(_ context.Context, _ *approval.Request) (*approval.Outcome, error) {
	f.calls++
	return f.outcome, nil
}

type spyReceiptGenerator struct {
	mu            sync.Mutex
	records       []*receipt.RecordInput
	decisionCalls []decisionCall
}

type decisionCall struct {
	sequence int64
	decision string
	status   string
	note     string
}

func (s *spyReceiptGenerator) Record(_ context.Context, in *receipt.RecordInput) (*receipt.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, in)
	return &receipt.Record{Sequence: int64(len(s.records))}, nil
}

func (s *spyReceiptGenerator) UpdateResult(_ context.Context, _ uuid.UUID, _ int64, _ model.Result) error {
	return nil
}

func (s *spyReceiptGenerator) UpdateDecision(_ context.Context, _ uuid.UUID, sequence int64, decision string, status string, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisionCalls = append(s.decisionCalls, decisionCall{sequence: sequence, decision: decision, status: status, note: note})
	return nil
}

func TestMediator_EscalateRoutesToApprovalApprove(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: escalate-write
    action: escalate
    match: { action_types: [file_write] }
    message: "needs review"
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	med, err := NewMediator(policy,
		WithReceiptGenerator(rec),
		WithApprovalService(&fakeApprovalService{outcome: &approval.Outcome{
			Decision: approval.DecisionApprove,
			Approver: "alice",
			Note:     "explicit override",
		}}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/etc/hosts"}`),
	}
	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision)
	assert.Contains(t, res.Guidance, "alice")
	require.Len(t, rec.records, 1)
	assert.Equal(t, string(model.DecisionEscalate), string(rec.records[0].Decision.Decision))
	require.Len(t, rec.decisionCalls, 1)
	assert.Equal(t, receipt.DecisionApproved, rec.decisionCalls[0].decision)
	assert.Equal(t, string(model.ResultSuccess), rec.decisionCalls[0].status)
}

func TestMediator_EscalateRoutesToApprovalDeny(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: escalate-write
    action: escalate
    match: { action_types: [file_write] }
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	med, err := NewMediator(policy,
		WithReceiptGenerator(rec),
		WithApprovalService(&fakeApprovalService{outcome: &approval.Outcome{
			Decision: approval.DecisionDeny,
			Approver: "alice",
			Note:     "nope",
		}}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/etc/hosts"}`),
	}
	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.Contains(t, res.Reason, "nope")
	require.Len(t, rec.decisionCalls, 1)
	assert.Equal(t, receipt.DecisionDenied, rec.decisionCalls[0].decision)
	assert.Equal(t, string(model.ResultRejected), rec.decisionCalls[0].status)
}

func TestMediator_EscalateRoutesToApprovalTimeout(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: escalate-write
    action: escalate
    match: { action_types: [file_write] }
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	med, err := NewMediator(policy,
		WithReceiptGenerator(rec),
		WithApprovalService(&fakeApprovalService{outcome: &approval.Outcome{
			Decision: approval.DecisionTimeout,
			Approver: "system",
		}}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/etc/hosts"}`),
	}
	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	require.Len(t, rec.decisionCalls, 1)
	assert.Equal(t, receipt.DecisionApprovalTimeout, rec.decisionCalls[0].decision)
	assert.Equal(t, string(model.ResultBlocked), rec.decisionCalls[0].status)
}

func TestMediator_DeferRecordsReceiptAndInvokesHook(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: defer-on-classify
    action: defer
    reason: wait_for_classification
    match: { action_types: [file_write] }
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	hookCalls := 0
	var capturedReason string
	var capturedReceiptSeq int64
	med, err := NewMediator(policy,
		WithReceiptGenerator(rec),
		WithDeferralConfig(DeferralConfig{Enabled: true, TimeoutSeconds: 600}),
		WithDeferralHook(func(_ context.Context, r DeferralRecord) (uuid.UUID, string, error) {
			hookCalls++
			capturedReason = r.Reason
			capturedReceiptSeq = r.ReceiptSequence
			return uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				"Resolve with `gryph policy deferrals resolve --id aaaaaaaa`.",
				nil
		}),
	)
	require.NoError(t, err)
	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/work/main.go"}`),
	}
	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.Contains(t, res.Reason, "Action deferred:")
	assert.Contains(t, res.Reason, "wait_for_classification")
	assert.Contains(t, res.Reason, "aaaaaaaa")
	require.Len(t, rec.records, 1)
	assert.Equal(t, string(model.DecisionDefer), string(rec.records[0].Decision.Decision))
	assert.Equal(t, "wait_for_classification", rec.records[0].DeferReason)
	assert.Equal(t, 1, hookCalls)
	assert.Equal(t, "wait_for_classification", capturedReason)
	assert.Equal(t, int64(1), capturedReceiptSeq)
}

func TestMediator_EscalateDefaultDenies(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: escalate-write
    action: escalate
    match: { action_types: [file_write] }
`))
	require.NoError(t, err)

	med, err := NewMediator(policy)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/etc/hosts"}`),
	}
	res, err := med.Check(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision, "Nop approval service denies by default")
}

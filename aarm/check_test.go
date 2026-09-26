package aarm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/classify"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediator_UsesSessionArgument(t *testing.T) {
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

	t.Run("without session, scope misses", func(t *testing.T) {
		res, err := med.Check(context.Background(), event, nil)
		require.NoError(t, err)
		assert.Equal(t, coresecurity.DecisionAllow, res.Decision)
	})

	t.Run("with session, project scope matches and blocks", func(t *testing.T) {
		res, err := med.Check(context.Background(), event, sess)
		require.NoError(t, err)
		assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
		assert.Contains(t, res.Reason, "payments")
		assert.Equal(t, []string{"block-on-project"}, res.MatchedRuleIDs)
	})
}

type spyAccumulator struct {
	appendErr         error
	snapshotErr       error
	appendCalls       int
	snapshotCalls     int
	recordResultCalls int
	lastEntry         *model.ContextEntry
	lastPending       *model.ContextEntry
	lastSessionID     uuid.UUID
	lastResult        model.Result
	snapshot          *model.ContextSnapshot
}

func (s *spyAccumulator) Append(_ context.Context, e *model.ContextEntry) error {
	s.appendCalls++
	s.lastEntry = e
	return s.appendErr
}

func (s *spyAccumulator) RecordResult(_ context.Context, _ uuid.UUID, r model.Result) error {
	s.recordResultCalls++
	s.lastResult = r
	return nil
}

func (s *spyAccumulator) Snapshot(_ context.Context, id uuid.UUID, pending *model.ContextEntry) (*model.ContextSnapshot, error) {
	s.snapshotCalls++
	s.lastSessionID = id
	s.lastPending = pending
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
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

	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, spy.appendCalls)
	assert.Equal(t, 1, spy.snapshotCalls)
	require.NotNil(t, spy.lastEntry)
	assert.Equal(t, sessID, spy.lastSessionID, "Snapshot must be queried by the action's session id")
	assert.Equal(t, coresecurity.DecisionGuidance, res.Decision,
		"PDP should observe the injected snapshot (files_written=15) and match the rule")
}

func TestMediator_BlockRecordsTerminalResult(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: block-writes
    action: block
    match: { action_types: [file_write] }
    message: "no writes"
`))
	require.NoError(t, err)

	spy := &spyAccumulator{}
	med, err := NewMediator(policy, WithAccumulator(spy))
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"main.go"}`),
	}

	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	require.Equal(t, 1, spy.appendCalls)
	assert.Equal(t, model.ResultBlocked, spy.lastEntry.Result, "a blocked entry is written with its terminal result")
	assert.Zero(t, spy.recordResultCalls, "the entry is written once")
}

func TestMediator_AppendErrorFailMode(t *testing.T) {
	cases := []struct {
		name     string
		action   string
		approval approval.Decision
		wantErr  bool
	}{
		{name: "allow", action: "allow", wantErr: true},
		{name: "warn", action: "warn", wantErr: true},
		{name: "approved escalate", action: "escalate", approval: approval.DecisionApprove, wantErr: true},
		{name: "block", action: "block"},
		{name: "denied escalate", action: "escalate", approval: approval.DecisionDeny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: writes
    action: ` + tc.action + `
    match: { action_types: [file_write] }
    message: "writes"
`))
			require.NoError(t, err)

			spy := &spyAccumulator{appendErr: errors.New("database is locked")}
			rec := &spyReceiptGenerator{}
			med, err := NewMediator(policy,
				WithAccumulator(spy),
				WithReceiptGenerator(rec),
				WithMediatorConfig(MediatorConfig{LogAllEvaluations: true}),
				WithApprovalService(&fakeApprovalService{outcome: &approval.Outcome{
					Decision: tc.approval, Approver: "tester", DecidedAt: time.Now().UTC(),
				}}),
			)
			require.NoError(t, err)

			event := &events.Event{
				ID:         uuid.New(),
				SessionID:  uuid.New(),
				Timestamp:  time.Now(),
				ActionType: events.ActionFileWrite,
				AgentName:  "claude-code",
				Payload:    []byte(`{"path":"main.go"}`),
			}

			res, err := med.Check(context.Background(), event, nil)
			require.Equal(t, 1, spy.appendCalls)
			if !tc.wantErr {
				require.NoError(t, err, "a failed append must not fail a check that stops the action")
				assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
				assert.Len(t, rec.records, 1, "a block keeps its receipt")
				return
			}
			require.ErrorIs(t, err, accumulator.ErrAppend)
			if tc.action != "escalate" {
				assert.Empty(t, rec.records, "no receipt records an action that the fail mode can still block")
			}

			for _, failOpen := range []bool{false, true} {
				evaluator := coresecurity.New(&coresecurity.Config{FailOpen: failOpen})
				evaluator.RegisterCheck(med)
				event.ID = uuid.New()
				want := coresecurity.DecisionBlock
				if failOpen {
					want = coresecurity.DecisionAllow
				}
				assert.Equal(t, want, evaluator.Evaluate(context.Background(), event, nil).FinalDecision,
					"fail_open=%v decides an action that runs without its entry", failOpen)
			}
		})
	}
}

func TestMediator_FailedDecisionAppendsEntry(t *testing.T) {
	cases := []struct {
		name        string
		condition   string
		snapshotErr error
		wantErr     error
	}{
		{
			name:        "snapshot error",
			condition:   "true",
			snapshotErr: errors.New("database is locked"),
			wantErr:     accumulator.ErrSnapshot,
		},
		{
			name:      "evaluation error",
			condition: "1 / (context.total_actions - context.total_actions) > 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: reads
    action: warn
    match: { action_types: [file_read] }
    condition: "` + tc.condition + `"
`))
			require.NoError(t, err)

			spy := &spyAccumulator{snapshotErr: tc.snapshotErr, appendErr: errors.New("database is locked")}
			adapter := mediation.NewHookAdapter(mediation.WithClassifier(classify.NewHeuristic()))
			med, err := NewMediator(policy, WithAccumulator(spy), WithAdapter(adapter))
			require.NoError(t, err)

			event := &events.Event{
				ID:         uuid.New(),
				SessionID:  uuid.New(),
				Timestamp:  time.Now(),
				ActionType: events.ActionFileRead,
				AgentName:  "claude-code",
				Payload:    []byte(`{"path":".env"}`),
			}

			_, err = med.Check(context.Background(), event, nil)
			require.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
			assert.ErrorIs(t, err, accumulator.ErrAppend, "the check error carries the failed append")
			require.Equal(t, 1, spy.appendCalls, "an action without a decision still gets a context entry")
			assert.Equal(t, model.ResultError, spy.lastEntry.Result)
			assert.Empty(t, spy.lastEntry.Decision)
			assert.Contains(t, spy.lastEntry.Classifications, privacy.ClassSecret)
		})
	}
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
	resultCalls   []resultCall
}

type decisionCall struct {
	sequence int64
	decision string
	status   string
	note     string
}

type resultCall struct {
	sessionID uuid.UUID
	sequence  int64
	result    model.Result
}

func (s *spyReceiptGenerator) Record(_ context.Context, in *receipt.RecordInput) (*receipt.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, in)
	return &receipt.Record{Sequence: int64(len(s.records))}, nil
}

func (s *spyReceiptGenerator) UpdateResult(_ context.Context, sessionID uuid.UUID, sequence int64, result model.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resultCalls = append(s.resultCalls, resultCall{sessionID: sessionID, sequence: sequence, result: result})
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
	res, err := med.Check(context.Background(), event, nil)
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
	res, err := med.Check(context.Background(), event, nil)
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
	res, err := med.Check(context.Background(), event, nil)
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
	res, err := med.Check(context.Background(), event, nil)
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
	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision, "Nop approval service denies by default")
}

func TestMediator_RequireHumanPrincipalBlocksWhenEmpty(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules: []
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	accum := &spyAccumulator{}
	adapter := mediation.NewHookAdapter(
		mediation.WithIdentityCapturer(identity.NewStaticCapturer(identity.Capture{})),
	)
	var auditCalls int
	med, err := NewMediator(policy,
		WithAdapter(adapter),
		WithAccumulator(accum),
		WithReceiptGenerator(rec),
		WithIdentityConfig(IdentityConfig{Enabled: true, RequireHumanPrincipal: true}),
		WithIdentityAuditHook(func(_ context.Context, _ IdentityAudit) {
			auditCalls++
		}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/tmp/x"}`),
	}

	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.Contains(t, res.Reason, "no verifiable human principal")
	assert.Equal(t, 1, auditCalls, "identity audit hook must fire once")
	assert.Equal(t, 1, accum.appendCalls, "a denied action is an attempt, so it gets a context entry")
	require.NotNil(t, accum.lastEntry)
	assert.Equal(t, model.DecisionBlock, accum.lastEntry.Decision)
	assert.Equal(t, model.ResultBlocked, accum.lastEntry.Result)
	assert.Equal(t, 0, accum.snapshotCalls, "denied action must not query the accumulator")
	require.Len(t, rec.records, 1, "block must still produce a receipt")
	assert.Equal(t, model.DecisionBlock, rec.records[0].Decision.Decision)
	assert.Equal(t, identityMissingReason, rec.records[0].ErrorMessage,
		"ErrorMessage must be folded into the initial insert so a second UpdateResult round trip is unnecessary")
	assert.Empty(t, rec.resultCalls, "block path must not invoke UpdateResult: error_message rides on the initial insert")
}

func TestMediator_RequireHumanPrincipalAllowsWhenPopulated(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules: []
`))
	require.NoError(t, err)

	rec := &spyReceiptGenerator{}
	adapter := mediation.NewHookAdapter(
		mediation.WithIdentityCapturer(identity.NewStaticCapturer(identity.Capture{
			HumanPrincipal: "alice@example.com",
		})),
	)
	med, err := NewMediator(policy,
		WithAdapter(adapter),
		WithReceiptGenerator(rec),
		WithIdentityConfig(IdentityConfig{Enabled: true, RequireHumanPrincipal: true}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/tmp/x"}`),
	}

	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision)
}

func TestMediator_IdentityDisabledSkipsEnforcement(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules: []
`))
	require.NoError(t, err)

	adapter := mediation.NewHookAdapter(
		mediation.WithIdentityCapturer(identity.NewStaticCapturer(identity.Capture{})),
	)
	med, err := NewMediator(policy,
		WithAdapter(adapter),
		WithIdentityConfig(IdentityConfig{Enabled: false, RequireHumanPrincipal: true}),
	)
	require.NoError(t, err)

	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  uuid.New(),
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/tmp/x"}`),
	}

	res, err := med.Check(context.Background(), event, nil)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision,
		"require_human_principal is a silent no-op when identity.enabled=false")
}

func TestMediator_FreshSessionDeferUsesSessionArgument(t *testing.T) {
	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: block-on-many-writes
    action: block
    match:
      action_types: [file_write]
    condition: "context.files_written > 5"
    message: blocked
`))
	require.NoError(t, err)

	med, err := NewMediator(policy, WithDeferralConfig(DeferralConfig{
		Enabled: true, FreshSessionSeconds: 60, TimeoutSeconds: 600,
	}))
	require.NoError(t, err)

	newEvent := func() *events.Event {
		return &events.Event{
			ID:         uuid.New(),
			SessionID:  uuid.New(),
			Timestamp:  time.Now(),
			ActionType: events.ActionFileWrite,
			AgentName:  "claude-code",
			Payload:    []byte(`{"path":"/work/main.go"}`),
		}
	}

	cases := []struct {
		name       string
		sess       *session.Session
		want       coresecurity.Decision
		wantReason string
	}{
		{name: "fresh session defers", sess: &session.Session{StartedAt: time.Now().UTC()}, want: coresecurity.DecisionBlock,
			wantReason: "Action deferred: " + pdp.DeferReasonFreshSession},
		{name: "old session evaluates the rule", sess: &session.Session{StartedAt: time.Now().UTC().Add(-time.Hour)}, want: coresecurity.DecisionAllow},
		{name: "no session evaluates the rule", want: coresecurity.DecisionAllow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := med.Check(context.Background(), newEvent(), tc.sess)
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
			assert.Contains(t, res.Reason, tc.wantReason)
		})
	}
}

func TestMediator_ReceiptDropsStrippedParameters(t *testing.T) {
	const rawURL = "https://example.com/invite/k7Qz9xWm"
	cases := []struct {
		name  string
		rule  string
		strip bool
	}{
		{"allow keeps", "allow", false},
		{"allow strips", "allow", true},
		{"block keeps", "block", false},
		{"block strips", "block", true},
		{"escalate strips", "escalate", true},
		{"defer strips", "defer\n    reason: wait", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: rule
    action: ` + tc.rule + `
    match: { action_types: [tool_use] }
    message: "fetch {{.Action.Params.URL}}"
`))
			require.NoError(t, err)

			rec := &spyReceiptGenerator{}
			var seen *events.Event
			med, err := NewMediator(policy,
				WithReceiptGenerator(rec),
				WithMediatorConfig(MediatorConfig{LogAllEvaluations: true}),
				WithDeferralConfig(DeferralConfig{Enabled: true}),
				WithContentStrip(func(e *events.Event) bool {
					seen = e
					return tc.strip
				}),
			)
			require.NoError(t, err)

			event := &events.Event{
				ID:         uuid.New(),
				SessionID:  uuid.New(),
				Timestamp:  time.Now(),
				ActionType: events.ActionToolUse,
				ToolName:   "WebFetch",
				AgentName:  "claude-code",
				Payload:    []byte(`{"tool_name":"WebFetch","input":{"value":"{\"url\":\"` + rawURL + `\"}","label":{}}}`),
			}
			res, err := med.Check(context.Background(), event, nil)
			require.NoError(t, err)
			assert.Same(t, event, seen)
			require.Len(t, rec.records, 1)
			if tc.rule == "block" {
				assert.Equal(t, "fetch "+rawURL, res.Reason)
			}
			if tc.strip {
				assert.Empty(t, rec.records[0].Action.Parameters.URL)
				assert.NotContains(t, rec.records[0].Decision.Message, rawURL)
				assert.NotContains(t, res.StoredReason, rawURL)
				return
			}
			assert.Equal(t, rawURL, rec.records[0].Action.Parameters.URL)
			if tc.rule == "block" {
				assert.Equal(t, "fetch "+rawURL, rec.records[0].Decision.Message)
				assert.Equal(t, "fetch "+rawURL, res.StoredReason)
			}
		})
	}
}

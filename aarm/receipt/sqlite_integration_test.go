package receipt_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeApprovalService struct {
	outcome *approval.Outcome
}

func (f *fakeApprovalService) Request(_ context.Context, _ *approval.Request) (*approval.Outcome, error) {
	return f.outcome, nil
}

func TestMediator_EscalateRecordsApprovedDecision_HashStable(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: escalate-root
    action: escalate
    match:
      action_types: [file_write]
      file_patterns: ["/etc/**"]
    message: "needs approval"
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy,
		aarmsec.WithAccumulator(accumulator.NewSQLite(store)),
		aarmsec.WithReceiptGenerator(receipt.NewSQLite(store)),
		aarmsec.WithApprovalService(&fakeApprovalService{outcome: &approval.Outcome{
			Decision: approval.DecisionApprove,
			Approver: "alice",
			Note:     "explicit",
		}}),
	)
	require.NoError(t, err)

	sessionID := uuid.New()
	res, err := med.Check(context.Background(), &events.Event{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		ToolName:   "Write",
		Payload:    []byte(`{"path":"/etc/hosts"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionAllow, res.Decision)

	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, receipt.DecisionApproved, rows[0].Decision)

	policyHash := policy.Hash()
	require.NotEmpty(t, policyHash, "test setup: policy hash must be non-empty")
	require.NotEmpty(t, rows[0].PolicyHash, "escalate path must persist a non-empty policy_hash")
	assert.True(t, bytes.Equal(policyHash, rows[0].PolicyHash),
		"persisted policy_hash must equal policy.Hash()")

	chainRow := receipt.ChainRowFromReceipt(rows[0])
	rehashed, err := receipt.ComputeHash(receipt.NewHashInput(chainRow.Fields))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(rehashed, rows[0].Hash),
		"hash recomputed via ChainRowFromReceipt must match stored hash even after decision mutation")
}

func TestMediator_BlockProducesReceipt(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: block-payments
    action: block
    severity: high
    match:
      action_types: [file_write]
    message: "no writes allowed"
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy,
		aarmsec.WithAccumulator(accumulator.NewSQLite(store)),
		aarmsec.WithReceiptGenerator(receipt.NewSQLite(store)),
	)
	require.NoError(t, err)

	sessionID := uuid.New()
	evt := &events.Event{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		ToolName:   "Write",
		Payload:    []byte(`{"path":"/work/app.go"}`),
	}

	res, err := med.Check(context.Background(), evt)
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionBlock, res.Decision)
	assert.Equal(t, sessionID, res.AarmSessionID)
	assert.Equal(t, int64(1), res.AarmSequence)

	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "block", rows[0].Decision)
	assert.Equal(t, "blocked", rows[0].ResultStatus)
	assert.NotEmpty(t, rows[0].Hash)
}

func TestMediator_AllowDecision_SkipsReceiptWhenLogAllEvalsFalse(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules: []
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy,
		aarmsec.WithAccumulator(accumulator.NewSQLite(store)),
		aarmsec.WithReceiptGenerator(receipt.NewSQLite(store)),
	)
	require.NoError(t, err)

	sessionID := uuid.New()
	_, err = med.Check(context.Background(), &events.Event{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionFileRead,
		AgentName:  "claude-code",
		ToolName:   "Read",
		Payload:    []byte(`{"path":"/work/x"}`),
	})
	require.NoError(t, err)

	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	assert.Empty(t, rows, "allow + LogAllEvaluations=false must not record a receipt")
}

func TestMediator_AllowDecision_RecordsReceiptWhenLogAllEvalsTrue(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules: []
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy,
		aarmsec.WithAccumulator(accumulator.NewSQLite(store)),
		aarmsec.WithReceiptGenerator(receipt.NewSQLite(store)),
		aarmsec.WithMediatorConfig(aarmsec.MediatorConfig{LogAllEvaluations: true}),
	)
	require.NoError(t, err)

	sessionID := uuid.New()
	_, err = med.Check(context.Background(), &events.Event{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionFileRead,
		AgentName:  "claude-code",
		ToolName:   "Read",
		Payload:    []byte(`{"path":"/work/x"}`),
	})
	require.NoError(t, err)

	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "allow", rows[0].Decision)
}

func TestMediator_RecordResult_FansOut(t *testing.T) {
	store := storagetest.NewStore(t)

	policy, err := pdp.ParsePolicy([]byte(`
version: "1"
rules:
  - id: guide-many
    action: guidance
    match:
      action_types: [file_read]
    message: "guidance"
`))
	require.NoError(t, err)

	med, err := aarmsec.NewMediator(policy,
		aarmsec.WithAccumulator(accumulator.NewSQLite(store)),
		aarmsec.WithReceiptGenerator(receipt.NewSQLite(store)),
	)
	require.NoError(t, err)

	sessionID := uuid.New()
	res, err := med.Check(context.Background(), &events.Event{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		ActionType: events.ActionFileRead,
		AgentName:  "claude-code",
		ToolName:   "Read",
		Payload:    []byte(`{"path":"/x"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, coresecurity.DecisionGuidance, res.Decision)
	require.NotEqual(t, uuid.Nil, res.AarmActionID)
	require.True(t, res.AarmSequence > 0)

	err = med.RecordResult(context.Background(), res.AarmActionID, res.AarmSessionID, res.AarmSequence, model.Result{
		Status: model.ResultSuccess,
	})
	require.NoError(t, err)

	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "success", rows[0].ResultStatus)
}

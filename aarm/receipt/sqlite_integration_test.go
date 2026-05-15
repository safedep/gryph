package receipt_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	aarmsec "github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/aarm/accumulator"
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

func TestMediator_AllowDecision_SkipsReceiptByDefault(t *testing.T) {
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

func TestMediator_AllowDecision_LogAllEvaluationsTrue(t *testing.T) {
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

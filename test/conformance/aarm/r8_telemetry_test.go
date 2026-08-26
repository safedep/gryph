package aarmconformance_test

import (
	"context"
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR8_BatchExport(t *testing.T) {
	aarm.Requires(t, aarm.R8, aarm.SHOULD, "Telemetry: batch export of receipts")

	ref := aarm.NewReferenceMediator(t)
	first := loadEventFixture(t, "command_exec_safe")
	_, err := ref.Mediator.Check(context.Background(), first)
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		next := loadEventFixture(t, "command_exec_safe")
		next.SessionID = first.SessionID
		_, err := ref.Mediator.Check(context.Background(), next)
		require.NoError(t, err)
	}
	rows, err := ref.Store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &first.SessionID, Limit: -1})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(rows), 5, "batch query must return every receipt for the session")
}

func TestR8_FilterByDecision(t *testing.T) {
	aarm.Requires(t, aarm.R8, aarm.SHOULD, "Telemetry: filtering by decision")

	ref := aarm.NewReferenceMediator(t)
	safe := loadEventFixture(t, "command_exec_safe")
	_, err := ref.Mediator.Check(context.Background(), safe)
	require.NoError(t, err)
	bad := loadEventFixture(t, "command_exec_destructive")
	bad.SessionID = safe.SessionID
	_, err = ref.Mediator.Check(context.Background(), bad)
	require.NoError(t, err)

	rows, err := ref.Store.QueryReceipts(context.Background(), &storage.ReceiptFilter{
		SessionID: &safe.SessionID,
		Decision:  "block",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, rows, "decision filter must return matching rows")
	for _, r := range rows {
		assert.Equal(t, "block", r.Decision)
	}
}

func TestR8_StreamingExport(t *testing.T) {
	aarm.Requires(t, aarm.R8, aarm.SHOULD, "Telemetry: streaming export")
	aarm.Skip(t, aarm.Deferred,
		"receipt streaming (push-mode export to a SIEM connector) is on the roadmap but not wired yet; batch export covers the SHOULD")
}

func TestR8_SchemaDocumented(t *testing.T) {
	aarm.Requires(t, aarm.R8, aarm.SHOULD, "Telemetry: receipt schema documented")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "command_exec_safe")
	_, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)
	rows := mustReceipts(t, ref, ev.SessionID)
	require.NotEmpty(t, rows)
	r := rows[0]
	assert.NotZero(t, r.Sequence)
	assert.NotEmpty(t, r.ActionType)
	assert.NotEmpty(t, r.Agent)
	assert.NotEmpty(t, r.Decision)
}

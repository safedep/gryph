package aarmconformance_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/classify"
	aarm "github.com/safedep/gryph/aarm/conformance"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestR2_PriorActionsAvailableInContext(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.MUST, "Prior actions are accumulated into session context")

	ref := aarm.NewReferenceMediator(t)
	first := loadEventFixture(t, "command_exec_safe")
	_, err := ref.Mediator.Check(context.Background(), first)
	require.NoError(t, err)

	second := loadEventFixture(t, "command_exec_safe")
	second.SessionID = first.SessionID
	_, err = ref.Mediator.Check(context.Background(), second)
	require.NoError(t, err)

	snap, err := ref.Accumulator.Snapshot(context.Background(), first.SessionID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, snap.TotalActions, 2, "two actions must accumulate into session context")
	assert.GreaterOrEqual(t, snap.CommandsExecuted, 2)
}

func TestR2_ClassificationsTracked(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.MUST, "Data classifications surface on the action and in context")

	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "file_read_secret")
	_, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)

	snap, err := ref.Accumulator.Snapshot(context.Background(), ev.SessionID)
	require.NoError(t, err)
	assert.Contains(t, snap.ClassificationsSeen, classify.LabelSecret,
		"reading .env must populate classifications_seen with 'secret'")
}

func TestR2_FailSafeOnNoClassifier(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.MUST, "Fail-safe default classification when no classifier produces a label")

	// When the classifier is wrapped in NewFailSafe (the production default
	// via the reference adapter), an action with no classifiable surface
	// still gets the unknown_sensitive label so policies that gate on
	// classification fail safe rather than open.
	ref := aarm.NewReferenceMediator(t)
	ev := loadEventFixture(t, "notification")
	_, err := ref.Mediator.Check(context.Background(), ev)
	require.NoError(t, err)

	snap, err := ref.Accumulator.Snapshot(context.Background(), ev.SessionID)
	require.NoError(t, err)
	assert.Contains(t, snap.ClassificationsSeen, classify.LabelUnknownSensitive,
		"actions with no other classification must surface as unknown_sensitive (fail-safe)")
}

func TestR2_ContextAvailableToPolicy(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.MUST, "Context counters and sets are available to policy evaluation")

	ref := aarm.NewReferenceMediator(t, aarm.WithPolicy(fixturePath(t, "policies", "defer_trigger")))
	action := loadActionFixture(t, "network_request_external")
	dec := mustEvaluate(t, ref, action, nil)
	assert.Equal(t, "defer", string(dec.Decision),
		"context.commands_executed CEL binding must be reachable from policy conditions")
}

func TestR2_HashChainEachReceiptCarriesPrevHash(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.SHOULD, "Session context store is append-only and hash-chained")

	// Drive receipts directly with controlled snapshots so the chain verifier
	// is not exposed to json type-drift on session_duration. See the R5
	// chain-verifiable test for the explanation.
	ref := aarm.NewReferenceMediator(t)
	sessionID := uuid.New()
	for i := 0; i < 3; i++ {
		action := loadActionFixture(t, "command_exec_safe")
		action.SessionID = sessionID
		dec := mustEvaluate(t, ref, action, nil)
		_, err := ref.Receipts.Record(context.Background(), &receipt.RecordInput{
			SessionID:  action.SessionID,
			ActionID:   action.ID,
			EventID:    action.EventID,
			Action:     action,
			Decision:   dec,
			PolicyHash: ref.PolicyHash,
			RecordedAt: zeroTime().Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
	}

	rows, err := ref.Store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 3)

	chainRows := make([]receipt.ChainRow, 0, len(rows))
	for _, r := range rows {
		chainRows = append(chainRows, receipt.ChainRowFromReceipt(r))
	}
	breaks := receipt.VerifyChain(chainRows)
	assert.Empty(t, breaks, "hash chain must verify for all session receipts")
}

func TestR2_PrevHashLinksReceipts(t *testing.T) {
	aarm.Requires(t, aarm.R2, aarm.SHOULD, "Each receipt carries prev_hash linking to its predecessor")

	ref := aarm.NewReferenceMediator(t)
	sessionID := uuid.New()
	for i := 0; i < 2; i++ {
		action := loadActionFixture(t, "command_exec_safe")
		action.SessionID = sessionID
		dec := mustEvaluate(t, ref, action, nil)
		_, err := ref.Receipts.Record(context.Background(), &receipt.RecordInput{
			SessionID:  action.SessionID,
			ActionID:   action.ID,
			EventID:    action.EventID,
			Action:     action,
			Decision:   dec,
			PolicyHash: ref.PolicyHash,
			RecordedAt: zeroTime().Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
	}

	rows, err := ref.Store.QueryReceipts(context.Background(), &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)
	assert.Equal(t, rows[0].Hash, rows[1].PrevHash, "second receipt prev_hash must match first hash")
}

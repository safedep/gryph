package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDeferral(t *testing.T, store *storage.SQLiteStore, sessionID uuid.UUID, expiresAt time.Time) *storage.DeferredActionRow {
	t.Helper()
	ctx := context.Background()
	g := receipt.NewSQLite(store)
	rec, err := g.Record(ctx, &receipt.RecordInput{
		SessionID:   sessionID,
		ActionID:    uuid.New(),
		EventID:     uuid.New(),
		Action:      &model.Action{SessionID: sessionID, Type: model.ActionFileWrite, Tool: "Write"},
		Decision:    &model.EvaluationResult{Decision: model.DecisionDefer, Message: "wait_for_classification"},
		DeferReason: "wait_for_classification",
	})
	require.NoError(t, err)

	row := &storage.DeferredActionRow{
		ID:              uuid.New(),
		SessionID:       sessionID,
		ReceiptSequence: rec.Sequence,
		ActionID:        uuid.New(),
		DeferredAt:      time.Now().UTC(),
		ExpiresAt:       expiresAt,
		Reason:          "wait_for_classification",
		Status:          storage.DeferredActionStatusPending,
	}
	require.NoError(t, store.InsertDeferredAction(ctx, row))
	return row
}

func TestRenderDeferralsTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderDeferralsTable(&buf, tui.NewColorizer(false), nil)
	assert.Contains(t, buf.String(), "No deferrals")
}

func TestResolveDeferralRow_AllowInsertsFollowUpReceipt(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	require.NoError(t, resolveDeferralRow(ctx, store, row, "allow", "alice", "ok"))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedAllow, got.Status)
	assert.Equal(t, "alice", got.Resolver)

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	follow := receipts[1]
	assert.Equal(t, "allow", follow.Decision)
	require.NotNil(t, follow.DeferralOfSequence)
	assert.Equal(t, row.ReceiptSequence, *follow.DeferralOfSequence)

	audits, err := store.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: SelfAuditActionDeferralResolved})
	require.NoError(t, err)
	assert.NotEmpty(t, audits)
}

func TestResolveDeferralRow_DenyInsertsFollowUpReceipt(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	require.NoError(t, resolveDeferralRow(ctx, store, row, "deny", "alice", ""))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedDeny, got.Status)

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	assert.Equal(t, "block", receipts[1].Decision)
}

func TestTimeoutDeferralRow_FlipsAndEmitsAudit(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(-1*time.Hour))

	require.NoError(t, timeoutDeferralRow(ctx, store, row, time.Now().UTC()))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedTimeout, got.Status)
	assert.Equal(t, "system:timeout", got.Resolver)

	audits, err := store.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: SelfAuditActionDeferralTimeout})
	require.NoError(t, err)
	assert.NotEmpty(t, audits)

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	assert.Equal(t, "block", receipts[1].Decision)
}

// seedExistingFollowUp emulates the partial-failure state: a follow-up
// receipt was inserted by a previous attempt that then crashed before the
// deferred-action row was flipped. The deferred row stays pending.
func seedExistingFollowUp(t *testing.T, store *storage.SQLiteStore, row *storage.DeferredActionRow, decision string) *receipt.Record {
	t.Helper()
	ctx := context.Background()
	originalSeq := row.ReceiptSequence
	gen := receipt.NewSQLite(store)
	rec, err := gen.Record(ctx, &receipt.RecordInput{
		SessionID: row.SessionID,
		ActionID:  row.ActionID,
		Action: &model.Action{
			SessionID: row.SessionID,
			Type:      model.ActionFileWrite,
			Tool:      "Write",
		},
		Decision: &model.EvaluationResult{
			Decision: model.Decision(decision),
			Message:  "pre-existing follow-up",
		},
		DeferReason:        row.Reason,
		DeferralOfSequence: &originalSeq,
	})
	require.NoError(t, err)
	return rec
}

func TestResolveDeferralRow_IsIdempotentWhenFollowUpAlreadyExists(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	existing := seedExistingFollowUp(t, store, row, "allow")

	require.NoError(t, resolveDeferralRow(ctx, store, row, "allow", "alice", "retry"))

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	require.Len(t, receipts, 2, "retry must reuse the existing follow-up receipt, not append another")
	follow := receipts[1]
	assert.Equal(t, existing.Sequence, follow.Sequence)

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedAllow, got.Status,
		"row must still flip to resolved on the retry")
}

func TestTimeoutDeferralRow_IsIdempotentWhenFollowUpAlreadyExists(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(-1*time.Hour))

	existing := seedExistingFollowUp(t, store, row, "block")

	require.NoError(t, timeoutDeferralRow(ctx, store, row, time.Now().UTC()))

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	require.Len(t, receipts, 2, "retry must reuse the existing follow-up receipt on timeout")
	follow := receipts[1]
	assert.Equal(t, existing.Sequence, follow.Sequence)

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedTimeout, got.Status)
}

func TestConfirmAllowResolution_AcceptsYes(t *testing.T) {
	for _, reply := range []string{"y\n", "Y\n", "yes\n", "YES\n", " yes \n"} {
		var out bytes.Buffer
		ok, err := confirmAllowResolution(&out, strings.NewReader(reply), uuid.New().String())
		require.NoError(t, err)
		assert.True(t, ok, "reply %q must confirm", reply)
		assert.Contains(t, out.String(), "ALLOW?")
	}
}

func TestConfirmAllowResolution_RejectsOthers(t *testing.T) {
	for _, reply := range []string{"n\n", "\n", "no\n", "abort\n", ""} {
		var out bytes.Buffer
		ok, err := confirmAllowResolution(&out, strings.NewReader(reply), uuid.New().String())
		require.NoError(t, err)
		assert.False(t, ok, "reply %q must not confirm", reply)
	}
}

func TestResolveCmd_AllowWithoutConfirmDoesNotResolve(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	var out bytes.Buffer
	confirmed, err := confirmAllowResolution(&out, strings.NewReader("n\n"), row.ID.String())
	require.NoError(t, err)
	require.False(t, confirmed)

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	assert.Len(t, receipts, 1, "no follow-up receipt should exist when confirmation is declined")
	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.DeferredActionStatusPending, got.Status)
}

func TestResolveCmd_AllowWithConfirmProceeds(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	var out bytes.Buffer
	confirmed, err := confirmAllowResolution(&out, strings.NewReader("y\n"), row.ID.String())
	require.NoError(t, err)
	require.True(t, confirmed)

	require.NoError(t, resolveDeferralRow(ctx, store, row, "allow", "alice", ""))

	got, err := store.GetDeferredAction(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, storage.DeferredActionStatusResolvedAllow, got.Status)
}

func TestResolveDeferralRow_ReceiptChainStaysValid(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()
	row := seedDeferral(t, store, sessionID, time.Now().Add(10*time.Minute))

	require.NoError(t, resolveDeferralRow(ctx, store, row, "allow", "alice", ""))

	receipts, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID, Limit: -1})
	require.NoError(t, err)
	chainRows := make([]receipt.ChainRow, 0, len(receipts))
	for _, r := range receipts {
		chainRows = append(chainRows, receipt.ChainRowFromReceipt(r))
	}
	breaks := receipt.VerifyChain(chainRows)
	assert.Empty(t, breaks, "follow-up receipt must keep the chain valid")
}

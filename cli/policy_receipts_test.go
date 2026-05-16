package cli

import (
	"bytes"
	"context"
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

func recordSampleReceipts(t *testing.T, store *storage.SQLiteStore, sessionID uuid.UUID, n int) []*receipt.Record {
	t.Helper()
	g := receipt.NewSQLite(store)
	ctx := context.Background()
	out := make([]*receipt.Record, 0, n)
	for i := 0; i < n; i++ {
		rec, err := g.Record(ctx, &receipt.RecordInput{
			SessionID: sessionID,
			ActionID:  uuid.New(),
			EventID:   uuid.New(),
			Action: &model.Action{
				SessionID: sessionID,
				Type:      model.ActionFileRead,
				Tool:      "Read",
				Agent:     "claude-code",
				Parameters: model.Parameters{
					Path: "/tmp/x.txt",
				},
			},
			Snapshot: &model.ContextSnapshot{TotalActions: i + 1},
			Decision: &model.EvaluationResult{
				Decision:       model.DecisionGuidance,
				MatchedRuleIDs: []string{"rule-a"},
				Severity:       model.SeverityMedium,
				Message:        "ok",
			},
		})
		require.NoError(t, err)
		out = append(out, rec)
	}
	return out
}

func TestVerifyReceiptChains_FreshDataPasses(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	recordSampleReceipts(t, store, sessionID, 4)

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 4)

	breaks, _, err := verifyReceiptChains(ctx, store, rows, &sessionID, false, nil)
	require.NoError(t, err)
	assert.Empty(t, breaks, "fresh chain must verify clean")
}

func TestVerifyReceiptChains_DetectsHashMutation(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	recordSampleReceipts(t, store, sessionID, 3)

	_, err := store.DB().ExecContext(ctx,
		`UPDATE aarm_receipts SET message = 'tampered' WHERE session_id = ? AND sequence = 2`,
		sessionID,
	)
	require.NoError(t, err)

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)

	breaks, _, err := verifyReceiptChains(ctx, store, rows, &sessionID, false, nil)
	require.NoError(t, err)
	require.NotEmpty(t, breaks, "tampered row must break verification")

	audits, err := store.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: SelfAuditActionReceiptChainBroken})
	require.NoError(t, err)
	assert.NotEmpty(t, audits, "chain break must produce a self-audit row")
}

func TestVerifyReceiptChains_SessionLargerThanLimit(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	const total = 25
	recordSampleReceipts(t, store, sessionID, total)

	truncated, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{
		SessionID: &sessionID,
		Limit:     5,
	})
	require.NoError(t, err)
	require.Len(t, truncated, 5, "list path must respect --limit")

	breaks, _, err := verifyReceiptChains(ctx, store, truncated, &sessionID, false, nil)
	require.NoError(t, err)
	assert.Empty(t, breaks, "verify with --session must re-fetch the full chain, ignoring --limit")
}

func TestVerifyReceiptChains_AllSessionsCoversChainsOutsideLimit(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	oldSession := uuid.New()
	newSession := uuid.New()
	recordSampleReceipts(t, store, oldSession, 3)
	recordSampleReceipts(t, store, newSession, 3)

	_, err := store.DB().ExecContext(ctx,
		`UPDATE aarm_receipts SET message = 'tampered' WHERE session_id = ? AND sequence = 2`,
		oldSession,
	)
	require.NoError(t, err)

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, rows, 3, "list path returns just the newest 3 receipts")
	for _, r := range rows {
		require.Equal(t, newSession, r.SessionID, "older session must not appear within the limit")
	}

	withoutAll, _, err := verifyReceiptChains(ctx, store, rows, nil, false, nil)
	require.NoError(t, err)
	assert.Empty(t, withoutAll, "the truncated default verify never visits the tampered older session")

	withAll, _, err := verifyReceiptChains(ctx, store, rows, nil, true, nil)
	require.NoError(t, err)
	require.NotEmpty(t, withAll, "--all-sessions must enumerate every session and catch the tamper")
	var tamperedSeen bool
	for _, b := range withAll {
		if b.SessionID == oldSession {
			tamperedSeen = true
		}
	}
	assert.True(t, tamperedSeen, "the older tampered session must be among the reported breaks")
}

// TestVerifyReceiptChains_DetectsNonZeroFirstPrevHash exercises the
// "first receipt prev_hash is not the zero state" rule on the DB-side chain
// verifier. The exported-log verifier already enforced this; the DB-side
// verifier silently let it pass before the shared VerifyChain unification.
func TestVerifyReceiptChains_DetectsNonZeroFirstPrevHash(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	recordSampleReceipts(t, store, sessionID, 2)

	bogus := bytes.Repeat([]byte{0x01}, 32)
	_, err := store.DB().ExecContext(ctx,
		`UPDATE aarm_receipts SET prev_hash = ? WHERE session_id = ? AND sequence = 1`,
		bogus, sessionID,
	)
	require.NoError(t, err)

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sessionID})
	require.NoError(t, err)

	breaks, _, err := verifyReceiptChains(ctx, store, rows, &sessionID, false, nil)
	require.NoError(t, err)
	require.NotEmpty(t, breaks, "non-zero prev_hash on the first receipt must surface as a chain break")
	var sawZeroStateReason bool
	for _, b := range breaks {
		if b.Sequence == 1 && b.Reason != "" &&
			(b.Reason == "first receipt prev_hash is not the zero state" ||
				b.Reason == "stored hash does not match recomputed hash") {
			sawZeroStateReason = true
		}
	}
	assert.True(t, sawZeroStateReason, "expected zero-state or hash mismatch break, got: %+v", breaks)
}

func TestRenderReceiptsTable_EmptyShowsHint(t *testing.T) {
	var buf bytes.Buffer
	renderReceiptsTable(&buf, tui.NewColorizer(false), nil, false)
	assert.Contains(t, buf.String(), "No receipts recorded yet.")
}

func TestRenderReceiptsTable_ListsRows(t *testing.T) {
	var buf bytes.Buffer
	row := &storage.ReceiptRow{
		ID:           uuid.New(),
		SessionID:    uuid.New(),
		Sequence:     1,
		RecordedAt:   time.Now().UTC(),
		Agent:        "claude-code",
		Tool:         "Read",
		ActionType:   "file_read",
		Decision:     "guidance",
		ResultStatus: "pending",
		Hash:         bytes.Repeat([]byte{0xab}, 32),
	}
	renderReceiptsTable(&buf, tui.NewColorizer(false), []*storage.ReceiptRow{row}, true)
	out := buf.String()
	assert.Contains(t, out, "ababababab")
	assert.Contains(t, out, "guidance")
}

func TestRenderVerifyResults_OKAndFailures(t *testing.T) {
	var buf bytes.Buffer
	renderVerifyResults(&buf, tui.NewColorizer(false), nil)
	assert.Contains(t, buf.String(), "Chain verification: OK")

	buf.Reset()
	renderVerifyResults(&buf, tui.NewColorizer(false), []receiptVerifyBreak{
		{SessionID: uuid.New(), Sequence: 2, Reason: "boom"},
	})
	out := buf.String()
	assert.Contains(t, out, "Chain verification: FAILED")
	assert.Contains(t, out, "boom")
}

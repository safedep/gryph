package receipt

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInput(sessionID uuid.UUID, decision model.Decision) *RecordInput {
	return &RecordInput{
		SessionID: sessionID,
		ActionID:  uuid.New(),
		EventID:   uuid.New(),
		Action: &model.Action{
			ID:        uuid.New(),
			SessionID: sessionID,
			Type:      model.ActionFileRead,
			Tool:      "Read",
			Agent:     "claude-code",
			Parameters: model.Parameters{
				Path: "/tmp/test.txt",
			},
		},
		Snapshot: &model.ContextSnapshot{
			TotalActions: 1,
			FilesRead:    1,
		},
		Decision: &model.EvaluationResult{
			Decision:       decision,
			MatchedRuleIDs: []string{"rule-1"},
			Severity:       model.SeverityMedium,
			Message:        "ok",
		},
	}
}

func TestSQLiteGenerator_RecordChainsHashes(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	first, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, int64(1), first.Sequence)
	assert.Empty(t, first.PrevHash, "first receipt has no prev_hash")
	assert.Len(t, first.Hash, HashSize)

	second, err := g.Record(ctx, newInput(sessionID, model.DecisionBlock))
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, int64(2), second.Sequence)
	assert.True(t, bytes.Equal(second.PrevHash, first.Hash), "second.prev_hash must equal first.hash")
	assert.False(t, bytes.Equal(second.Hash, first.Hash))
}

func TestSQLiteGenerator_SequenceMonotonic(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	const n = 10
	for i := 1; i <= n; i++ {
		rec, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
		require.NoError(t, err)
		assert.Equal(t, int64(i), rec.Sequence)
	}

	rows, err := store.QueryReceipts(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, rows, n)
}

func TestSQLiteGenerator_ConcurrentSameSession(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	const n = 128
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	rows, err := store.QueryReceipts(ctx, nil)
	require.NoError(t, err)
	require.Len(t, rows, n, "every concurrent insert must be persisted")

	seen := make(map[int64]bool, n)
	for _, r := range rows {
		assert.False(t, seen[r.Sequence], "duplicate sequence %d", r.Sequence)
		seen[r.Sequence] = true
	}
	for i := int64(1); i <= int64(n); i++ {
		assert.True(t, seen[i], "missing sequence %d in 1..%d", i, n)
	}
}

func TestSQLiteGenerator_ConcurrentSessionsIsolated(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionA := uuid.New()
	sessionB := uuid.New()
	const per = 8

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < per; i++ {
			_, err := g.Record(ctx, newInput(sessionA, model.DecisionGuidance))
			assert.NoError(t, err)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < per; i++ {
			_, err := g.Record(ctx, newInput(sessionB, model.DecisionGuidance))
			assert.NoError(t, err)
		}
	}()
	wg.Wait()

	for _, sess := range []uuid.UUID{sessionA, sessionB} {
		got, err := store.GetLastReceiptForSession(ctx, sess)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(per), got.Sequence)
	}
}

func TestSQLiteGenerator_AllowVsBlockDifferInRow(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	allow, err := g.Record(ctx, newInput(sessionID, model.DecisionAllow))
	require.NoError(t, err)
	block, err := g.Record(ctx, newInput(sessionID, model.DecisionBlock))
	require.NoError(t, err)
	assert.False(t, bytes.Equal(allow.Hash, block.Hash))

	last, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, "blocked", last.ResultStatus, "block decision defaults to result_status=blocked")
	assert.Equal(t, "block", last.Decision)
}

func TestSQLiteGenerator_UpdateResult(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	rec, err := g.Record(ctx, newInput(sessionID, model.DecisionGuidance))
	require.NoError(t, err)

	require.NoError(t, g.UpdateResult(ctx, sessionID, rec.Sequence, model.Result{
		Status:   model.ResultSuccess,
		Duration: 150 * time.Millisecond,
	}))

	last, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, "success", last.ResultStatus)
	require.NotNil(t, last.DurationMS)
	assert.Equal(t, int64(150), *last.DurationMS)
}

func TestComputeHash_DeterministicAndStable(t *testing.T) {
	in := &HashInput{
		Sequence:       1,
		PrevHash:       nil,
		RecordedAtUnix: 1700000000,
		SessionID:      uuid.UUID{},
		Agent:          "claude-code",
		Tool:           "Read",
		ActionType:     "file_read",
		Decision:       "block",
		MatchedRuleIDs: []string{"r1", "r2"},
		Snapshot: map[string]interface{}{
			"total_actions": 3,
			"files_read":    2,
		},
		ActionPayload: map[string]interface{}{
			"path": "/tmp/a",
		},
	}
	h1, err := ComputeHash(in)
	require.NoError(t, err)
	h2, err := ComputeHash(in)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(h1, h2), "ComputeHash must be deterministic")
	assert.Len(t, h1, HashSize)
}

func TestSQLiteGenerator_PolicyHashAndSubagentPersist(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()
	sessionID := uuid.New()

	in := newInput(sessionID, model.DecisionGuidance)
	in.PolicyHash = []byte("0123456789abcdef0123456789abcdef")
	in.Action.SubagentID = "sub-1"
	in.Action.SubagentType = "Plan"

	rec, err := g.Record(ctx, in)
	require.NoError(t, err)
	require.NotNil(t, rec)

	last, err := store.GetLastReceiptForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, "sub-1", last.SubagentID)
	assert.Equal(t, "Plan", last.SubagentType)
	assert.True(t, bytes.Equal(in.PolicyHash, last.PolicyHash))

	chain := ChainRowFromReceipt(last)
	expected, err := ComputeHash(NewHashInput(chain.Fields))
	require.NoError(t, err)
	assert.True(t, bytes.Equal(expected, last.Hash), "row hash must match recomputed hash from chain row")
}

func TestSQLiteGenerator_PolicyHashFlipChangesHash(t *testing.T) {
	store := storagetest.NewStore(t)
	g := NewSQLite(store)
	ctx := context.Background()

	sessionA := uuid.New()
	inA := newInput(sessionA, model.DecisionGuidance)
	inA.PolicyHash = bytes.Repeat([]byte{0xaa}, 32)
	recA, err := g.Record(ctx, inA)
	require.NoError(t, err)

	sessionB := uuid.New()
	inB := newInput(sessionB, model.DecisionGuidance)
	inB.PolicyHash = bytes.Repeat([]byte{0xbb}, 32)
	recB, err := g.Record(ctx, inB)
	require.NoError(t, err)

	assert.False(t, bytes.Equal(recA.Hash, recB.Hash), "different policy hashes must yield different receipt hashes")
}

func TestComputeHash_SortedKeysMatter(t *testing.T) {
	a := &HashInput{
		Sequence:       1,
		RecordedAtUnix: 1,
		SessionID:      uuid.UUID{},
		Snapshot: map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	}
	b := &HashInput{
		Sequence:       1,
		RecordedAtUnix: 1,
		SessionID:      uuid.UUID{},
		Snapshot: map[string]interface{}{
			"b": 2,
			"a": 1,
		},
	}
	ha, err := ComputeHash(a)
	require.NoError(t, err)
	hb, err := ComputeHash(b)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(ha, hb), "key insertion order must not affect hash")
}

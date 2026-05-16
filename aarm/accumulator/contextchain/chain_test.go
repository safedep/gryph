package contextchain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildChainedRows builds n chained rows starting at startSeq for the
// supplied session, producing rows whose stored (sequence, prev_hash, hash)
// triplet satisfies Verify when startSeq == 1. The hash inputs use the same
// canonical mapping as the storage layer so per-row recomputation matches.
func buildChainedRows(t *testing.T, sessionID uuid.UUID, startSeq int64, n int) []Row {
	t.Helper()
	rows := make([]Row, 0, n)
	base := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	var prevHash []byte
	for i := 0; i < n; i++ {
		seq := startSeq + int64(i)
		actionID := uuid.New()
		fields := InputFromRow(
			seq, prevHash, base.Add(time.Duration(i)*time.Millisecond),
			sessionID, uuid.Nil, actionID,
			"file_read", "Read", "claude-code", "", "",
			nil, 0,
		)
		hash, err := ComputeHash(fields)
		require.NoError(t, err)
		rows = append(rows, Row{
			SessionID: sessionID,
			Sequence:  seq,
			PrevHash:  prevHash,
			Hash:      hash,
			Fields:    fields,
		})
		prevHash = hash
	}
	return rows
}

func TestVerify_CleanChainHasNoBreaks(t *testing.T) {
	sessionID := uuid.New()
	rows := buildChainedRows(t, sessionID, 1, 3)
	verified, breaks := Verify(rows)
	assert.Empty(t, breaks)
	assert.Equal(t, 3, verified, "clean chain must count every row as verified")
}

func TestVerify_FirstRowSequenceMismatchReportsPerRowDamage(t *testing.T) {
	sessionID := uuid.New()
	rows := buildChainedRows(t, sessionID, 2, 3)

	verified, breaks := Verify(rows)

	require.NotEmpty(t, breaks, "missing first row must surface as a break")
	assert.Contains(t, breaks[0].Reason, "first row sequence is 2, expected 1",
		"the head-of-chain diagnostic must point at the first observed sequence")
	assert.Equal(t, 0, verified, "no row may count as verified when every row produces a break")

	// The fix continues independent per-row checks after the head-of-chain
	// break instead of carrying the bad prevSeq forward. Every row in the
	// surviving slice has a non-1 sequence so each one must report a
	// distinct sequence break (one for sequence=2, then gap-style breaks
	// for sequences 3 and 4 against the reset baseline).
	sequenceBreaks := 0
	for _, b := range breaks {
		if b.Reason == "first row sequence is 2, expected 1" ||
			b.Reason == "first row sequence is 3, expected 1" ||
			b.Reason == "first row sequence is 4, expected 1" ||
			b.Reason == "sequence gap: got 3, expected 1" ||
			b.Reason == "sequence gap: got 4, expected 1" {
			sequenceBreaks++
		}
	}
	assert.GreaterOrEqual(t, sequenceBreaks, 3,
		"each surviving row must produce its own sequence diagnostic, got breaks=%v", breaks)
}

func TestVerify_DetectsTamperedHash(t *testing.T) {
	sessionID := uuid.New()
	rows := buildChainedRows(t, sessionID, 1, 3)
	rows[1].Fields.Tool = "tampered"

	verified, breaks := Verify(rows)
	require.NotEmpty(t, breaks)
	assert.Equal(t, 2, verified, "two clean rows must remain verified when the middle row is tampered")
	found := false
	for _, b := range breaks {
		if b.Sequence == 2 && b.Reason == "stored hash does not match recomputed hash" {
			found = true
		}
	}
	assert.True(t, found, "tampered row must produce a hash mismatch break, got %v", breaks)
}

func TestVerify_DetectsPrevHashMismatch(t *testing.T) {
	sessionID := uuid.New()
	rows := buildChainedRows(t, sessionID, 1, 3)
	bogus := make([]byte, HashSize)
	bogus[0] = 0xAA
	rows[2].PrevHash = bogus

	verified, breaks := Verify(rows)
	require.NotEmpty(t, breaks)
	assert.Equal(t, 2, verified, "the two earlier rows must remain verified when row 3's prev_hash is bogus")
	found := false
	for _, b := range breaks {
		if b.Sequence == 3 && b.Reason == "prev_hash does not match previous row hash" {
			found = true
		}
	}
	assert.True(t, found, "broken prev_hash must produce a chain break, got %v", breaks)
}

func TestVerify_SingleTamperedRowEmitsMultipleBreaksButCountsOnce(t *testing.T) {
	sessionID := uuid.New()
	rows := buildChainedRows(t, sessionID, 1, 3)
	// Tamper row 2 in two independent ways so it emits both a recomputed-
	// hash break (Fields changed) and a prev_hash break (PrevHash zeroed).
	// rows[1].Hash is left intact so the chain still threads through to
	// row 3 and rows 1 and 3 stay clean. The verified counter must credit
	// each of those clean rows exactly once even though row 2 contributes
	// multiple Breaks.
	rows[1].Fields.Tool = "tampered"
	rows[1].PrevHash = make([]byte, HashSize)

	verified, breaks := Verify(rows)
	assert.Equal(t, 2, verified,
		"exactly the two unmodified rows must count as verified, got verified=%d breaks=%v", verified, breaks)
	assert.GreaterOrEqual(t, len(breaks), 2,
		"tampered row must surface multiple diagnostics (recompute + prev_hash)")
	for _, b := range breaks {
		assert.Equal(t, int64(2), b.Sequence,
			"every diagnostic must point at the tampered row, got %+v", b)
	}
}

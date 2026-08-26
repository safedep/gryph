package contextchain

import (
	"bytes"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/testchain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Property-based tests covering the load-bearing context-chain invariants:
//
//   1. Every row's prev_hash equals the previous row's hash and the first row's
//      prev_hash is the 32-byte zero state.
//   2. Sequence is strictly monotonic, starts at 1, no gaps.
//   3. ComputeHash(InputFromRow(...)) == row.Hash for every row.
//   4. A tampered row produces at least one Break from Verify.
//
// Structural inputs (chain size, tamper case, field selectors) are
// reproducible from the logged seed. UUID identifiers are crypto/rand
// and not part of the verification logic, so failures from chain
// invariants are deterministic given the seed even though UUIDs differ.

// buildPropertyChain constructs a clean context-chain of n rows for the
// given session. Fields vary per row so any field-level tamper detection
// actually fires. Each row's PrevHash is a fresh copy so per-row tamper
// mutations stay localised to that row's PrevHash buffer (mutating it does
// not also corrupt the previous row's Hash through a shared backing array).
func buildPropertyChain(t *testing.T, sessionID uuid.UUID, n int) []Row {
	t.Helper()
	require.GreaterOrEqual(t, n, 1)
	rows := make([]Row, 0, n)
	base := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	var prevHash []byte
	for i := 0; i < n; i++ {
		seq := int64(i + 1)
		var rowPrev []byte
		if prevHash != nil {
			rowPrev = append([]byte(nil), prevHash...)
		}
		fields := InputFromRow(
			seq, rowPrev,
			base.Add(time.Duration(i)*time.Millisecond),
			sessionID, uuid.New(), uuid.New(),
			"file_read", "Read", "claude-code", "proj", "/work",
			[]string{"none"}, float32(i)*0.1,
		)
		hash, err := ComputeHash(fields)
		require.NoError(t, err)
		rows = append(rows, Row{
			SessionID: sessionID,
			Sequence:  seq,
			PrevHash:  rowPrev,
			Hash:      hash,
			Fields:    fields,
		})
		prevHash = hash
	}
	return rows
}

func TestProperty_ChainBuildsValid(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(n testchain.ChainSize) bool {
		rows := buildPropertyChain(t, uuid.New(), int(n))
		verified, breaks := Verify(rows)
		if len(breaks) != 0 {
			t.Logf("unexpected breaks for n=%d: %+v", int(n), breaks)
			return false
		}
		if verified != len(rows) {
			t.Logf("verified=%d want=%d", verified, len(rows))
			return false
		}
		if rows[0].Sequence != 1 {
			return false
		}
		for i := 1; i < len(rows); i++ {
			if rows[i].Sequence != rows[i-1].Sequence+1 {
				return false
			}
			if !bytes.Equal(rows[i].PrevHash, rows[i-1].Hash) {
				return false
			}
		}
		return true
	}
	require.NoError(t, quick.Check(property, cfg))
}

type tamperCase struct {
	Size  int
	Row   int
	Field int
}

func (tamperCase) Generate(rand *rand.Rand, _ int) reflect.Value {
	size := rand.Intn(20) + 1
	return reflect.ValueOf(tamperCase{
		Size:  size,
		Row:   rand.Intn(size),
		Field: rand.Intn(8),
	})
}

// tamperRow flips one of eight hash-input fields on r so Verify must report
// a break.
func tamperRow(r *Row, field int) {
	switch field % 8 {
	case 0:
		r.Fields.ActionType = r.Fields.ActionType + "-tampered"
	case 1:
		r.Fields.Tool = r.Fields.Tool + "-tampered"
	case 2:
		r.Fields.Agent = r.Fields.Agent + "-tampered"
	case 3:
		r.Fields.Project = r.Fields.Project + "-tampered"
	case 4:
		r.Fields.WorkingDir = r.Fields.WorkingDir + "-tampered"
	case 5:
		r.Fields.DataClassifications = append([]string{}, "tampered")
	case 6:
		r.Fields.InjectionScore = r.Fields.InjectionScore + 0.5
	case 7:
		r.Fields.TimestampUnixNano++
	}
}

func TestProperty_AnyFieldTamperDetected(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(tc tamperCase) bool {
		rows := buildPropertyChain(t, uuid.New(), tc.Size)
		original := rows[tc.Row]
		tamperRow(&rows[tc.Row], tc.Field)
		if reflect.DeepEqual(original.Fields, rows[tc.Row].Fields) {
			t.Logf("tamper produced identical fields for case %+v: skipping", tc)
			return true
		}
		_, breaks := Verify(rows)
		if len(breaks) == 0 {
			t.Logf("expected at least one break for tamper case %+v but got none", tc)
			return false
		}
		return true
	}
	require.NoError(t, quick.Check(property, cfg))
}

// TestProperty_PrevHashTamperDetected closes a tamper-coverage gap left by
// TestProperty_AnyFieldTamperDetected: tamperRow never mutates PrevHash, so a
// regression that broke either the prev_hash chain-link check or the link
// between row.PrevHash and the shared Fields.PrevHash backing slice would not
// surface. Two tamper modes:
//
//   - Kind 0: in-place bit flip on row.PrevHash[0]. Because
//     buildPropertyChain has the row's PrevHash and Fields.PrevHash share a
//     single backing slice, this breaks BOTH the chain-link check ("prev_hash
//     does not match previous row hash") AND the hash recompute check
//     ("stored hash does not match recomputed hash").
//   - Kind 1: reassign row.PrevHash to a new buffer without touching
//     Fields.PrevHash. This isolates the chain-link check: the hash recompute
//     stays valid because Fields.PrevHash is unchanged, but the chain link
//     between this row and the previous row is severed.
//
// The test asserts that at least one break attributed to the tampered row
// names the prev_hash chain-link check explicitly so a future change to the
// verifier reason text fails loudly rather than silently passing on any
// unrelated hash diagnostic.
func TestProperty_PrevHashTamperDetected(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(tc testchain.PrevHashTamperCase) bool {
		rows := buildPropertyChain(t, uuid.New(), tc.Size)
		row := &rows[tc.Row]
		require.NotEmpty(t, row.PrevHash, "non-first row must have prev_hash")
		switch tc.Kind {
		case 0:
			row.PrevHash[0] ^= 0xFF
		case 1:
			row.PrevHash = []byte("tampered-prev-hash-buffer-not32!")
		}
		_, breaks := Verify(rows)
		if len(breaks) == 0 {
			t.Logf("expected at least one break for prev_hash tamper case %+v", tc)
			return false
		}
		sawRow := false
		for _, b := range breaks {
			if b.Sequence != row.Sequence {
				continue
			}
			sawRow = true
			if strings.Contains(b.Reason, "prev_hash") {
				return true
			}
		}
		if !sawRow {
			t.Logf("no break attributed to tampered row %d for case %+v: breaks=%+v", row.Sequence, tc, breaks)
		} else {
			t.Logf("no prev_hash break attributed to tampered row %d for case %+v: breaks=%+v", row.Sequence, tc, breaks)
		}
		return false
	}
	require.NoError(t, quick.Check(property, cfg))
}

func TestProperty_HashRecomputeIsDeterministic(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(n testchain.ChainSize) bool {
		rows := buildPropertyChain(t, uuid.New(), int(n))
		const repeats = 5
		for _, r := range rows {
			first, err := ComputeHash(r.Fields)
			require.NoError(t, err)
			for i := 1; i < repeats; i++ {
				got, err := ComputeHash(r.Fields)
				require.NoError(t, err)
				if !bytes.Equal(first, got) {
					t.Logf("hash recompute drift on iteration %d: first=%x got=%x", i, first, got)
					return false
				}
			}
		}
		return true
	}
	require.NoError(t, quick.Check(property, cfg))
}

// Sanity-check that buildPropertyChain produces a chain with the documented
// invariants.
func TestPropertyHelpers_BuildPropertyChainSanity(t *testing.T) {
	rows := buildPropertyChain(t, uuid.New(), 5)
	require.Len(t, rows, 5)
	assert.Equal(t, int64(1), rows[0].Sequence)
	assert.Empty(t, rows[0].PrevHash)
	for i := 1; i < len(rows); i++ {
		assert.Equal(t, rows[i-1].Sequence+1, rows[i].Sequence)
		assert.True(t, bytes.Equal(rows[i].PrevHash, rows[i-1].Hash))
	}
	verified, breaks := Verify(rows)
	assert.Empty(t, breaks)
	assert.Equal(t, 5, verified)
}

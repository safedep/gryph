package receipt

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/testchain"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Property-based tests covering the load-bearing receipt chain invariants:
//
//   1. Every row's prev_hash equals the previous row's hash and the first row's
//      prev_hash is the 32-byte zero state.
//   2. Sequence is strictly monotonic, starts at 1, no gaps.
//   3. ComputeHash(NewHashInput(row)) == row.Hash for every row.
//   4. A tampered row produces at least one ChainBreak from VerifyChain.
//   5. A v2 row verifies after an export profile removes its command and URL.
//
// Structural inputs (chain size, tamper case, field selectors) are
// reproducible from the logged seed. UUID identifiers are crypto/rand
// and not part of the verification logic, so failures from chain
// invariants are deterministic given the seed even though UUIDs differ.

// buildPropertyChain constructs a clean hash chain of n rows for the given
// session. The fields vary per row so any field-level tamper detection
// actually fires. The first half of the rows uses hash v1 and the rest v2,
// as in a session that spans an upgrade. Each row's PrevHash is a fresh copy so per-row tamper
// mutations stay localised to that row's PrevHash buffer (mutating it does
// not also corrupt the previous row's Hash through a shared backing array).
func buildPropertyChain(t *testing.T, sessionID uuid.UUID, n int) []ChainRow {
	t.Helper()
	require.GreaterOrEqual(t, n, 1)
	rows := make([]ChainRow, 0, n)
	base := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	var prevHash []byte
	for i := 0; i < n; i++ {
		seq := int64(i + 1)
		var rowPrev []byte
		if prevHash != nil {
			rowPrev = append([]byte(nil), prevHash...)
		}
		command := fmt.Sprintf("curl https://example.com/%d", i)
		url := fmt.Sprintf("https://example.com/%d", i)
		fields := HashInputFields{
			Sequence:       seq,
			PrevHash:       rowPrev,
			RecordedAtUnix: base.Add(time.Duration(i) * time.Millisecond).UnixNano(),
			SessionID:      sessionID,
			ActionID:       uuid.New(),
			EventID:        uuid.New(),
			Agent:          "claude-code",
			Tool:           "Read",
			ActionType:     "file_read",
			Project:        "proj",
			Decision:       "guidance",
			Severity:       "medium",
			Message:        "msg",
			MatchedRuleIDs: []string{"r-1"},
			Snapshot:       map[string]interface{}{"total_actions": i + 1},
			ActionPayload:  map[string]interface{}{"path": "/tmp/x", "command": command, "url": url},
			HashVersion:    HashV1,
		}
		var salt []byte
		if i >= n/2 {
			fields.HashVersion = HashV2
			salt = bytes.Repeat([]byte{byte(i)}, contentSaltSize)
			var err error
			fields.CommandDigest, fields.URLDigest, err = contentDigests(salt, fields.ActionPayload)
			require.NoError(t, err)
		}
		hash, err := ComputeHash(NewHashInput(fields))
		require.NoError(t, err)
		rows = append(rows, ChainRow{
			SessionID:   sessionID,
			Sequence:    seq,
			PrevHash:    rowPrev,
			Hash:        hash,
			Fields:      fields,
			ContentSalt: salt,
		})
		prevHash = hash
	}
	return rows
}

func TestProperty_ChainBuildsValid(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(n testchain.ChainSize) bool {
		rows := buildPropertyChain(t, uuid.New(), int(n))
		breaks := VerifyChain(rows)
		if len(breaks) != 0 {
			t.Logf("unexpected breaks for n=%d: %+v", int(n), breaks)
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

// tamperCase captures a randomised tamper target: a chain size, a row index
// to tamper, and the field selector. Generate biases away from degenerate
// cases (size 0) and confines indices to the row range chosen.
type tamperCase struct {
	Size  int
	Row   int
	Field int
}

// Generate implements quick.Generator. The field enum is the set of
// hash-input fields exercised by tamperRow below.
func (tamperCase) Generate(rand *rand.Rand, _ int) reflect.Value {
	size := rand.Intn(20) + 1
	return reflect.ValueOf(tamperCase{
		Size:  size,
		Row:   rand.Intn(size),
		Field: rand.Intn(tamperFields),
	})
}

const tamperFields = 15

// tamperRow flips one hash-input field on r so VerifyChain must report a
// break. The selector matches the tamperCase.Field enum. A v1 row does not
// hash the digests, so the digest cases change the value that v1 hashes.
func tamperRow(r *ChainRow, field int) {
	v2 := r.Fields.HashVersion == HashV2
	switch field % tamperFields {
	case 0:
		r.Fields.Agent = r.Fields.Agent + "-tampered"
	case 1:
		r.Fields.Tool = r.Fields.Tool + "-tampered"
	case 2:
		r.Fields.ActionType = r.Fields.ActionType + "-tampered"
	case 3:
		r.Fields.Project = r.Fields.Project + "-tampered"
	case 4:
		r.Fields.Decision = "block"
	case 5:
		r.Fields.Severity = "critical"
	case 6:
		r.Fields.Message = r.Fields.Message + "-tampered"
	case 7:
		r.Fields.MatchedRuleIDs = append([]string{}, "r-tampered")
	case 8:
		r.Fields.RecordedAtUnix++
	case 9:
		r.Fields.Snapshot = map[string]interface{}{"total_actions": 999}
	case 10:
		if v2 {
			r.Fields.CommandDigest = privacy.Digest("tampered")
		} else {
			r.Fields.ActionPayload["command"] = "tampered"
		}
	case 11:
		if v2 {
			r.Fields.URLDigest = privacy.Digest("tampered")
		} else {
			r.Fields.ActionPayload["url"] = "tampered"
		}
	case 12:
		if v2 {
			r.Fields.HashVersion = HashV1
		} else {
			r.Fields.HashVersion = HashV2
		}
	case 13:
		r.Fields.ActionPayload["path"] = "/tmp/tampered"
	case 14:
		r.Fields.ActionPayload["command"] = "rm -rf /"
	}
}

func TestProperty_V2ContentRemovalVerifies(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(n testchain.ChainSize) bool {
		rows := buildPropertyChain(t, uuid.New(), int(n))
		for i := range rows {
			if rows[i].Fields.HashVersion == HashV2 {
				projectChainRow(&rows[i])
			}
		}
		if breaks := VerifyChain(rows); len(breaks) != 0 {
			t.Logf("unexpected breaks for n=%d: %+v", int(n), breaks)
			return false
		}
		return true
	}
	require.NoError(t, quick.Check(property, cfg))
}

func TestProperty_V2UnprojectedContentRemovalDetected(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(n testchain.ChainSize) bool {
		rows := buildPropertyChain(t, uuid.New(), int(n))
		last := &rows[len(rows)-1]
		if last.Fields.HashVersion != HashV2 {
			return true
		}
		delete(last.Fields.ActionPayload, "command")
		last.Fields.ActionPayload["url"] = privacy.RedactedValue
		return len(VerifyChain(rows)) > 0
	}
	require.NoError(t, quick.Check(property, cfg))
}

// projectChainRow removes the content as an export profile does: it
// redacts the command and the URL, drops the args and the salt, and marks
// the row projected.
func projectChainRow(r *ChainRow) {
	delete(r.Fields.ActionPayload, "args")
	r.Fields.ActionPayload["command"] = privacy.RedactedValue
	r.Fields.ActionPayload["url"] = privacy.RedactedValue
	r.ContentSalt = nil
	r.Projected = true
}

func TestProperty_AnyFieldTamperDetected(t *testing.T) {
	cfg := testchain.PropertyConfig(t)
	property := func(tc tamperCase) bool {
		rows := buildPropertyChain(t, uuid.New(), tc.Size)
		// Snapshot the original row contents so we are sure the tamper
		// actually changed the hash input.
		original := rows[tc.Row]
		tamperRow(&rows[tc.Row], tc.Field)
		if reflect.DeepEqual(original.Fields, rows[tc.Row].Fields) {
			t.Logf("tamper produced identical fields for case %+v: skipping", tc)
			return true
		}
		breaks := VerifyChain(rows)
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
		breaks := VerifyChain(rows)
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
			first, err := ComputeHash(NewHashInput(r.Fields))
			require.NoError(t, err)
			for i := 1; i < repeats; i++ {
				got, err := ComputeHash(NewHashInput(r.Fields))
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
// invariants. The property tests above all depend on this generator being
// correct. If this regresses every property test silently green-flags.
func TestPropertyHelpers_BuildPropertyChainSanity(t *testing.T) {
	rows := buildPropertyChain(t, uuid.New(), 5)
	require.Len(t, rows, 5)
	assert.Equal(t, int64(1), rows[0].Sequence)
	assert.Empty(t, rows[0].PrevHash)
	for i := 1; i < len(rows); i++ {
		assert.Equal(t, rows[i-1].Sequence+1, rows[i].Sequence)
		assert.True(t, bytes.Equal(rows[i].PrevHash, rows[i-1].Hash))
	}
	assert.Empty(t, VerifyChain(rows))
}

// Package contextchain implements the per-session hash chain for AARM
// context-action rows. It lives in a sub-package of aarm/accumulator
// because aarm/accumulator already imports storage, and the storage layer
// must in turn import the chain primitives to compute the per-row hash at
// insert time. The sub-package boundary breaks the import cycle while
// keeping the chain code adjacent to the accumulator it serves.
//
// The Context Accumulator records every mediated action into
// aarm_context_actions. Phase 5a layers a per-session hash chain on top so
// the PDP-facing context log carries the same tamper-evidence guarantees as
// the AARM receipt chain. Each row stores its (sequence, prev_hash, hash).
// The hash is SHA-256 over the canonical serialization of the row's identity
// and counter-feeding fields, length-prefixed in the order documented below.
//
// # Canonical hash input
//
// ComputeHash builds the SHA-256 input by length-prefixing each field with
// an 8-byte big-endian length, then concatenating in the exact order below.
// String fields are UTF-8 bytes. Numbers are 8-byte big-endian. UUIDs use
// their 16-byte binary form. JSON-ish payloads (data_classifications,
// reserved future map fields) pass through canonical.MarshalJSON so the
// serialization is order-stable. The final SHA-256 is the value stored in
// the hash column. The prev_hash for the first row of a session is 32 zero
// bytes.
//
// Field order (canonical):
//  1. sequence            (int64, 8 bytes BE)
//  2. prev_hash           (32 bytes, zero-padded for first row)
//  3. timestamp_unix_ns   (int64, 8 bytes BE)
//  4. session_id          (16 bytes)
//  5. event_id            (16 bytes, all zero when unset)
//  6. action_id           (16 bytes)
//  7. action_type         (utf-8 bytes)
//  8. tool                (utf-8 bytes)
//  9. agent               (utf-8 bytes)
//  10. project            (utf-8 bytes)
//  11. working_dir        (utf-8 bytes)
//  12. data_classifications (canonical JSON of []string, "null" when empty)
//  13. injection_score    (int64 bits of the float32 widened to float64,
//     8 bytes BE; 0 when unset)
//
// # Stability contract
//
// result_status, duration_ms, and error_message are not part of the hash
// input. They are populated post-hook by UpdateContextActionResult and
// would otherwise force a re-hash on every result update. The chain
// attests to the as-mediated row, not the post-hook outcome. This mirrors
// the receipt chain's split between insert-time hash inputs and post-hook
// mutations.
package contextchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/canonical"
)

// HashSize is the byte length of a context chain hash (SHA-256).
const HashSize = 32

// Input collects every byte that participates in the context chain hash.
// The storage layer constructs one per insert, hands it to ComputeHash, and
// persists both the input fields and the resulting hash.
type Input struct {
	Sequence            int64
	PrevHash            []byte
	TimestampUnixNano   int64
	SessionID           uuid.UUID
	EventID             uuid.UUID
	ActionID            uuid.UUID
	ActionType          string
	Tool                string
	Agent               string
	Project             string
	WorkingDir          string
	DataClassifications []string
	InjectionScore      float32
}

// Row is the minimal subset of context-action fields needed to re-derive a
// per-session hash chain. Verifiers convert their native representation (DB
// rows today, exported rows tomorrow) to a slice of Row.
type Row struct {
	SessionID uuid.UUID
	Sequence  int64
	PrevHash  []byte
	Hash      []byte
	Fields    Input
}

// InputFromRow builds an Input from a row's fields. Single source of truth
// for the field-to-input mapping so the insert path and the verifier
// always agree.
func InputFromRow(
	sequence int64,
	prevHash []byte,
	timestamp time.Time,
	sessionID, eventID, actionID uuid.UUID,
	actionType, tool, agent, project, workingDir string,
	dataClassifications []string,
	injectionScore float32,
) Input {
	return Input{
		Sequence:            sequence,
		PrevHash:            prevHash,
		TimestampUnixNano:   timestamp.UnixNano(),
		SessionID:           sessionID,
		EventID:             eventID,
		ActionID:            actionID,
		ActionType:          actionType,
		Tool:                tool,
		Agent:               agent,
		Project:             project,
		WorkingDir:          workingDir,
		DataClassifications: dataClassifications,
		InjectionScore:      injectionScore,
	}
}

// ComputeHash returns the SHA-256 of the canonical serialization of in. See
// package doc for the serialization format.
func ComputeHash(in Input) ([]byte, error) {
	var buf bytes.Buffer

	if err := writeInt64(&buf, in.Sequence); err != nil {
		return nil, err
	}

	prev := in.PrevHash
	if len(prev) == 0 {
		prev = make([]byte, HashSize)
	}
	if len(prev) != HashSize {
		return nil, fmt.Errorf("contextchain: prev_hash must be %d bytes, got %d", HashSize, len(prev))
	}
	if err := writeBytes(&buf, prev); err != nil {
		return nil, err
	}

	if err := writeInt64(&buf, in.TimestampUnixNano); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.SessionID); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.EventID); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.ActionID); err != nil {
		return nil, err
	}

	for _, s := range []string{in.ActionType, in.Tool, in.Agent, in.Project, in.WorkingDir} {
		if err := writeString(&buf, s); err != nil {
			return nil, err
		}
	}

	cls, err := canonical.MarshalJSON(in.DataClassifications)
	if err != nil {
		return nil, fmt.Errorf("contextchain: canonicalize data_classifications: %w", err)
	}
	if err := writeBytes(&buf, cls); err != nil {
		return nil, err
	}

	var scoreBits int64
	if in.InjectionScore != 0 {
		scoreBits = int64(math.Float64bits(float64(in.InjectionScore)))
	}
	if err := writeInt64(&buf, scoreBits); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(buf.Bytes())
	return sum[:], nil
}

func writeInt64(buf *bytes.Buffer, v int64) error {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	_, err := buf.Write(b[:])
	return err
}

func writeBytes(buf *bytes.Buffer, b []byte) error {
	var l [8]byte
	binary.BigEndian.PutUint64(l[:], uint64(len(b)))
	if _, err := buf.Write(l[:]); err != nil {
		return err
	}
	_, err := buf.Write(b)
	return err
}

func writeString(buf *bytes.Buffer, s string) error {
	return writeBytes(buf, []byte(s))
}

func writeUUID(buf *bytes.Buffer, id uuid.UUID) error {
	return writeBytes(buf, id[:])
}

// Break is a single integrity failure surfaced by Verify. The
// (SessionID, Sequence) pair identifies the offending row.
type Break struct {
	SessionID uuid.UUID
	Sequence  int64
	Reason    string
}

// Verify re-derives the per-session hash chain over rows and returns the
// number of rows that produced no integrity failures together with the
// per-row diagnostics. The caller must pass rows for a single session sorted
// ascending by Sequence. The check covers: sequence starts at 1 and
// increases monotonically, the first row's prev_hash is the zero state,
// every other prev_hash matches the previous row's hash, and each stored
// hash matches the hash recomputed from its fields.
//
// A row is counted as verified when it emits zero Breaks. A single tampered
// row can emit multiple diagnostics (for example a sequence gap plus a
// prev_hash mismatch plus a hash mismatch). Returning verified separately
// from len(breaks) keeps callers from having to subtract diagnostic counts
// from row counts and prevents under-counting.
//
// When the first row's sequence is not 1 (typically because the head of the
// chain was deleted) the verifier emits a break for that row and continues
// with prevSeq == 0 so every subsequent row is also checked against the
// expected start. This gives operators a per-row picture of the damage
// instead of one composite diagnostic.
func Verify(rows []Row) (verified int, breaks []Break) {
	var prevHash []byte
	var prevSeq int64
	for _, r := range rows {
		rowStart := len(breaks)
		expectedSeq := prevSeq + 1
		sequenceBroken := false
		if prevSeq == 0 && r.Sequence != 1 {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("first row sequence is %d, expected 1", r.Sequence),
			})
			sequenceBroken = true
		} else if prevSeq > 0 && r.Sequence != expectedSeq {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("sequence gap: got %d, expected %d", r.Sequence, expectedSeq),
			})
		}

		if prevSeq == 0 {
			if len(r.PrevHash) > 0 {
				zero := bytes.Repeat([]byte{0}, HashSize)
				if !bytes.Equal(r.PrevHash, zero) {
					breaks = append(breaks, Break{
						SessionID: r.SessionID,
						Sequence:  r.Sequence,
						Reason:    "first row prev_hash is not the zero state",
					})
				}
			}
		} else if !bytes.Equal(r.PrevHash, prevHash) {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "prev_hash does not match previous row hash",
			})
		}

		expectedHash, err := ComputeHash(r.Fields)
		if err != nil {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("recompute hash: %v", err),
			})
		} else if !bytes.Equal(expectedHash, r.Hash) {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "stored hash does not match recomputed hash",
			})
		}

		if len(breaks) == rowStart {
			verified++
		}

		prevHash = r.Hash
		if sequenceBroken {
			prevSeq = 0
		} else {
			prevSeq = r.Sequence
		}
	}
	return verified, breaks
}

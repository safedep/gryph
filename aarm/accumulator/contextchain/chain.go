// Package contextchain implements the per-session hash chain for context
// entries. It lives in a sub-package of aarm/accumulator because
// aarm/accumulator already imports storage, and the storage layer must in
// turn import the chain primitives to compute the per-row hash at insert
// time. The sub-package boundary breaks the import cycle while keeping the
// chain code adjacent to the accumulator it serves.
//
// Each context_entries row stores its (sequence, prev_hash, hash,
// hash_version). The hash is SHA-256 over the canonical serialization of the
// row's facts, length-prefixed in the order documented below.
//
// # Canonical hash input, version 2
//
// ComputeHash builds the SHA-256 input by length-prefixing each field with
// an 8-byte big-endian length, then concatenating in the exact order below.
// String fields are UTF-8 bytes. Numbers are 8-byte big-endian. UUIDs use
// their 16-byte binary form. Lists and the target pass through
// canonical.MarshalJSON so the serialization is order-stable. The prev_hash
// for the first row of a session is 32 zero bytes.
//
// Field order:
//  1. sequence            (int64, 8 bytes BE)
//  2. prev_hash           (32 bytes, zero-padded for first row)
//  3. timestamp_unix_ns   (int64, 8 bytes BE)
//  4. session_id          (16 bytes)
//  5. event_id            (16 bytes, all zero when unset)
//  6. entry_id            (16 bytes)
//  7. kind                (utf-8 bytes)
//  8. action_type         (utf-8 bytes)
//  9. tool                (utf-8 bytes)
//  10. tool_call_id       (utf-8 bytes)
//  11. linked_event_id    (16 bytes, all zero when unset)
//  12. phase              (utf-8 bytes)
//  13. target             (canonical JSON of host, mcp_server, mcp_tool)
//  14. origin             (utf-8 bytes)
//  15. tags               (canonical JSON of []string, "null" when empty)
//  16. classifications    (canonical JSON of []string, "null" when empty)
//  17. injection_score    (int64 bits of the float32 widened to float64,
//     8 bytes BE; 0 when unset)
//  18. decision           (utf-8 bytes)
//  19. matched_rule_ids   (canonical JSON of []string, "null" when empty)
//  20. content_digest     (utf-8 bytes)
//
// Version 1 hashed the old aarm_context_actions rows. The migration to
// context_entries drops those rows, so no version 1 row exists and the
// verifier reports any other version as a break.
//
// # Stability contract
//
// result_status, duration_ms, and error_message are not part of the hash
// input. The post hook sets them after the insert, and they would otherwise
// force a re-hash on every result update. The chain attests to the
// as-mediated entry, not the post-hook outcome. This mirrors the receipt
// chain's split between insert-time hash inputs and post-hook mutations.
package contextchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/canonical"
)

// HashSize is the byte length of a context chain hash (SHA-256).
const HashSize = 32

// Version is the hash version that ComputeHash writes.
const Version = 2

// Target is the derived target of an entry.
type Target struct {
	Host      string
	MCPServer string
	MCPTool   string
}

// Input collects every value that participates in the context chain hash.
// The storage layer constructs one per insert, hands it to ComputeHash, and
// persists both the input fields and the resulting hash.
type Input struct {
	Sequence          int64
	PrevHash          []byte
	TimestampUnixNano int64
	SessionID         uuid.UUID
	EventID           uuid.UUID
	EntryID           uuid.UUID
	Kind              string
	ActionType        string
	Tool              string
	ToolCallID        string
	LinkedEventID     uuid.UUID
	Phase             string
	Target            Target
	Origin            string
	Tags              []string
	Classifications   []string
	InjectionScore    float32
	Decision          string
	MatchedRuleIDs    []string
	ContentDigest     string
}

// Row is the minimal subset of entry fields needed to re-derive a
// per-session hash chain. Verifiers convert their native representation to
// a slice of Row.
type Row struct {
	SessionID uuid.UUID
	Sequence  int64
	Version   int
	PrevHash  []byte
	Hash      []byte
	Fields    Input
}

// ComputeHash returns the SHA-256 of the canonical serialization of in. See
// package doc for the serialization format.
func ComputeHash(in Input) ([]byte, error) {
	var buf bytes.Buffer

	writeInt64(&buf, in.Sequence)

	prev := in.PrevHash
	if len(prev) == 0 {
		prev = make([]byte, HashSize)
	}
	if len(prev) != HashSize {
		return nil, fmt.Errorf("contextchain: prev_hash must be %d bytes, got %d", HashSize, len(prev))
	}
	writeBytes(&buf, prev)
	writeInt64(&buf, in.TimestampUnixNano)
	writeUUID(&buf, in.SessionID)
	writeUUID(&buf, in.EventID)
	writeUUID(&buf, in.EntryID)
	for _, s := range []string{in.Kind, in.ActionType, in.Tool, in.ToolCallID} {
		writeString(&buf, s)
	}
	writeUUID(&buf, in.LinkedEventID)
	writeString(&buf, in.Phase)

	target, err := canonical.MarshalJSON(map[string]string{
		"host": in.Target.Host, "mcp_server": in.Target.MCPServer, "mcp_tool": in.Target.MCPTool,
	})
	if err != nil {
		return nil, fmt.Errorf("contextchain: canonicalize target: %w", err)
	}
	writeBytes(&buf, target)
	writeString(&buf, in.Origin)

	if err := writeJSON(&buf, "tags", in.Tags); err != nil {
		return nil, err
	}
	if err := writeJSON(&buf, "classifications", in.Classifications); err != nil {
		return nil, err
	}

	var scoreBits int64
	if in.InjectionScore != 0 {
		scoreBits = int64(math.Float64bits(float64(in.InjectionScore)))
	}
	writeInt64(&buf, scoreBits)
	writeString(&buf, in.Decision)
	if err := writeJSON(&buf, "matched_rule_ids", in.MatchedRuleIDs); err != nil {
		return nil, err
	}
	writeString(&buf, in.ContentDigest)

	sum := sha256.Sum256(buf.Bytes())
	return sum[:], nil
}

func writeJSON(buf *bytes.Buffer, name string, v []string) error {
	b, err := canonical.MarshalJSON(v)
	if err != nil {
		return fmt.Errorf("contextchain: canonicalize %s: %w", name, err)
	}
	writeBytes(buf, b)
	return nil
}

// The writers append to a bytes.Buffer, whose Write never returns an error.
func writeInt64(buf *bytes.Buffer, v int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	buf.Write(b[:])
}

func writeBytes(buf *bytes.Buffer, b []byte) {
	var l [8]byte
	binary.BigEndian.PutUint64(l[:], uint64(len(b)))
	buf.Write(l[:])
	buf.Write(b)
}

func writeString(buf *bytes.Buffer, s string) {
	writeBytes(buf, []byte(s))
}

func writeUUID(buf *bytes.Buffer, id uuid.UUID) {
	writeBytes(buf, id[:])
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

		if reason := hashBreak(r); reason != "" {
			breaks = append(breaks, Break{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    reason,
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

// hashBreak checks the stored hash of a row. It checks the version first,
// because a row of another version has fields that ComputeHash cannot read.
func hashBreak(r Row) string {
	if r.Version != Version {
		return fmt.Sprintf("unknown hash version %d", r.Version)
	}
	expected, err := ComputeHash(r.Fields)
	if err != nil {
		return fmt.Sprintf("recompute hash: %v", err)
	}
	if !bytes.Equal(expected, r.Hash) {
		return "stored hash does not match recomputed hash"
	}
	return ""
}

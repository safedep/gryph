package receipt

import (
	"bytes"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/storage"
)

// ChainBreak is a single integrity failure surfaced by VerifyChain. The
// (SessionID, Sequence) pair identifies the offending receipt.
type ChainBreak struct {
	SessionID uuid.UUID
	Sequence  int64
	Reason    string
}

// ChainRow is the minimal subset of receipt fields VerifyChain needs to
// re-derive a per-session hash chain. Each call site (DB rows, exported JSONL
// rows) converts its native representation to a slice of ChainRow.
type ChainRow struct {
	SessionID uuid.UUID
	Sequence  int64
	PrevHash  []byte
	Hash      []byte
	Fields    HashInputFields
}

// ChainRowFromReceipt builds a ChainRow from a storage.ReceiptRow.
func ChainRowFromReceipt(r *storage.ReceiptRow) ChainRow {
	return ChainRow{
		SessionID: r.SessionID,
		Sequence:  r.Sequence,
		PrevHash:  r.PrevHash,
		Hash:      r.Hash,
		Fields: HashInputFields{
			Sequence:       r.Sequence,
			PrevHash:       r.PrevHash,
			RecordedAtUnix: r.RecordedAt.UnixNano(),
			SessionID:      r.SessionID,
			ActionID:       r.ActionID,
			EventID:        r.EventID,
			Agent:          r.Agent,
			Tool:           r.Tool,
			ActionType:     r.ActionType,
			Project:        r.Project,
			Decision:       r.Decision,
			Severity:       r.Severity,
			Message:        r.Message,
			MatchedRuleIDs: r.MatchedRuleIDs,
			Snapshot:       r.Snapshot,
			ActionPayload:  r.ActionPayload,
			SubagentID:     r.SubagentID,
			SubagentType:   r.SubagentType,
			PolicyHash:     r.PolicyHash,
		},
	}
}

// VerifyChain re-derives the per-session hash chain over rows and returns any
// integrity failures. The caller must pass rows for a single session sorted
// ascending by Sequence. The check covers: sequence starts at 1 and increases
// monotonically, the first receipt's prev_hash is the zero state, every other
// prev_hash matches the previous row's hash, and each stored hash matches the
// hash recomputed from its fields.
func VerifyChain(rows []ChainRow) []ChainBreak {
	var breaks []ChainBreak
	var prevHash []byte
	var prevSeq int64
	for _, r := range rows {
		expectedSeq := prevSeq + 1
		if prevSeq == 0 && r.Sequence != 1 {
			breaks = append(breaks, ChainBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("first receipt sequence is %d, expected 1", r.Sequence),
			})
		} else if prevSeq > 0 && r.Sequence != expectedSeq {
			breaks = append(breaks, ChainBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("sequence gap: got %d, expected %d", r.Sequence, expectedSeq),
			})
		}

		if prevSeq == 0 {
			if len(r.PrevHash) > 0 {
				zero := bytes.Repeat([]byte{0}, HashSize)
				if !bytes.Equal(r.PrevHash, zero) {
					breaks = append(breaks, ChainBreak{
						SessionID: r.SessionID,
						Sequence:  r.Sequence,
						Reason:    "first receipt prev_hash is not the zero state",
					})
				}
			}
		} else if !bytes.Equal(r.PrevHash, prevHash) {
			breaks = append(breaks, ChainBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "prev_hash does not match previous row hash",
			})
		}

		expectedHash, err := ComputeHash(NewHashInput(r.Fields))
		if err != nil {
			breaks = append(breaks, ChainBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("recompute hash: %v", err),
			})
		} else if !bytes.Equal(expectedHash, r.Hash) {
			breaks = append(breaks, ChainBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "stored hash does not match recomputed hash",
			})
		}

		prevHash = r.Hash
		prevSeq = r.Sequence
	}
	return breaks
}

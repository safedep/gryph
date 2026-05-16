// Package receipt builds and stores the AARM receipt log: an append-only,
// hash-chained record of every mediated action that produced a non-skip
// decision (plus optional allow logging when policy.log_all_evaluations is
// true). Receipts outlive the originating session and audit event.
//
// # Hash canonicalization
//
// ComputeHash builds the SHA-256 input by length-prefixing each field with
// an 8-byte big-endian length, then concatenating in the exact order below.
// String fields are encoded as UTF-8. Numbers are encoded as 8-byte
// big-endian. UUIDs use their 16-byte binary form. matched_rule_ids is a
// sorted-key-canonical JSON array. JSON objects (snapshot, action_payload)
// are passed through canonical.MarshalJSON: keys are recursively sorted,
// arrays keep order, scalars are encoded by encoding/json, and nil values
// serialize to the literal "null". The final SHA-256 is the value stored in
// the receipt's hash column. The prev_hash for the first receipt of a
// session is 32 zero bytes.
//
// Field order (canonical):
//  1. sequence            (int64, 8 bytes BE)
//  2. prev_hash           (32 bytes, zero-padded for first row)
//  3. recorded_at_unix_ns (int64, 8 bytes BE)
//  4. session_id          (16 bytes)
//  5. action_id           (16 bytes, all zero when unset)
//  6. event_id            (16 bytes, all zero when unset)
//  7. agent               (utf-8 bytes)
//  8. tool                (utf-8 bytes)
//  9. action_type         (utf-8 bytes)
//  10. project            (utf-8 bytes)
//  11. decision           (utf-8 bytes)
//  12. severity           (utf-8 bytes)
//  13. message            (utf-8 bytes)
//  14. matched_rule_ids   (canonical JSON of []string, "null" when empty)
//  15. result_status      (utf-8 bytes)
//  16. duration_ms        (int64, 8 bytes BE; 0 when unset)
//  17. error_message      (utf-8 bytes)
//  18. snapshot           (canonical JSON, "null" when nil)
//  19. action_payload     (canonical JSON, "null" when nil)
//  20. subagent_id        (utf-8 bytes; empty when not a subagent action)
//  21. subagent_type      (utf-8 bytes; empty when not a subagent action)
//  22. policy_hash        (length-prefixed bytes; empty for pre-Phase-4.5 rows)
//
// result_status contract
//
// At insert time, result_status is derived solely from the decision: it is
// "blocked" when the decision is "block" and "pending" otherwise. This
// derivation is the value hashed into the row. duration_ms and
// error_message are likewise zero/empty at insert time. The verifier in
// cli/policy_receipts.go re-applies the same derivation when re-computing
// the hash so insert-time and verify-time hash inputs always agree.
// UpdateReceiptResult subsequently mutates result_status to its terminal
// value (e.g. "success") but does not re-hash the row. Verifying the hash
// chain therefore checks the as-recorded decision, not the post-hook
// outcome.
//
// decision contract (Phase 3)
//
// Approval-outcome decisions (approved, denied, approval_timeout) are
// written to the decision column post-insert by the Mediator's escalate
// path. They are NOT produced by the PDP. For hash-chain stability the
// insert-time decision used in the hash input is always the PDP's original
// value: DeriveInsertDecision collapses approval-outcome values back to
// "escalate" so the verifier re-derives the same hash regardless of which
// outcome eventually landed in the column.
package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/aarm/model"
)

// HashSize is the byte length of a receipt hash (SHA-256).
const HashSize = 32

// resultStatusPending is the insert-time result_status assigned to any
// non-block decision. The storage layer carries an equivalent constant
// (contextResultStatusPending); kept local here so aarm/receipt does not
// import storage. The string value is the load-bearing contract.
const resultStatusPending = "pending"

// HashInput collects every byte that participates in the receipt hash. The
// receipt generator constructs one of these per insert, computes the hash,
// and persists both the input and its hash.
type HashInput struct {
	Sequence       int64
	PrevHash       []byte
	RecordedAtUnix int64
	SessionID      uuid.UUID
	ActionID       uuid.UUID
	EventID        uuid.UUID
	Agent          string
	Tool           string
	ActionType     string
	Project        string
	Decision       string
	Severity       string
	Message        string
	MatchedRuleIDs []string
	ResultStatus   string
	DurationMS     int64
	ErrorMessage   string
	Snapshot       map[string]interface{}
	ActionPayload  map[string]interface{}
	SubagentID     string
	SubagentType   string
	PolicyHash     []byte
}

// DeriveInsertResultStatus returns the insert-time result_status implied by
// the policy decision. Blocked decisions short-circuit to "blocked", every
// other decision starts as "pending" and is later promoted by
// UpdateReceiptResult. Used by both the insert path and the verifier so the
// hash inputs always agree.
func DeriveInsertResultStatus(decision string) string {
	if decision == string(model.DecisionBlock) {
		return string(model.ResultBlocked)
	}
	return resultStatusPending
}

// DeriveInsertDecision returns the insert-time decision implied by the
// stored decision. Approval-outcome values (approved, denied,
// approval_timeout) collapse to "escalate" so the hash chain remains stable
// across post-hook decision mutation. Used by both the insert path and the
// verifier so the hash inputs always agree.
func DeriveInsertDecision(decision string) string {
	switch decision {
	case DecisionApproved, DecisionDenied, DecisionApprovalTimeout:
		return string(model.DecisionEscalate)
	}
	return decision
}

// Decision values written to the receipt's decision column post-approval.
// They are not produced by the PDP. The Mediator synthesizes them when the
// Approval Service returns an outcome.
const (
	DecisionApproved        = "approved"
	DecisionDenied          = "denied"
	DecisionApprovalTimeout = "approval_timeout"
)

// HashInputFields collects the row-level inputs that feed NewHashInput. It
// matches the persisted ReceiptRow shape minus the canonical zeroing the
// constructor applies (duration_ms, error_message). The caller passes the raw
// fields; NewHashInput owns the derivation rules.
type HashInputFields struct {
	Sequence       int64
	PrevHash       []byte
	RecordedAtUnix int64
	SessionID      uuid.UUID
	ActionID       uuid.UUID
	EventID        uuid.UUID
	Agent          string
	Tool           string
	ActionType     string
	Project        string
	Decision       string
	Severity       string
	Message        string
	MatchedRuleIDs []string
	Snapshot       map[string]interface{}
	ActionPayload  map[string]interface{}
	SubagentID     string
	SubagentType   string
	PolicyHash     []byte
}

// NewHashInput builds a *HashInput from the explicit row fields, applying the
// canonical zeroing rules: Decision is derived from f.Decision via
// DeriveInsertDecision (so approval-outcome values collapse to "escalate"),
// ResultStatus is derived from the same insert-time decision via
// DeriveInsertResultStatus, DurationMS is set to 0, and ErrorMessage is
// emptied. Single source of truth for the as-recorded hash input shape.
func NewHashInput(f HashInputFields) *HashInput {
	insertDecision := DeriveInsertDecision(f.Decision)
	return &HashInput{
		Sequence:       f.Sequence,
		PrevHash:       f.PrevHash,
		RecordedAtUnix: f.RecordedAtUnix,
		SessionID:      f.SessionID,
		ActionID:       f.ActionID,
		EventID:        f.EventID,
		Agent:          f.Agent,
		Tool:           f.Tool,
		ActionType:     f.ActionType,
		Project:        f.Project,
		Decision:       insertDecision,
		Severity:       f.Severity,
		Message:        f.Message,
		MatchedRuleIDs: f.MatchedRuleIDs,
		ResultStatus:   DeriveInsertResultStatus(insertDecision),
		DurationMS:     0,
		ErrorMessage:   "",
		Snapshot:       f.Snapshot,
		ActionPayload:  f.ActionPayload,
		SubagentID:     f.SubagentID,
		SubagentType:   f.SubagentType,
		PolicyHash:     f.PolicyHash,
	}
}

// ComputeHash returns the SHA-256 of the canonical serialization of in.
// See package doc for the serialization format.
func ComputeHash(in *HashInput) ([]byte, error) {
	if in == nil {
		return nil, fmt.Errorf("receipt: nil hash input")
	}
	var buf bytes.Buffer

	if err := writeInt64(&buf, in.Sequence); err != nil {
		return nil, err
	}

	prev := in.PrevHash
	if len(prev) == 0 {
		prev = make([]byte, HashSize)
	}
	if len(prev) != HashSize {
		return nil, fmt.Errorf("receipt: prev_hash must be %d bytes, got %d", HashSize, len(prev))
	}
	if err := writeBytes(&buf, prev); err != nil {
		return nil, err
	}

	if err := writeInt64(&buf, in.RecordedAtUnix); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.SessionID); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.ActionID); err != nil {
		return nil, err
	}
	if err := writeUUID(&buf, in.EventID); err != nil {
		return nil, err
	}

	for _, s := range []string{
		in.Agent, in.Tool, in.ActionType, in.Project,
		in.Decision, in.Severity, in.Message,
	} {
		if err := writeString(&buf, s); err != nil {
			return nil, err
		}
	}

	ruleIDs, err := canonical.MarshalJSON(in.MatchedRuleIDs)
	if err != nil {
		return nil, fmt.Errorf("receipt: canonicalize matched_rule_ids: %w", err)
	}
	if err := writeBytes(&buf, ruleIDs); err != nil {
		return nil, err
	}

	if err := writeString(&buf, in.ResultStatus); err != nil {
		return nil, err
	}
	if err := writeInt64(&buf, in.DurationMS); err != nil {
		return nil, err
	}
	if err := writeString(&buf, in.ErrorMessage); err != nil {
		return nil, err
	}

	snap, err := canonical.MarshalJSON(in.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("receipt: canonicalize snapshot: %w", err)
	}
	if err := writeBytes(&buf, snap); err != nil {
		return nil, err
	}

	payload, err := canonical.MarshalJSON(in.ActionPayload)
	if err != nil {
		return nil, fmt.Errorf("receipt: canonicalize action_payload: %w", err)
	}
	if err := writeBytes(&buf, payload); err != nil {
		return nil, err
	}

	if err := writeString(&buf, in.SubagentID); err != nil {
		return nil, err
	}
	if err := writeString(&buf, in.SubagentType); err != nil {
		return nil, err
	}
	if err := writeBytes(&buf, in.PolicyHash); err != nil {
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

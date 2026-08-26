package receipt

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
)

// ErrInsert is the sentinel returned (wrapped) when the receipt generator
// fails to persist a row. The lazyPolicyCheck self-audit wiring uses
// errors.Is to detect insert failures without coupling to the concrete
// cause.
var ErrInsert = errors.New("receipt insert")

// Generator records mediated actions to the append-only receipt log and
// updates result_status post-hook. Implementations must be safe for
// concurrent calls across sessions. Within a single session, Record must
// atomically read the previous (sequence, hash) and insert the next row.
//
// UpdateDecision mutates the receipt's decision and result_status to the
// approval outcome. The hash column is NOT recomputed; the hash input
// always collapses approval-outcome decisions back to "escalate" via
// DeriveInsertDecision so the chain stays verifiable.
type Generator interface {
	Record(ctx context.Context, in *RecordInput) (*Record, error)
	UpdateResult(ctx context.Context, sessionID uuid.UUID, sequence int64, result model.Result) error
	UpdateDecision(ctx context.Context, sessionID uuid.UUID, sequence int64, decision string, resultStatus string, note string) error
}

// RecordInput is the input to Generator.Record.
//
// result_status is not part of this input. It is derived from the decision
// inside Generator.Record: "blocked" for a block decision, "deferred" for a
// defer decision, "pending" otherwise. The same derivation is used by the
// verifier when re-computing the row hash, so the insert-time and verify-time
// hash inputs always agree.
type RecordInput struct {
	SessionID uuid.UUID
	ActionID  uuid.UUID
	EventID   uuid.UUID
	Action    *model.Action
	Snapshot  *model.ContextSnapshot
	Decision  *model.EvaluationResult
	Agent     string

	// RecordedAt overrides the row's recorded_at. Default: time.Now().UTC().
	RecordedAt time.Time

	// PolicyHash is the SHA-256 over the policy document this action was
	// evaluated under. Persisted on the row and folded into the receipt hash
	// so a policy edit is visible at verify time.
	PolicyHash []byte

	// DeferReason populates the receipt's defer_reason column. Set by the
	// Mediator when persisting a defer decision.
	DeferReason string

	// DeferralOfSequence ties a follow-up resolution receipt to the original
	// defer receipt's sequence within the same session. Nil on the original
	// defer row and on non-resolution receipts.
	DeferralOfSequence *int64

	// ErrorMessage is an optional explanation persisted on the receipt row's
	// error_message column at insert time. The hash chain ignores
	// error_message (the verifier zeros it via DeriveInsertResultStatus), so
	// setting this changes only the row write, not the row's hash. Used by
	// pre-PDP block paths that want to record the failure reason without a
	// second UpdateResult round trip.
	ErrorMessage string
}

// Record summarizes a successful Generator.Record call so the Mediator can
// thread (sessionID, sequence) into the post-hook RecordResult call.
type Record struct {
	ID         uuid.UUID
	Sequence   int64
	RecordedAt time.Time
	Hash       []byte
	PrevHash   []byte

	// SignerKeyID is non-empty when the row was signed at insert time. The
	// CLI uses this to emit a SelfAuditActionReceiptSigned row without
	// re-querying the receipt.
	SignerKeyID string
}

// GeneratorOption configures a Generator at construction time. Today the
// SQLite-backed generator is the only consumer; the Nop generator ignores
// options so callers can pass them unconditionally.
type GeneratorOption func(*generatorConfig)

// generatorConfig collects optional dependencies shared by Generator
// implementations. Kept un-exported to keep the option surface small.
type generatorConfig struct {
	signer Signer
}

// WithSigner wires a Signer into a Generator. When set, every receipt insert
// also produces and stores the signature column. When nil, signing is
// disabled (the same as the default).
func WithSigner(s Signer) GeneratorOption {
	return func(c *generatorConfig) {
		if s != nil {
			c.signer = s
		}
	}
}

// Nop is a no-op Generator. Used when no store is wired so the Mediator
// surface stays stable.
type Nop struct{}

// NewNop returns a no-op Generator.
func NewNop() *Nop { return &Nop{} }

// Record implements Generator.
func (*Nop) Record(_ context.Context, _ *RecordInput) (*Record, error) { return &Record{}, nil }

// UpdateResult implements Generator.
func (*Nop) UpdateResult(_ context.Context, _ uuid.UUID, _ int64, _ model.Result) error {
	return nil
}

// UpdateDecision implements Generator.
func (*Nop) UpdateDecision(_ context.Context, _ uuid.UUID, _ int64, _, _, _ string) error {
	return nil
}

var _ Generator = (*Nop)(nil)

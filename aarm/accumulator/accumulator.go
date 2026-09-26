// Package accumulator defines the AARM Context Accumulator surface: the
// per-session memory that the Policy Decision Point consults via
// context.* CEL variables.
//
// The package name is "accumulator" rather than the AARM spec's "context" to
// avoid shadowing the stdlib context package at every call site.
package accumulator

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
)

// ErrSnapshot is the sentinel returned (wrapped) when the Context Accumulator
// fails to produce a snapshot for the PDP. Call sites use errors.Is to detect
// snapshot failures and record a self-audit entry without coupling to the
// concrete cause.
var ErrSnapshot = errors.New("accumulator snapshot")

// ErrAppend is the sentinel returned (wrapped) when the Context Accumulator
// fails to write an entry and the decision lets the action run. Without the
// entry, later rules read incomplete session state, so the fail mode decides.
var ErrAppend = errors.New("accumulator append")

// Accumulator records the session context and produces the point-in-time
// snapshot that the PDP reads.
//
// Method semantics:
//   - Snapshot is called before PDP evaluation. It returns the stored
//     state of the session with the pending entry added in memory, so a
//     rule sees the current event in the counters.
//   - Append is called once after evaluation, with the entry that carries
//     the decision. It writes the entry and the state change in one
//     transaction.
//   - RecordResult is called with the execution outcome of an entry. It
//     does not re-run the PDP.
//   - ConfirmIntent is called when an approval lets an escalated entry
//     reach the agent. An intent entry then becomes the latest intent.
//     Other entries do not change.
//
// Implementations must be safe for concurrent calls across sessions. A
// Snapshot error propagates to the Mediator and is subject to the security
// evaluator's fail-open / fail-closed policy. An Append error propagates
// the same way when the action runs. The Mediator only logs an Append error
// when the action does not run, and it only logs a RecordResult error. So an
// accumulator error never turns a block into an allow under fail_mode open.
type Accumulator interface {
	Snapshot(ctx context.Context, sessionID uuid.UUID, pending *model.ContextEntry) (*model.ContextSnapshot, error)
	Append(ctx context.Context, entry *model.ContextEntry) error
	RecordResult(ctx context.Context, entryID uuid.UUID, result model.Result) error
	ConfirmIntent(ctx context.Context, entryID uuid.UUID) error
}

// Nop is a no-op Accumulator: Append and RecordResult succeed silently and
// Snapshot returns an empty snapshot.
type Nop struct{}

// NewNop returns a no-op Accumulator.
func NewNop() *Nop { return &Nop{} }

// Snapshot implements Accumulator. Each call returns a new snapshot, so a
// caller can change it.
func (*Nop) Snapshot(_ context.Context, _ uuid.UUID, _ *model.ContextEntry) (*model.ContextSnapshot, error) {
	return &model.ContextSnapshot{}, nil
}

// Append implements Accumulator.
func (*Nop) Append(_ context.Context, _ *model.ContextEntry) error { return nil }

// RecordResult implements Accumulator.
func (*Nop) RecordResult(_ context.Context, _ uuid.UUID, _ model.Result) error { return nil }

// ConfirmIntent implements Accumulator.
func (*Nop) ConfirmIntent(_ context.Context, _ uuid.UUID) error { return nil }

var _ Accumulator = (*Nop)(nil)

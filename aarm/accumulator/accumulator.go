// Package accumulator defines the AARM Context Accumulator surface: the
// per-session memory that the Policy Decision Point consults via
// context.* CEL variables.
//
// The package name is "accumulator" rather than the AARM spec's "context" to
// avoid shadowing the stdlib context package at every call site.
//
// This first cut nails down the interface and ships a Nop implementation. A
// SQLite-backed accumulator persisting to dedicated aarm_context_* tables
// (per docs/security-spec.md section 7) will plug in later without changing callers.
package accumulator

import (
	"context"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
)

// Accumulator records mediated actions for a session and produces the
// point-in-time snapshot consumed by the PDP.
//
// Method semantics:
//   - Append is called before PDP evaluation with the canonical Action.
//   - Snapshot is called immediately after Append. It returns a read-only
//     view of the accumulated state for this session (counters, distinct
//     tools, reserved classification/entity sets).
//   - RecordResult is called post-hook with the action's execution outcome.
//     It updates result-derived counters (e.g. errors) for that action row
//     and does not re-run the PDP.
//
// Implementations must be safe for concurrent calls across sessions. Within
// a single session, Append and Snapshot are separate calls and implementations
// are responsible for any ordering / atomicity guarantees the PDP needs
// (e.g. that the Snapshot a Check observes reflects its own Append and not
// a racing one). Errors returned propagate to the Mediator and are subject
// to the security evaluator's fail-open / fail-closed policy.
type Accumulator interface {
	Append(ctx context.Context, action *model.Action) error
	RecordResult(ctx context.Context, actionID uuid.UUID, result model.Result) error
	Snapshot(ctx context.Context, sessionID uuid.UUID) (*model.ContextSnapshot, error)
}

// Nop is a no-op Accumulator: Append and RecordResult succeed silently and
// Snapshot returns an empty snapshot. Used as the default until a persistent
// implementation lands so the Mediator wiring is stable from day one.
type Nop struct{}

// NewNop returns a no-op Accumulator.
func NewNop() *Nop { return &Nop{} }

// Append implements Accumulator.
func (*Nop) Append(_ context.Context, _ *model.Action) error { return nil }

// RecordResult implements Accumulator.
func (*Nop) RecordResult(_ context.Context, _ uuid.UUID, _ model.Result) error { return nil }

// Snapshot implements Accumulator. The returned snapshot is freshly allocated;
// callers may mutate without affecting subsequent calls.
func (*Nop) Snapshot(_ context.Context, _ uuid.UUID) (*model.ContextSnapshot, error) {
	return &model.ContextSnapshot{}, nil
}

var _ Accumulator = (*Nop)(nil)

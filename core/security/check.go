package security

import (
	"context"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// CheckResult represents the result of a single security check.
type CheckResult struct {
	// Decision is the outcome of this check.
	Decision Decision
	// Reason is required for Block decisions, explaining why the action was blocked.
	Reason string
	// Guidance is optional advisory text for the agent.
	Guidance string
	// CheckName identifies which check produced this result.
	CheckName string
	// MatchedRuleIDs lists policy rule identifiers that produced this decision.
	// Empty for checks that don't have a rule-based model.
	MatchedRuleIDs []string
	// Severity classifies the seriousness of the decision. Optional.
	Severity Severity
	// Tags carries free-form labels attached by the rule (e.g. "compliance", "pii").
	Tags []string

	// AarmSessionID and AarmSequence reference the receipt row produced by
	// the AARM mediator for this check, if any. cli/hook.go uses them on
	// the allow path to record the post-hook result without re-querying.
	AarmSessionID uuid.UUID
	AarmSequence  int64
	// AarmActionID is the canonical action ID assigned by the Mediator
	// during normalization. cli/hook.go uses it post-hook to flip the
	// accumulator's context_action result_status from pending to final.
	AarmActionID uuid.UUID
}

// Check defines the interface for security checks.
type Check interface {
	// Name returns the unique identifier for this check.
	Name() string
	// Check evaluates the event and returns a result.
	Check(ctx context.Context, event *events.Event) (*CheckResult, error)
	// Enabled returns whether this check is currently active.
	Enabled() bool
}

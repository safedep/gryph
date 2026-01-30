package security

import (
	"context"

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

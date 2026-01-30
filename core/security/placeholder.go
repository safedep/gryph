package security

import (
	"context"

	"github.com/safedep/gryph/core/events"
)

// PlaceholderCheck is a no-op check that always allows actions.
// It serves as the initial implementation and can be used for testing.
type PlaceholderCheck struct{}

// NewPlaceholderCheck creates a new PlaceholderCheck.
func NewPlaceholderCheck() *PlaceholderCheck {
	return &PlaceholderCheck{}
}

// Name returns the check identifier.
func (c *PlaceholderCheck) Name() string {
	return "placeholder"
}

// Check always returns an Allow decision.
func (c *PlaceholderCheck) Check(ctx context.Context, event *events.Event) (*CheckResult, error) {
	return &CheckResult{
		Decision:  DecisionAllow,
		CheckName: c.Name(),
	}, nil
}

// Enabled returns true - placeholder check is always enabled.
func (c *PlaceholderCheck) Enabled() bool {
	return true
}

// Ensure PlaceholderCheck implements Check
var _ Check = (*PlaceholderCheck)(nil)

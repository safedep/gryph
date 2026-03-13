package cost

import "context"

// TokenCollector extracts token usage from an agent session.
type TokenCollector interface {
	// Collect returns token usage for a session.
	// transcriptPath may be empty if unavailable.
	// Returns nil, nil if no usage data is available.
	Collect(ctx context.Context, transcriptPath string) (*SessionUsage, error)

	// Source returns the data source this collector reads from.
	Source() CostSource
}

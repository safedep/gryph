package security

import (
	"strings"

	"github.com/google/uuid"
)

// Result aggregates results from all security checks.
type Result struct {
	// FinalDecision is the overall decision after evaluating all checks.
	FinalDecision Decision
	// BlockReason is the reason if the action was blocked.
	BlockReason string
	// BlockedBy is the name of the check that blocked the action.
	BlockedBy string
	// Guidance contains aggregated guidance from all checks.
	Guidance []string
	// CheckResults contains the individual results from each check.
	CheckResults []*CheckResult
	// Error contains any error that occurred during evaluation.
	Error error
}

// NewAllowResult creates a new Result with an Allow decision.
func NewAllowResult() *Result {
	return &Result{
		FinalDecision: DecisionAllow,
		CheckResults:  make([]*CheckResult, 0),
		Guidance:      make([]string, 0),
	}
}

// IsAllowed returns true if the action is allowed (not blocked).
func (r *Result) IsAllowed() bool {
	return r.FinalDecision != DecisionBlock
}

// HasGuidance returns true if there is any guidance to provide.
func (r *Result) HasGuidance() bool {
	return len(r.Guidance) > 0
}

// AggregatedGuidance returns all guidance joined with newlines.
func (r *Result) AggregatedGuidance() string {
	return strings.Join(r.Guidance, "\n")
}

// AarmRef returns the (actionID, sessionID, sequence) from the AARM check
// result in this evaluation, if any. The post-hook caller uses these to
// record the action's execution outcome on the accumulator (by actionID)
// and the receipt (by sessionID+sequence) without re-querying. Returns
// zero values when the AARM mediator did not participate.
func (r *Result) AarmRef() (uuid.UUID, uuid.UUID, int64) {
	if r == nil {
		return uuid.Nil, uuid.Nil, 0
	}
	for _, c := range r.CheckResults {
		if c == nil {
			continue
		}
		if c.AarmActionID != uuid.Nil || c.AarmSessionID != uuid.Nil {
			return c.AarmActionID, c.AarmSessionID, c.AarmSequence
		}
	}
	return uuid.Nil, uuid.Nil, 0
}

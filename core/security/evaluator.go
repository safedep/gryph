package security

import (
	"context"
	"fmt"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
)

// Config holds configuration options for the security evaluator.
type Config struct {
	// FailOpen determines behavior when a check returns an error.
	FailOpen bool
}

// Evaluator orchestrates security checks against events.
type Evaluator struct {
	checks []Check
	config *Config
}

// New creates a new security Evaluator with the given configuration.
func New(cfg *Config) *Evaluator {
	if cfg == nil {
		cfg = &Config{FailOpen: true}
	}
	return &Evaluator{
		checks: make([]Check, 0),
		config: cfg,
	}
}

// RegisterCheck adds a security check to the evaluator.
func (e *Evaluator) RegisterCheck(check Check) {
	e.checks = append(e.checks, check)
}

// Evaluate runs all registered checks against the event and returns an aggregated result.
// Checks are evaluated in order, and evaluation stops immediately on a Block decision.
func (e *Evaluator) Evaluate(ctx context.Context, event *events.Event) *Result {
	result := NewAllowResult()

	for _, check := range e.checks {
		if !check.Enabled() {
			continue
		}

		checkResult, err := check.Check(ctx, event)
		if err != nil {
			if e.config.FailOpen {
				log.Warnf("check %s failed with error: %v, but failing open", check.Name(), err)
				continue
			}

			result.FinalDecision = DecisionBlock
			result.BlockReason = fmt.Sprintf("check %s failed: %v", check.Name(), err)
			result.BlockedBy = check.Name()
			result.Error = err

			return result
		}

		result.CheckResults = append(result.CheckResults, checkResult)

		switch checkResult.Decision {
		case DecisionBlock:
			result.FinalDecision = DecisionBlock
			result.BlockReason = checkResult.Reason
			result.BlockedBy = checkResult.CheckName
			return result
		case DecisionGuidance:
			if checkResult.Guidance != "" {
				result.Guidance = append(result.Guidance, checkResult.Guidance)
			}
		}
	}

	if len(result.Guidance) > 0 {
		result.FinalDecision = DecisionGuidance
	}

	return result
}

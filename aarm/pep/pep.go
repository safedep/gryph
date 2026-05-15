// Package pep maps AARM PDP decisions to Gryph security check results.
package pep

import (
	"github.com/safedep/gryph/aarm/model"
	coresecurity "github.com/safedep/gryph/core/security"
)

// CheckName is the security.Check name set on every CheckResult emitted via
// the PEP boundary. The aarm package re-exports it so adapters and CLI
// wrappers can refer to a single source of truth.
const CheckName = "aarm-pdp"

// Apply converts a PDP decision into a security.CheckResult. MatchedRuleIDs,
// Severity, and Tags from the PDP are propagated so audit, receipts, and UX
// can reference rule identity directly instead of parsing it out of the
// rendered message.
func Apply(result *model.EvaluationResult) *coresecurity.CheckResult {
	if result == nil {
		return allow()
	}

	out := &coresecurity.CheckResult{
		CheckName:      CheckName,
		MatchedRuleIDs: result.MatchedRuleIDs,
		Severity:       mapSeverity(result.Severity),
		Tags:           result.Tags,
	}

	message := result.Message
	if message == "" && result.Decision == model.DecisionBlock {
		message = "Blocked by policy"
	}

	switch result.Decision {
	case model.DecisionBlock:
		out.Decision = coresecurity.DecisionBlock
		out.Reason = message
	case model.DecisionGuidance, model.DecisionWarn:
		out.Decision = coresecurity.DecisionGuidance
		out.Guidance = message
	default:
		out.Decision = coresecurity.DecisionAllow
	}
	return out
}

func mapSeverity(s model.Severity) coresecurity.Severity {
	switch s {
	case model.SeverityCritical:
		return coresecurity.SeverityCritical
	case model.SeverityHigh:
		return coresecurity.SeverityHigh
	case model.SeverityMedium:
		return coresecurity.SeverityMedium
	case model.SeverityLow:
		return coresecurity.SeverityLow
	case model.SeverityInfo:
		return coresecurity.SeverityInfo
	default:
		return coresecurity.SeverityUnspecified
	}
}

func allow() *coresecurity.CheckResult {
	return &coresecurity.CheckResult{
		Decision:  coresecurity.DecisionAllow,
		CheckName: CheckName,
	}
}

// Package pep maps AARM PDP decisions to Gryph security check results.
package pep

import (
	"cmp"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/model"
	coresecurity "github.com/safedep/gryph/core/security"
)

// CheckName is the security.Check name set on every CheckResult emitted via
// the PEP boundary. The aarm package re-exports it so adapters and CLI
// wrappers can refer to a single source of truth.
const CheckName = "aarm-pdp"

const blockedByPolicy = "Blocked by policy"

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

	message := result.AgentMessage()
	stored := result.Message
	if result.Decision == model.DecisionBlock {
		message = cmp.Or(message, blockedByPolicy)
		stored = cmp.Or(stored, blockedByPolicy)
	}

	switch result.Decision {
	case model.DecisionBlock:
		out.Decision = coresecurity.DecisionBlock
		out.Reason = message
		out.StoredReason = stored
	case model.DecisionGuidance, model.DecisionWarn:
		out.Decision = coresecurity.DecisionGuidance
		out.Guidance = message
	case model.DecisionEscalate:
		log.Warnf("aarm/pep: escalate decision reached PEP without approval handling (matched_rules=%v); treating as guidance to avoid silent block",
			result.MatchedRuleIDs)
		out.Decision = coresecurity.DecisionGuidance
		if message == "" {
			message = "Action requires approval but escalation was not routed; configuration bug"
		}
		out.Guidance = message
	case model.DecisionDefer:
		log.Warnf("aarm/pep: defer decision reached PEP without deferral handling (matched_rules=%v); treating as block to avoid silent allow",
			result.MatchedRuleIDs)
		out.Decision = coresecurity.DecisionBlock
		const unrouted = "Action deferred but deferral was not routed; configuration bug"
		out.Reason = cmp.Or(message, unrouted)
		out.StoredReason = cmp.Or(stored, unrouted)
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

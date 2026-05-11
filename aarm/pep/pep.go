// Package pep maps AARM PDP decisions to Gryph security check results.
package pep

import (
	"github.com/safedep/gryph/aarm/model"
	coresecurity "github.com/safedep/gryph/core/security"
)

const checkName = "aarm-pdp"

// Apply converts a PDP decision into the existing security evaluator result.
func Apply(result *model.EvaluationResult) *coresecurity.CheckResult {
	if result == nil {
		return allow()
	}

	message := result.Message
	if message == "" {
		message = "Blocked by policy"
	}

	switch result.Decision {
	case model.DecisionBlock:
		return &coresecurity.CheckResult{
			Decision:  coresecurity.DecisionBlock,
			Reason:    message,
			CheckName: checkName,
		}
	case model.DecisionGuidance:
		return &coresecurity.CheckResult{
			Decision:  coresecurity.DecisionGuidance,
			Guidance:  message,
			CheckName: checkName,
		}
	case model.DecisionWarn:
		return &coresecurity.CheckResult{
			Decision:  coresecurity.DecisionGuidance,
			Guidance:  message,
			CheckName: checkName,
		}
	default:
		return allow()
	}
}

func allow() *coresecurity.CheckResult {
	return &coresecurity.CheckResult{
		Decision:  coresecurity.DecisionAllow,
		CheckName: checkName,
	}
}

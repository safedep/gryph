package aarm

import (
	"fmt"

	"github.com/safedep/gryph/aarm/model"
)

type ActionType = model.ActionType
type Action = model.Action
type Parameters = model.Parameters
type Decision = model.Decision
type Result = model.Result
type ContextSnapshot = model.ContextSnapshot
type FailMode = model.FailMode

const (
	DecisionAllow    = model.DecisionAllow
	DecisionWarn     = model.DecisionWarn
	DecisionGuidance = model.DecisionGuidance
	DecisionBlock    = model.DecisionBlock
	DecisionEscalate = model.DecisionEscalate

	FailClosed = model.FailClosed
	FailOpen   = model.FailOpen
)

func parseFailMode(s string) (FailMode, error) {
	switch FailMode(s) {
	case "":
		return FailClosed, nil
	case FailClosed:
		return FailClosed, nil
	case FailOpen:
		return FailOpen, nil
	default:
		return "", fmt.Errorf("invalid fail mode: %q", s)
	}
}

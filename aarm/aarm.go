package aarm

import (
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pep"
)

// CheckName is the security.Check name reported by the AARM mediator and any
// wrapper that delegates to it. Re-exported from pep so the mediator, the PEP
// boundary, and the CLI lazy-load wrapper all share one source of truth.
const CheckName = pep.CheckName

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
	DecisionDefer    = model.DecisionDefer

	FailClosed = model.FailClosed
	FailOpen   = model.FailOpen
)

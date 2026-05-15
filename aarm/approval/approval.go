// Package approval implements the AARM Approval Service for human-in-the-loop
// escalation. The Mediator calls Service.Request when the PDP returns
// Decision.Escalate. Two implementations ship: Nop (deny everything, the
// safe default) and CLIPrompt (interactive terminal prompt via /dev/tty).
package approval

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
)

// Decision is the operator's response to an approval request.
type Decision string

const (
	// DecisionApprove allows the action.
	DecisionApprove Decision = "approve"
	// DecisionDeny blocks the action.
	DecisionDeny Decision = "deny"
	// DecisionTimeout indicates the request expired before a response.
	DecisionTimeout Decision = "timeout"
)

// Audit-action constants describing approval lifecycle events. The Mediator
// stamps these onto ApprovalAudit.Action and the CLI's self-audit log records
// them verbatim. They are kept here, beside the approval domain types, so
// every layer references the same source of truth.
const (
	AuditActionRequested = "approval_requested"
	AuditActionGranted   = "approval_granted"
	AuditActionDenied    = "approval_denied"
	AuditActionTimeout   = "approval_timeout"
)

// Request carries the data the operator (or an automated frontend) needs to
// reach a decision about an escalated action.
type Request struct {
	SessionID uuid.UUID
	EventID   uuid.UUID
	ActionID  uuid.UUID
	Action    *model.Action
	Snapshot  *model.ContextSnapshot
	Rule      *model.EvaluationResult
	Timeout   time.Duration
}

// Outcome is the result of an approval request.
type Outcome struct {
	Decision  Decision
	Approver  string
	Note      string
	DecidedAt time.Time
}

// Service requests an approval decision for an escalated action.
type Service interface {
	Request(ctx context.Context, r *Request) (*Outcome, error)
}

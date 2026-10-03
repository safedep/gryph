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
	// DecisionPending says the request is open: no channel answered in the
	// inline wait, and the request waits for an approver. The action blocks
	// now, and a later answer stores a grant for a retry.
	DecisionPending Decision = "pending"
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
	AuditActionPending   = "approval_pending"
	// AuditActionSuperseded records an answer that came after the first
	// one and did not apply.
	AuditActionSuperseded = "approval_superseded"
	// AuditActionRefused records an answer the service refused: not an
	// approver, or the person who asked.
	AuditActionRefused = "approval_refused"
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
	// ReceiptSequence is the sequence of the receipt that records the
	// request. The Mediator writes it before it asks.
	ReceiptSequence int64
	// MinAssurance is the lowest assurance the rule accepts. Empty takes
	// the floor of the service.
	MinAssurance Assurance
	// Digest is the identity of the action for a request and a grant.
	Digest string
	// PeerTrust is the trust of the connection that carried the action.
	PeerTrust string
}

// Outcome is the result of an approval request.
type Outcome struct {
	Decision  Decision
	Approver  string
	Note      string
	DecidedAt time.Time
	// Channel and Assurance say who answered and how sure Gryph is of it.
	// PeerTrust is the trust of the connection that carried the answer.
	// The receipt records all three.
	Channel   string
	Assurance Assurance
	PeerTrust string
	// RequestID names the request in the store of the service, when the
	// service keeps one. GrantID names the grant that approved a retry.
	RequestID uuid.UUID
	GrantID   uuid.UUID
	// Scope is the scope of the grant an approval stored, empty for none.
	Scope Scope
}

// Meta returns the outcome as the receipt records it.
func (o *Outcome) Meta() map[string]any {
	if o == nil {
		return nil
	}
	m := map[string]any{}
	for k, v := range map[string]string{"channel": o.Channel, "assurance": string(o.Assurance), "approver": o.Approver, "peer_trust": o.PeerTrust} {
		if v != "" {
			m[k] = v
		}
	}
	if o.RequestID != uuid.Nil {
		m["request_id"] = o.RequestID.String()
	}
	if o.GrantID != uuid.Nil {
		m["grant_id"] = o.GrantID.String()
	}
	if o.Scope != "" {
		m["scope"] = string(o.Scope)
	}
	return m
}

// Service requests an approval decision for an escalated action.
type Service interface {
	Request(ctx context.Context, r *Request) (*Outcome, error)
}

package approval

import (
	"context"
	"time"
)

// Nop denies every approval request without prompting. This is the safe
// default when escalation is enabled in config but no operator-facing
// frontend is wired.
type Nop struct {
	now func() time.Time
}

// NewNop returns a Nop approval service.
func NewNop() *Nop {
	return &Nop{now: func() time.Time { return time.Now().UTC() }}
}

// Request implements Service. Always returns DecisionDeny.
func (n *Nop) Request(_ context.Context, _ *Request) (*Outcome, error) {
	return &Outcome{
		Decision:  DecisionDeny,
		Approver:  "nop",
		Note:      "no approval frontend configured",
		DecidedAt: n.now(),
	}, nil
}

var _ Service = (*Nop)(nil)

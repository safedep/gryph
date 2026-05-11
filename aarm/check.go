package aarm

import (
	"context"
	"fmt"

	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/pep"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
)

const mediatorName = "aarm-pdp"

// Mediator implements the Gryph security.Check interface with AARM components.
type Mediator struct {
	adapter mediation.Adapter
	pdp     *pdp.PDP
	enabled bool
}

// NewMediator creates an enabled AARM security check from a parsed policy.
func NewMediator(policy *pdp.Policy) (*Mediator, error) {
	engine, err := pdp.New(policy)
	if err != nil {
		return nil, err
	}
	return &Mediator{
		adapter: mediation.NewHookAdapter(),
		pdp:     engine,
		enabled: true,
	}, nil
}

// Name implements security.Check.
func (m *Mediator) Name() string {
	return mediatorName
}

// Enabled implements security.Check.
func (m *Mediator) Enabled() bool {
	return m != nil && m.enabled
}

// Check implements security.Check.
func (m *Mediator) Check(ctx context.Context, event *events.Event) (*coresecurity.CheckResult, error) {
	if m == nil || m.adapter == nil || m.pdp == nil {
		return nil, fmt.Errorf("aarm: mediator is not initialized")
	}

	action, err := m.adapter.Normalize(ctx, event, nil)
	if err != nil {
		return nil, err
	}
	decision, err := m.pdp.Evaluate(ctx, action, nil)
	if err != nil {
		return nil, err
	}
	return pep.Apply(decision), nil
}

var _ coresecurity.Check = (*Mediator)(nil)

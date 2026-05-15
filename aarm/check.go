package aarm

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/pep"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
)

// Mediator implements the Gryph security.Check interface with AARM components.
type Mediator struct {
	adapter mediation.Adapter
	pdp     *pdp.PDP
	accum   accumulator.Accumulator
	enabled bool
}

var _ coresecurity.Check = (*Mediator)(nil)

// MediatorOption configures optional Mediator dependencies.
type MediatorOption func(*Mediator)

// WithAccumulator overrides the default no-op Context Accumulator.
func WithAccumulator(a accumulator.Accumulator) MediatorOption {
	return func(m *Mediator) {
		if a != nil {
			m.accum = a
		}
	}
}

// NewMediator creates an enabled AARM security check from a parsed policy. By
// default the Context Accumulator is a no-op. Pass WithAccumulator to swap in
// a persistent implementation.
func NewMediator(policy *pdp.Policy, opts ...MediatorOption) (*Mediator, error) {
	engine, err := pdp.New(policy)
	if err != nil {
		return nil, err
	}
	m := &Mediator{
		adapter: mediation.NewHookAdapter(),
		pdp:     engine,
		accum:   accumulator.NewNop(),
		enabled: true,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// Name implements security.Check.
func (m *Mediator) Name() string {
	return CheckName
}

// Enabled implements security.Check.
func (m *Mediator) Enabled() bool {
	return m != nil && m.enabled
}

// Check implements security.Check.
func (m *Mediator) Check(ctx context.Context, event *events.Event) (*coresecurity.CheckResult, error) {
	if m == nil || m.adapter == nil || m.pdp == nil || m.accum == nil {
		return nil, fmt.Errorf("aarm: mediator is not initialized")
	}

	// Session is optional, not required. Today the only caller is cli/hook.go
	// which always wraps ctx via session.WithSession, so a miss would be a bug
	// in that path. Future non-hook adapters (MCP proxy, HTTP proxy, OS
	// mediation) may legitimately have no Gryph session, and the Mediator must
	// still produce decisions for rules that don't depend on session-derived
	// fields (Action.Project / AgentSessionID). Tighten to fail-fast when a
	// sessionless adapter is no longer plausible.
	sess, _ := session.FromContext(ctx)
	action, err := m.adapter.Normalize(ctx, event, sess)
	if err != nil {
		return nil, err
	}

	if err := m.accum.Append(ctx, action); err != nil {
		return nil, fmt.Errorf("aarm: accumulator append: %w", err)
	}

	snapshot, err := m.accum.Snapshot(ctx, action.SessionID)
	if err != nil {
		return nil, fmt.Errorf("aarm: accumulator snapshot: %w", err)
	}

	decision, err := m.pdp.Evaluate(ctx, action, snapshot)
	if err != nil {
		return nil, err
	}

	return pep.Apply(decision), nil
}

// RecordResult propagates a post-hook execution outcome to the Context
// Accumulator. cli/hook.go does not invoke this yet. It exists so post-hook
// wiring (Phase 2) can be added without further plumbing on the Mediator.
func (m *Mediator) RecordResult(ctx context.Context, actionID uuid.UUID, result model.Result) error {
	if m == nil || m.accum == nil {
		return nil
	}
	return m.accum.RecordResult(ctx, actionID, result)
}

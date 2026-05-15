package aarm

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/mediation"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/pep"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/core/events"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
)

// MediatorConfig holds runtime tuning for the Mediator.
type MediatorConfig struct {
	// LogAllEvaluations controls whether allow decisions also produce a
	// receipt. Default false: only block / guidance / warn / escalate
	// generate receipt rows.
	LogAllEvaluations bool
}

// Mediator implements the Gryph security.Check interface with AARM components.
type Mediator struct {
	adapter mediation.Adapter
	pdp     *pdp.PDP
	accum   accumulator.Accumulator
	receipt receipt.Generator
	cfg     MediatorConfig
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

// WithReceiptGenerator overrides the default no-op receipt generator.
func WithReceiptGenerator(g receipt.Generator) MediatorOption {
	return func(m *Mediator) {
		if g != nil {
			m.receipt = g
		}
	}
}

// WithMediatorConfig overrides the default MediatorConfig (zero value).
func WithMediatorConfig(c MediatorConfig) MediatorOption {
	return func(m *Mediator) {
		m.cfg = c
	}
}

// NewMediator creates an enabled AARM security check from a parsed policy. By
// default the Context Accumulator and receipt generator are no-ops. Pass
// WithAccumulator / WithReceiptGenerator to swap in persistent
// implementations.
func NewMediator(policy *pdp.Policy, opts ...MediatorOption) (*Mediator, error) {
	engine, err := pdp.New(policy)
	if err != nil {
		return nil, err
	}
	m := &Mediator{
		adapter: mediation.NewHookAdapter(),
		pdp:     engine,
		accum:   accumulator.NewNop(),
		receipt: receipt.NewNop(),
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
	return m != nil
}

// Check implements security.Check.
func (m *Mediator) Check(ctx context.Context, event *events.Event) (*coresecurity.CheckResult, error) {
	if m == nil || m.adapter == nil || m.pdp == nil || m.accum == nil || m.receipt == nil {
		return nil, fmt.Errorf("aarm: mediator is not initialized")
	}

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
		return nil, fmt.Errorf("aarm: %w: %w", accumulator.ErrSnapshot, err)
	}

	decision, err := m.pdp.Evaluate(ctx, action, snapshot)
	if err != nil {
		return nil, err
	}

	result := pep.Apply(decision)
	result.AarmActionID = action.ID
	result.AarmSessionID = action.SessionID

	if m.shouldRecordReceipt(decision) {
		rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
			SessionID: action.SessionID,
			ActionID:  action.ID,
			EventID:   action.EventID,
			Action:    action,
			Snapshot:  snapshot,
			Decision:  decision,
		})
		if rerr != nil {
			return result, rerr
		}
		if rec != nil {
			result.AarmSequence = rec.Sequence
		}
	}

	return result, nil
}

// shouldRecordReceipt encodes the LogAllEvaluations gating: an allow
// decision only produces a receipt when LogAllEvaluations=true. Every other
// decision always produces a receipt.
func (m *Mediator) shouldRecordReceipt(decision *model.EvaluationResult) bool {
	if decision == nil {
		return false
	}
	if decision.Decision == model.DecisionAllow && !m.cfg.LogAllEvaluations {
		return false
	}
	return true
}

// RecordResult propagates a post-hook execution outcome to both the Context
// Accumulator and the receipt generator. cli/hook.go invokes this on the
// allow path after the response is sent.
func (m *Mediator) RecordResult(ctx context.Context, actionID uuid.UUID, sessionID uuid.UUID, sequence int64, result model.Result) error {
	if m == nil {
		return nil
	}
	if m.accum != nil && actionID != uuid.Nil {
		if err := m.accum.RecordResult(ctx, actionID, result); err != nil {
			log.Warnf("aarm: accumulator record result: %v", err)
		}
	}
	if m.receipt != nil && sessionID != uuid.Nil && sequence > 0 {
		if err := m.receipt.UpdateResult(ctx, sessionID, sequence, result); err != nil {
			log.Warnf("aarm: receipt update result: %v", err)
		}
	}
	return nil
}

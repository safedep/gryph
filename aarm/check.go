package aarm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/accumulator"
	"github.com/safedep/gryph/aarm/approval"
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

	// ApprovalTimeout bounds how long the Mediator will block while waiting
	// for the Approval Service to return. Zero falls back to the Approval
	// Service's own default.
	ApprovalTimeout time.Duration
}

// ApprovalAudit is the data the Mediator hands to the optional approval
// audit hook. cli wires this through to logSelfAudit so the four
// approval_* self-audit actions get emitted without aarm depending on cli.
type ApprovalAudit struct {
	Action   string
	Outcome  *approval.Outcome
	Request  *approval.Request
	Decision *model.EvaluationResult
	Error    error
}

// ApprovalAuditHook receives ApprovalAudit events from the Mediator escalate
// path. Nil disables emission.
type ApprovalAuditHook func(ctx context.Context, e ApprovalAudit)

// Mediator implements the Gryph security.Check interface with AARM components.
type Mediator struct {
	adapter   mediation.Adapter
	pdp       *pdp.PDP
	accum     accumulator.Accumulator
	receipt   receipt.Generator
	approval  approval.Service
	auditHook ApprovalAuditHook
	cfg       MediatorConfig
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

// WithApprovalService overrides the default Nop approval service.
func WithApprovalService(s approval.Service) MediatorOption {
	return func(m *Mediator) {
		if s != nil {
			m.approval = s
		}
	}
}

// WithApprovalAuditHook installs an audit hook called from the escalate
// path. Used by the CLI to emit approval_* self-audit rows without aarm
// importing cli.
func WithApprovalAuditHook(h ApprovalAuditHook) MediatorOption {
	return func(m *Mediator) {
		if h != nil {
			m.auditHook = h
		}
	}
}

// WithAdapter overrides the default mediation adapter. Callers that need to
// wire a classifier or an injection scorer construct the adapter themselves
// (with mediation.NewHookAdapter) and pass it in. Keeps adapter-shaped
// configuration outside the Mediator's option surface.
func WithAdapter(a mediation.Adapter) MediatorOption {
	return func(m *Mediator) {
		if a != nil {
			m.adapter = a
		}
	}
}

// NewMediator creates an enabled AARM security check from a parsed policy. By
// default the Context Accumulator and receipt generator are no-ops and the
// adapter is a plain HookAdapter with no classifier or scorer. Pass
// WithAccumulator / WithReceiptGenerator / WithAdapter to swap in real
// implementations.
func NewMediator(policy *pdp.Policy, opts ...MediatorOption) (*Mediator, error) {
	engine, err := pdp.New(policy)
	if err != nil {
		return nil, err
	}
	m := &Mediator{
		pdp:      engine,
		accum:    accumulator.NewNop(),
		receipt:  receipt.NewNop(),
		approval: approval.NewNop(),
		adapter:  mediation.NewHookAdapter(),
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

	if decision.Decision == model.DecisionEscalate {
		return m.handleEscalate(ctx, action, snapshot, decision)
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

// handleEscalate routes an escalated decision through the Approval Service
// and synthesizes a security.CheckResult from the outcome.
func (m *Mediator) handleEscalate(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot, decision *model.EvaluationResult) (*coresecurity.CheckResult, error) {
	rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
		SessionID: action.SessionID,
		ActionID:  action.ID,
		EventID:   action.EventID,
		Action:    action,
		Snapshot:  snapshot,
		Decision:  decision,
	})
	if rerr != nil {
		return nil, rerr
	}

	req := &approval.Request{
		SessionID: action.SessionID,
		EventID:   action.EventID,
		ActionID:  action.ID,
		Action:    action,
		Snapshot:  snapshot,
		Rule:      decision,
		Timeout:   m.cfg.ApprovalTimeout,
	}
	m.emitAudit(ctx, ApprovalAudit{
		Action:   approval.AuditActionRequested,
		Request:  req,
		Decision: decision,
	})

	outcome, aerr := m.approval.Request(ctx, req)
	if aerr != nil && outcome == nil {
		m.emitAudit(ctx, ApprovalAudit{
			Action:   approval.AuditActionDenied,
			Request:  req,
			Decision: decision,
			Error:    aerr,
		})
		log.Warnf("aarm: approval service error: %v", aerr)
		return m.applyApprovalOutcome(ctx, action, decision, rec, &approval.Outcome{
			Decision:  approval.DecisionDeny,
			Approver:  "system",
			Note:      aerr.Error(),
			DecidedAt: time.Now().UTC(),
		}), nil
	}

	switch outcome.Decision {
	case approval.DecisionApprove:
		m.emitAudit(ctx, ApprovalAudit{Action: approval.AuditActionGranted, Request: req, Decision: decision, Outcome: outcome})
	case approval.DecisionTimeout:
		m.emitAudit(ctx, ApprovalAudit{Action: approval.AuditActionTimeout, Request: req, Decision: decision, Outcome: outcome})
	default:
		m.emitAudit(ctx, ApprovalAudit{Action: approval.AuditActionDenied, Request: req, Decision: decision, Outcome: outcome})
	}

	return m.applyApprovalOutcome(ctx, action, decision, rec, outcome), nil
}

func (m *Mediator) applyApprovalOutcome(ctx context.Context, action *model.Action, decision *model.EvaluationResult, rec *receipt.Record, outcome *approval.Outcome) *coresecurity.CheckResult {
	var (
		decisionValue string
		resultStatus  string
		coreDecision  coresecurity.Decision
		message       string
	)

	switch outcome.Decision {
	case approval.DecisionApprove:
		decisionValue = receipt.DecisionApproved
		resultStatus = string(model.ResultSuccess)
		coreDecision = coresecurity.DecisionAllow
		if outcome.Note != "" {
			message = fmt.Sprintf("Approved by %s: %s", outcome.Approver, outcome.Note)
		} else {
			message = fmt.Sprintf("Approved by %s", outcome.Approver)
		}
	case approval.DecisionTimeout:
		decisionValue = receipt.DecisionApprovalTimeout
		resultStatus = string(model.ResultBlocked)
		coreDecision = coresecurity.DecisionBlock
		message = "Approval timed out"
		if outcome.Note != "" {
			message = outcome.Note
		}
	default:
		decisionValue = receipt.DecisionDenied
		resultStatus = string(model.ResultRejected)
		coreDecision = coresecurity.DecisionBlock
		message = "Denied by approval policy"
		if outcome.Note != "" {
			message = fmt.Sprintf("Denied by %s: %s", outcome.Approver, outcome.Note)
		}
	}

	if rec != nil && rec.Sequence > 0 {
		if err := m.receipt.UpdateDecision(ctx, action.SessionID, rec.Sequence, decisionValue, resultStatus, outcome.Note); err != nil {
			log.Warnf("aarm: receipt update decision: %v", err)
		}
	}

	result := &coresecurity.CheckResult{
		CheckName:      CheckName,
		Decision:       coreDecision,
		MatchedRuleIDs: decision.MatchedRuleIDs,
		Severity:       mapSeverity(decision.Severity),
		Tags:           decision.Tags,
		AarmActionID:   action.ID,
		AarmSessionID:  action.SessionID,
	}
	if rec != nil {
		result.AarmSequence = rec.Sequence
	}
	if coreDecision == coresecurity.DecisionBlock {
		result.Reason = message
	} else {
		result.Guidance = message
	}
	return result
}

func (m *Mediator) emitAudit(ctx context.Context, e ApprovalAudit) {
	if m == nil || m.auditHook == nil {
		return
	}
	m.auditHook(ctx, e)
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

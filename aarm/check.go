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
	// receipt. The Go zero value is false (only block / guidance / warn /
	// escalate generate receipt rows), but the CLI default sourced from
	// policy.log_all_evaluations is true so allow rows are recorded too.
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

// DeferralRecord describes a deferral the Mediator is about to record. The
// hook owns the actual storage insert (so aarm does not depend on storage)
// and emits the deferral_requested self-audit row.
type DeferralRecord struct {
	SessionID       uuid.UUID
	ActionID        uuid.UUID
	ReceiptSequence int64
	Reason          string
	DeferredAt      time.Time
	ExpiresAt       time.Time
	Action          *model.Action
	Decision        *model.EvaluationResult
}

// DeferralHook receives DeferralRecord events from the Mediator defer path
// and returns the deferred-action row id assigned by the storage layer plus
// the operator-actionable hint that gets spliced into the block message
// returned to the agent. Returning an empty hint omits it from the block
// message. The hook keeps CLI-shaped guidance out of aarm. Nil disables
// emission. The hook must be safe for concurrent calls.
type DeferralHook func(ctx context.Context, r DeferralRecord) (uuid.UUID, string, error)

// DeferralConfig configures the Mediator's defer path. TimeoutSeconds bounds
// the per-deferral expires_at written to the queue. FreshSessionSeconds and
// ConflictTriggersDefer are forwarded to the PDP's synthetic-defer triggers.
type DeferralConfig struct {
	Enabled               bool
	TimeoutSeconds        int
	FreshSessionSeconds   int
	ConflictTriggersDefer bool
}

// IdentityConfig controls the AARM identity-capture enforcement layer at the
// Mediator boundary. Enabled mirrors the config.PolicyConfig.Identity.Enabled
// switch. When false, capture and enforcement are both no-ops.
// RequireHumanPrincipal is the pre-PDP block trigger.
type IdentityConfig struct {
	Enabled               bool
	RequireHumanPrincipal bool
}

// IdentityAudit is the data the Mediator hands to the optional identity audit
// hook when a Check is blocked because no human principal was captured. cli
// wires this through to logSelfAudit so aarm does not depend on cli.
type IdentityAudit struct {
	Action   *model.Action
	Decision *coresecurity.CheckResult
}

// IdentityAuditHook receives IdentityAudit events from the Mediator. Nil
// disables emission.
type IdentityAuditHook func(ctx context.Context, e IdentityAudit)

// Mediator implements the Gryph security.Check interface with AARM components.
type Mediator struct {
	adapter      mediation.Adapter
	pdp          *pdp.PDP
	accum        accumulator.Accumulator
	receipt      receipt.Generator
	approval     approval.Service
	auditHook    ApprovalAuditHook
	deferralHook DeferralHook
	identityHook IdentityAuditHook
	deferralCfg  DeferralConfig
	identityCfg  IdentityConfig
	cfg          MediatorConfig
	policyHash   []byte
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

// WithDeferralHook installs a hook called from the defer path so the CLI can
// persist the deferred-action row and emit a deferral_requested self-audit
// row without aarm importing storage or cli.
func WithDeferralHook(h DeferralHook) MediatorOption {
	return func(m *Mediator) {
		if h != nil {
			m.deferralHook = h
		}
	}
}

// WithDeferralConfig overrides the default deferral configuration (disabled).
func WithDeferralConfig(cfg DeferralConfig) MediatorOption {
	return func(m *Mediator) {
		m.deferralCfg = cfg
	}
}

// WithIdentityConfig overrides the default identity configuration (disabled).
// When Enabled and RequireHumanPrincipal are both true, the Mediator blocks
// any action whose HumanPrincipal field is empty before consulting the PDP.
func WithIdentityConfig(cfg IdentityConfig) MediatorOption {
	return func(m *Mediator) {
		m.identityCfg = cfg
	}
}

// WithIdentityAuditHook installs an audit hook called from the
// identity-missing pre-PDP block path. Used by the CLI to emit the
// identity_missing self-audit row without aarm importing cli.
func WithIdentityAuditHook(h IdentityAuditHook) MediatorOption {
	return func(m *Mediator) {
		if h != nil {
			m.identityHook = h
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
	m := &Mediator{
		accum:      accumulator.NewNop(),
		receipt:    receipt.NewNop(),
		approval:   approval.NewNop(),
		adapter:    mediation.NewHookAdapter(),
		policyHash: policy.Hash(),
	}
	for _, opt := range opts {
		opt(m)
	}
	pdpOpts := []pdp.Option{
		pdp.WithDeferConfig(pdp.DeferConfig{
			Enabled:               m.deferralCfg.Enabled,
			FreshSessionSeconds:   m.deferralCfg.FreshSessionSeconds,
			ConflictTriggersDefer: m.deferralCfg.ConflictTriggersDefer,
		}),
		pdp.WithSessionStartFn(func(ctx context.Context) (time.Time, bool) {
			sess, ok := session.FromContext(ctx)
			if !ok || sess == nil {
				return time.Time{}, false
			}
			return sess.StartedAt, true
		}),
	}
	engine, err := pdp.New(policy, pdpOpts...)
	if err != nil {
		return nil, err
	}
	m.pdp = engine
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

	if res, blocked := m.enforceIdentity(ctx, action); blocked {
		return res, nil
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

	if decision.Decision == model.DecisionDefer {
		return m.handleDefer(ctx, action, snapshot, decision)
	}

	result := pep.Apply(decision)
	result.AarmActionID = action.ID
	result.AarmSessionID = action.SessionID

	if m.shouldRecordReceipt(decision) {
		rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
			SessionID:  action.SessionID,
			ActionID:   action.ID,
			EventID:    action.EventID,
			Action:     action,
			Snapshot:   snapshot,
			Decision:   decision,
			PolicyHash: m.policyHash,
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

// identityMissingReason is the operator-facing block message returned when
// require_human_principal is true and no human principal was captured.
const identityMissingReason = "Action denied: no verifiable human principal"

// enforceIdentity blocks before PDP eval (and before the accumulator append)
// when require_human_principal is true and the captured HumanPrincipal is
// empty. Returns (result, true) when the action is denied. Records a block
// receipt with error_message populated on the initial insert (one writer-lock
// round trip) and fires the identity-missing audit hook so the CLI can emit
// the identity_missing self-audit row. A denied action did not happen, so it
// is recorded with a nil snapshot and does not contribute to
// context.total_actions.
func (m *Mediator) enforceIdentity(ctx context.Context, action *model.Action) (*coresecurity.CheckResult, bool) {
	if !m.identityCfg.Enabled || !m.identityCfg.RequireHumanPrincipal {
		return nil, false
	}
	if action.HumanPrincipal != "" {
		return nil, false
	}

	decision := &model.EvaluationResult{
		Decision:       model.DecisionBlock,
		MatchedRuleIDs: []string{},
		Message:        identityMissingReason,
		Severity:       model.SeverityHigh,
	}
	rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
		SessionID:    action.SessionID,
		ActionID:     action.ID,
		EventID:      action.EventID,
		Action:       action,
		Decision:     decision,
		PolicyHash:   m.policyHash,
		ErrorMessage: identityMissingReason,
	})
	if rerr != nil {
		log.Warnf("aarm: identity-missing receipt insert: %v", rerr)
	}

	result := &coresecurity.CheckResult{
		CheckName:      CheckName,
		Decision:       coresecurity.DecisionBlock,
		MatchedRuleIDs: decision.MatchedRuleIDs,
		Severity:       mapSeverity(decision.Severity),
		Reason:         identityMissingReason,
		AarmActionID:   action.ID,
		AarmSessionID:  action.SessionID,
	}
	if rec != nil {
		result.AarmSequence = rec.Sequence
	}
	if m.identityHook != nil {
		m.identityHook(ctx, IdentityAudit{Action: action, Decision: result})
	}
	return result, true
}

// handleEscalate routes an escalated decision through the Approval Service
// and synthesizes a security.CheckResult from the outcome.
func (m *Mediator) handleEscalate(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot, decision *model.EvaluationResult) (*coresecurity.CheckResult, error) {
	rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
		SessionID:  action.SessionID,
		ActionID:   action.ID,
		EventID:    action.EventID,
		Action:     action,
		Snapshot:   snapshot,
		Decision:   decision,
		PolicyHash: m.policyHash,
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

// handleDefer persists the defer receipt and asks the optional DeferralHook
// to record the pending-deferral queue row. The agent always sees a block so
// the action does not execute until an operator (or the timeout sweep)
// resolves it out-of-band.
func (m *Mediator) handleDefer(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot, decision *model.EvaluationResult) (*coresecurity.CheckResult, error) {
	reason := decision.DeferReason
	if reason == "" {
		reason = "unspecified"
	}
	now := time.Now().UTC()
	timeout := time.Duration(m.deferralCfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	expiresAt := now.Add(timeout)

	rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
		SessionID:   action.SessionID,
		ActionID:    action.ID,
		EventID:     action.EventID,
		Action:      action,
		Snapshot:    snapshot,
		Decision:    decision,
		PolicyHash:  m.policyHash,
		RecordedAt:  now,
		DeferReason: reason,
	})
	if rerr != nil {
		return nil, rerr
	}

	var operatorHint string
	if m.deferralHook != nil && rec != nil {
		_, hint, hookErr := m.deferralHook(ctx, DeferralRecord{
			SessionID:       action.SessionID,
			ActionID:        action.ID,
			ReceiptSequence: rec.Sequence,
			Reason:          reason,
			DeferredAt:      now,
			ExpiresAt:       expiresAt,
			Action:          action,
			Decision:        decision,
		})
		if hookErr != nil {
			log.Warnf("aarm: deferral hook: %v", hookErr)
		}
		operatorHint = hint
	}

	message := fmt.Sprintf("Action deferred: %s.", reason)
	if operatorHint != "" {
		message = message + " " + operatorHint
	}

	result := &coresecurity.CheckResult{
		CheckName:      CheckName,
		Decision:       coresecurity.DecisionBlock,
		MatchedRuleIDs: decision.MatchedRuleIDs,
		Severity:       mapSeverity(decision.Severity),
		Tags:           decision.Tags,
		AarmActionID:   action.ID,
		AarmSessionID:  action.SessionID,
		Reason:         message,
	}
	if rec != nil {
		result.AarmSequence = rec.Sequence
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

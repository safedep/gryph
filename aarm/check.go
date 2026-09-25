package aarm

import (
	"context"
	"errors"
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
	celEntries   int
	receipt      receipt.Generator
	approval     approval.Service
	auditHook    ApprovalAuditHook
	deferralHook DeferralHook
	identityHook IdentityAuditHook
	deferralCfg  DeferralConfig
	identityCfg  IdentityConfig
	cfg          MediatorConfig
	policyHash   []byte

	stripsContent func(event *events.Event) bool
}

var _ coresecurity.Check = (*Mediator)(nil)

// MediatorOption configures optional Mediator dependencies.
type MediatorOption func(*Mediator)

// DefaultCELEntries is the number of entries that context.entries holds when
// the config does not set policy.context.cel_entries.
const DefaultCELEntries = 100

// WithCELEntries sets the number of entries that context.entries holds.
func WithCELEntries(n int) MediatorOption {
	return func(m *Mediator) {
		if n > 0 {
			m.celEntries = n
		}
	}
}

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

// WithContentStrip installs the rule that tells whether the stored event
// loses its content values. When the rule returns true, the receipt keeps
// only the parameters that the stored event keeps. The PDP still sees every
// parameter.
func WithContentStrip(fn func(event *events.Event) bool) MediatorOption {
	return func(m *Mediator) {
		m.stripsContent = fn
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
		celEntries: DefaultCELEntries,
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
func (m *Mediator) Check(ctx context.Context, event *events.Event, sess *session.Session) (*coresecurity.CheckResult, error) {
	if m == nil || m.adapter == nil || m.pdp == nil || m.accum == nil || m.receipt == nil {
		return nil, fmt.Errorf("aarm: mediator is not initialized")
	}

	action, entry, err := m.adapter.Normalize(ctx, event, sess)
	if err != nil {
		return nil, err
	}

	if res, blocked := m.enforceIdentity(ctx, m.receiptAction(event, action), entry); blocked {
		return res, nil
	}

	snapshot, err := m.accum.Snapshot(ctx, action.SessionID, entry)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("aarm: %w: %w", accumulator.ErrSnapshot, err), m.appendFailedEntry(ctx, entry))
	}
	if sess != nil && snapshot != nil {
		owned := *snapshot
		owned.SessionStartedAt = sess.StartedAt
		snapshot = &owned
	}
	if snapshot != nil && m.pdp.NeedsEntries() {
		entries, err := m.accum.Entries(ctx, action.SessionID, m.celEntries)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("aarm: %w: %w", accumulator.ErrSnapshot, err), m.appendFailedEntry(ctx, entry))
		}
		owned := *snapshot
		owned.Entries = entries
		snapshot = &owned
	}

	stored := m.receiptAction(event, action)
	decision, err := m.pdp.EvaluateStored(ctx, action, stored, snapshot)
	if err != nil {
		return nil, errors.Join(err, m.appendFailedEntry(ctx, entry))
	}

	if r := entryResult(decision.Decision); r != "" {
		entry.Result = r
	}
	appendErr := m.appendEntry(ctx, entry, decision)

	// Drop the full match buffer so no later serialization of the action can
	// leak full content.
	action.Parameters.ContentFull = ""

	if decision.Decision == model.DecisionEscalate {
		res, err := m.handleEscalate(ctx, action, stored, snapshot, decision)
		return withAppendErr(res, err, appendErr)
	}

	if decision.Decision == model.DecisionDefer {
		res, err := m.handleDefer(ctx, stored, snapshot, decision)
		return withAppendErr(res, err, appendErr)
	}

	result := pep.Apply(decision)
	result.AarmActionID = action.ID
	result.AarmSessionID = action.SessionID

	// The fail mode can still block an action that runs without its entry.
	// So no receipt records it as allowed.
	if appendErr != nil && result.Decision != coresecurity.DecisionBlock {
		return result, appendErr
	}

	if m.shouldRecordReceipt(decision) {
		rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
			SessionID:  action.SessionID,
			ActionID:   action.ID,
			EventID:    action.EventID,
			Action:     stored,
			Snapshot:   snapshot,
			Decision:   decision,
			PolicyHash: m.policyHash,
		})
		if rerr != nil {
			return withAppendErr(result, rerr, appendErr)
		}
		if rec != nil {
			result.AarmSequence = rec.Sequence
		}
	}

	return withAppendErr(result, nil, appendErr)
}

// withAppendErr adds the append error to the check error when the result
// lets the action run. The session state then misses the entry, and later
// rules that read it fail open. So the fail mode decides. A block only logs
// the append error, so a block never becomes an allow under fail_mode open.
func withAppendErr(result *coresecurity.CheckResult, err, appendErr error) (*coresecurity.CheckResult, error) {
	if appendErr == nil || err != nil {
		return result, errors.Join(err, appendErr)
	}
	if result == nil || result.Decision == coresecurity.DecisionBlock {
		log.Warnf("%v", appendErr)
		return result, nil
	}
	return result, appendErr
}

// receiptAction returns the action as the receipt records it. A stripped
// event loses its line counts and its tool input. So the receipt drops the
// line counts, the URL, and every parameter that a tool-use action takes
// from the tool input.
func (m *Mediator) receiptAction(event *events.Event, action *model.Action) *model.Action {
	if m.stripsContent == nil || !m.stripsContent(event) {
		return action
	}
	stored := *action
	stored.Parameters = model.Parameters{SizeBytes: action.Parameters.SizeBytes}
	if action.Type != model.ActionToolUse {
		stored.Parameters.Path = action.Parameters.Path
		stored.Parameters.Command = action.Parameters.Command
		stored.Parameters.Args = action.Parameters.Args
	}
	return &stored
}

// appendEntry records the decision on the entry and writes it once, after
// the evaluation. It returns a failed append wrapped with
// accumulator.ErrAppend. The caller decides if the error reaches the fail mode.
func (m *Mediator) appendEntry(ctx context.Context, entry *model.ContextEntry, decision *model.EvaluationResult) error {
	entry.Decision = decision.Decision
	entry.MatchedRuleIDs = decision.MatchedRuleIDs
	entry.Tags = decision.MatchedTags
	if err := m.accum.Append(ctx, entry); err != nil {
		return fmt.Errorf("aarm: %w: %w", accumulator.ErrAppend, err)
	}
	return nil
}

// appendFailedEntry writes the entry of an action that has no decision
// because the snapshot, the entry log or the evaluation failed. The fail
// mode can still allow the action, so its classes must reach the session
// state. The caller already returns an error, so the append error joins it.
func (m *Mediator) appendFailedEntry(ctx context.Context, entry *model.ContextEntry) error {
	entry.Result = model.ResultError
	return m.appendEntry(ctx, entry, &model.EvaluationResult{})
}

// entryResult is the result that the first insert of an entry records. The
// hook returns before the allow-path RecordResult on block and defer, so
// those results go in with the entry. Other decisions keep the result of the
// event, and the allow path records the outcome later.
func entryResult(d model.Decision) model.ResultStatus {
	switch d {
	case model.DecisionBlock:
		return model.ResultBlocked
	case model.DecisionDefer:
		return model.ResultDeferred
	default:
		return ""
	}
}

// identityMissingReason is the operator-facing block message returned when
// require_human_principal is true and no human principal was captured.
const identityMissingReason = "Action denied: no verifiable human principal"

// enforceIdentity blocks before PDP eval when require_human_principal is
// true and the captured HumanPrincipal is empty. Returns (result, true) when
// the action is denied. Records a block receipt with error_message populated
// on the initial insert (one writer-lock round trip) and fires the
// identity-missing audit hook so the CLI can emit the identity_missing
// self-audit row. The receipt has a nil snapshot. The denied action is an
// attempt, so it gets a context entry and counts in context.total_actions.
func (m *Mediator) enforceIdentity(ctx context.Context, action *model.Action, entry *model.ContextEntry) (*coresecurity.CheckResult, bool) {
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
	entry.Result = model.ResultBlocked
	if err := m.appendEntry(ctx, entry, decision); err != nil {
		log.Warnf("%v", err)
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
// and synthesizes a security.CheckResult from the outcome. The operator sees
// the full action. The receipt records the stored action.
func (m *Mediator) handleEscalate(ctx context.Context, action, stored *model.Action, snapshot *model.ContextSnapshot, decision *model.EvaluationResult) (*coresecurity.CheckResult, error) {
	rec, rerr := m.receipt.Record(ctx, &receipt.RecordInput{
		SessionID:  action.SessionID,
		ActionID:   action.ID,
		EventID:    action.EventID,
		Action:     stored,
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
	if outcome == nil {
		// Fail closed: a nil outcome (with or without an error) must never
		// fall through to the switch below, which would panic on a nil
		// dereference. Any approval service that fails to return a verdict is
		// treated as a denial.
		note := "approval service returned no outcome"
		if aerr != nil {
			note = aerr.Error()
			log.Warnf("aarm: approval service error: %v", aerr)
		}
		m.emitAudit(ctx, ApprovalAudit{
			Action:   approval.AuditActionDenied,
			Request:  req,
			Decision: decision,
			Error:    aerr,
		})
		return m.applyApprovalOutcome(ctx, action, decision, rec, &approval.Outcome{
			Decision:  approval.DecisionDeny,
			Approver:  "system",
			Note:      note,
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

	// Approved actions proceed and are recorded on the allow path; record the
	// terminal result here only for deny/timeout. An approved prompt reaches
	// the agent, so only then does it become the latest intent.
	if coreDecision == coresecurity.DecisionBlock {
		m.recordContextResult(ctx, action.ID, model.ResultStatus(resultStatus))
	} else {
		m.confirmIntent(ctx, action.ID)
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
// Accumulator and the receipt generator. decision.Local invokes this on the
// allow path after it saves the event.
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

// recordContextResult sets a terminal result status on the already-appended
// context-action row. Errors are logged, not propagated, so a failed status
// update never turns a block into an allow.
func (m *Mediator) recordContextResult(ctx context.Context, actionID uuid.UUID, status model.ResultStatus) {
	if m == nil || m.accum == nil || actionID == uuid.Nil {
		return
	}
	if err := m.accum.RecordResult(ctx, actionID, model.Result{Status: status}); err != nil {
		log.Warnf("aarm: accumulator record terminal result: %v", err)
	}
}

// confirmIntent logs an error and does not propagate it, as
// recordContextResult does.
func (m *Mediator) confirmIntent(ctx context.Context, actionID uuid.UUID) {
	if m == nil || m.accum == nil || actionID == uuid.Nil {
		return
	}
	if err := m.accum.ConfirmIntent(ctx, actionID); err != nil {
		log.Warnf("aarm: accumulator confirm intent: %v", err)
	}
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

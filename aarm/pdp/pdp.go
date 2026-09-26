package pdp

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/safedep/gryph/core/privacy"
)

const conditionTimeout = 100 * time.Millisecond

// Synthetic defer reasons emitted by the auto-defer triggers. Surfaced on the
// receipt's defer_reason column and on the CheckResult message.
const (
	DeferReasonFreshSession        = "fresh_session_insufficient_context"
	DeferReasonConflictingPolicies = "conflicting_policies"
)

// DeferConfig tunes the synthetic defer triggers. Both the fresh-session and
// the conflicting-policies trigger are gated by Enabled.
type DeferConfig struct {
	Enabled               bool
	FreshSessionSeconds   int
	ConflictTriggersDefer bool
}

// PDP evaluates actions against policy rules.
type PDP struct {
	rules    []compiledRule
	deferCfg DeferConfig
}

// Option configures optional PDP behavior.
type Option func(*PDP)

// WithDeferConfig wires synthetic-defer behavior into the PDP.
func WithDeferConfig(cfg DeferConfig) Option {
	return func(p *PDP) {
		p.deferCfg = cfg
	}
}

// New creates a PDP from a validated policy.
func New(policy *Policy, opts ...Option) (*PDP, error) {
	rules := make([]Rule, 0)
	if policy != nil {
		rules = append(rules, policy.Rules...)
	}
	compiled, err := compileRules(rules)
	if err != nil {
		return nil, err
	}
	if policy != nil {
		if err := cmp.Or(CheckTagNames(policy), removedFieldError(compiled)); err != nil {
			log.Warnf("pdp: %v. The policy loads, but gryph policy validate rejects it", err)
		}
	}
	p := &PDP{rules: compiled}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// Evaluate computes the final decision and matched rule IDs. It renders the
// rule message from the action.
func (p *PDP) Evaluate(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot) (*model.EvaluationResult, error) {
	return p.EvaluateStored(ctx, action, action, snapshot)
}

// EvaluateStored evaluates the action as Evaluate does. It renders
// FullMessage from the action and Message from stored, the action as the
// receipt records it. A template can name a parameter that the stored action
// drops, so the stored message must not come from the full action.
func (p *PDP) EvaluateStored(ctx context.Context, action, stored *model.Action, snapshot *model.ContextSnapshot) (*model.EvaluationResult, error) {
	result := &model.EvaluationResult{Decision: model.DecisionAllow, MatchedRuleIDs: []string{}}
	if action == nil {
		return result, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	evalCtx, cancel := context.WithTimeout(ctx, conditionTimeout)
	defer cancel()

	var activations map[string]any

	conflictDetection := p.deferCfg.Enabled && p.deferCfg.ConflictTriggersDefer
	var tiers map[int][]matchedTier
	if conflictDetection {
		tiers = map[int][]matchedTier{}
	}

	freshSessionDeferred := false
	freshDeferRule := ""

	var winnerRule *compiledRule
	paths := &actionPaths{action: action}
	// condErr is the first condition error. The loop still runs the other
	// rules, so that a block rule decides even when another rule fails.
	var condErr error

	for i := range p.rules {
		rule := p.rules[i]
		if !isRuleEnabled(rule.rule) {
			continue
		}
		if !matchesScope(rule.rule.Scope, action) {
			continue
		}
		if !rule.matches(action, paths) {
			continue
		}
		if rule.hasCondition {
			if !freshSessionDeferred && p.shouldDeferFreshSession(rule, snapshot) {
				freshSessionDeferred = true
				freshDeferRule = rule.rule.ID
				continue
			}
			if activations == nil {
				activations = map[string]any{
					"action":  actionActivation(action, paths),
					"context": contextActivation(snapshot),
				}
			}
			ok, err := rule.conditionMatches(evalCtx, activations)
			if err != nil {
				condErr = cmp.Or(condErr, err)
				continue
			}
			if !ok {
				continue
			}
		}

		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.rule.ID)
		result.MatchedTags = addTags(result.MatchedTags, rule.rule.Tags)
		tier := precedence(rule.rule.Action)
		if conflictDetection {
			tiers[tier] = append(tiers[tier], matchedTier{
				decision: rule.rule.Action,
				ruleID:   rule.rule.ID,
				severity: rule.rule.Severity,
			})
		}
		if tier > precedence(result.Decision) {
			result.Decision = rule.rule.Action
			result.Severity = rule.rule.Severity
			result.Tags = rule.rule.Tags
			if rule.rule.Action == model.DecisionDefer {
				result.DeferReason = rule.rule.Reason
			} else {
				result.DeferReason = ""
			}
			winnerRule = &p.rules[i]
		}
	}

	if condErr != nil {
		if !gates(result.Decision) {
			return nil, condErr
		}
		log.Warnf("pdp: a condition failed, and a %s rule decides: %v", result.Decision, condErr)
	}

	// An allow tag rule matches without a decision, so the defer checks the
	// decision and not the count of matched rules.
	if freshSessionDeferred && result.Decision == model.DecisionAllow {
		return &model.EvaluationResult{
			Decision:       model.DecisionDefer,
			MatchedRuleIDs: []string{freshDeferRule},
			MatchedTags:    result.MatchedTags,
			Message:        DeferReasonFreshSession,
			DeferReason:    DeferReasonFreshSession,
		}, nil
	}

	if conflictDetection {
		if conflict, ruleIDs := detectConflict(tiers, result.Decision); conflict {
			return &model.EvaluationResult{
				Decision:       model.DecisionDefer,
				MatchedRuleIDs: ruleIDs,
				MatchedTags:    result.MatchedTags,
				Message:        DeferReasonConflictingPolicies,
				DeferReason:    DeferReasonConflictingPolicies,
			}, nil
		}
	}

	if winnerRule != nil {
		full, err := winnerRule.renderMessage(action, snapshot)
		if err != nil {
			return nil, err
		}
		result.FullMessage = full
		result.Message = full
		if stored != action {
			result.Message = winnerRule.storedMessage(stored, snapshot)
		}
	}

	return result, nil
}

// gates reports whether a decision stops the action or makes it wait for an
// approval. A failed condition can only make a decision stricter, so a
// matched gate stands. An agent can make a condition fail with a long
// command, and under fail_mode open the error would drop the gate.
func gates(d model.Decision) bool {
	return d == model.DecisionBlock || d == model.DecisionEscalate || d == model.DecisionDefer
}

// storedMessage renders the message from the stored action. The stored
// action lacks the values that Gryph strips, so a template that works on the
// full action can fail here. The decision must not depend on the logging
// level, so a failed render gives a fixed message and not an error.
func (r compiledRule) storedMessage(stored *model.Action, snapshot *model.ContextSnapshot) string {
	msg, err := r.renderMessage(stored, snapshot)
	if err != nil {
		log.Warnf("pdp: rule %s: render the stored message: %v", r.rule.ID, err)
		return "rule " + r.rule.ID
	}
	return msg
}

// addTags adds the tags that set does not hold, and keeps set sorted.
func addTags(set, tags []string) []string {
	for _, t := range tags {
		if i, found := slices.BinarySearch(set, t); !found {
			set = slices.Insert(set, i, t)
		}
	}
	return set
}

// detectConflict returns true when more than one rule matched at the
// winning precedence tier with structurally different output. Same-tier
// matches share a decision by construction (precedence is per-decision), so
// the meaningful disagreement is between severities. Two matches conflict
// when their (decision, severity) fingerprint differs. Comparing
// rendered messages would over-fire on trivially differing wording and
// under-fire when two rules at the same tier disagree on severity but share
// a message. The returned rule IDs are the matched rule IDs at the winning
// tier in stable order. The Mediator treats this as ambiguous policy
// authorship and synthesizes a defer decision.
func detectConflict(tiers map[int][]matchedTier, winner model.Decision) (bool, []string) {
	if winner == model.DecisionAllow {
		return false, nil
	}
	tier := precedence(winner)
	matches := tiers[tier]
	if len(matches) < 2 {
		return false, nil
	}
	firstFP := matches[0].fingerprint()
	conflict := false
	for _, m := range matches[1:] {
		if m.fingerprint() != firstFP {
			conflict = true
			break
		}
	}
	if !conflict {
		return false, nil
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ruleID)
	}
	return true, ids
}

// matchedTier is the per-tier match record used to detect conflicting
// decisions at the same severity precedence.
type matchedTier struct {
	decision model.Decision
	ruleID   string
	severity model.Severity
}

// fingerprint returns the structural identity of a matched rule used to
// detect conflicts at the same precedence tier. Tags are not part of it.
// Tags label the event, and every matched rule adds its tags, so two rules
// with different tags do not disagree.
func (m matchedTier) fingerprint() string {
	return string(m.decision) + "|" + string(m.severity)
}

// shouldDeferFreshSession reports whether the rule should defer instead of
// evaluating its CEL condition. Fires only when defer is enabled, the rule's
// referenced context fields are zero or empty in the snapshot, and the session
// is younger than FreshSessionSeconds.
func (p *PDP) shouldDeferFreshSession(rule compiledRule, snapshot *model.ContextSnapshot) bool {
	if !p.deferCfg.Enabled {
		return false
	}
	if p.deferCfg.FreshSessionSeconds <= 0 {
		return false
	}
	if len(rule.contextRefs) == 0 {
		return false
	}
	if snapshot == nil || snapshot.SessionStartedAt.IsZero() {
		return false
	}
	start := snapshot.SessionStartedAt
	if time.Since(start) >= time.Duration(p.deferCfg.FreshSessionSeconds)*time.Second {
		return false
	}
	if !contextRefsEmpty(rule.contextRefs, snapshot) {
		return false
	}
	return true
}

// contextRefsEmpty reports whether every referenced context field is zero or
// empty in the snapshot.
func contextRefsEmpty(refs []string, snapshot *model.ContextSnapshot) bool {
	if snapshot == nil {
		return true
	}
	for _, ref := range refs {
		if !contextFieldEmpty(ref, snapshot) {
			return false
		}
	}
	return true
}

// contextFieldEmpty reports whether a context field has no data yet. The
// intent, tag, origin, egress host and entry fields are never empty. A
// session with no intent, no tag or no earlier entry is a fact that a rule
// can act on, and the fresh-session defer must not hide it. A rule such as
// "block a write after a .pem read" must allow the first write of a session.
func contextFieldEmpty(field string, s *model.ContextSnapshot) bool {
	switch field {
	case "total_actions":
		return s.TotalActions == 0
	case "files_read":
		return s.FilesRead == 0
	case "files_written":
		return s.FilesWritten == 0
	case "commands_executed":
		return s.CommandsExecuted == 0
	case "network_requests":
		return s.NetworkRequests == 0
	case "errors":
		return s.Errors == 0
	case "tools_used":
		return len(s.ToolsUsed) == 0
	case "session_duration_ms":
		return s.SessionDuration == 0
	case "classifications_seen":
		return len(s.ClassificationsSeen) == 0
	case "entities_seen":
		return len(s.EntitiesSeen) == 0
	case "semantic_drift":
		return true
	default:
		return false
	}
}

func precedence(d model.Decision) int {
	switch d {
	case model.DecisionBlock:
		return 5
	case model.DecisionEscalate:
		return 4
	case model.DecisionDefer:
		return 3
	case model.DecisionGuidance:
		return 2
	case model.DecisionWarn:
		return 1
	default:
		return 0
	}
}

func matchesScope(scope Scope, action *model.Action) bool {
	if len(scope.Agents) > 0 && !containsFold(scope.Agents, action.Agent) {
		return false
	}
	if len(scope.Projects) > 0 && !containsFold(scope.Projects, action.Project) {
		return false
	}
	if len(scope.Tools) > 0 && !containsFold(scope.Tools, action.Tool) {
		return false
	}
	return true
}

type compiledRule struct {
	rule               Rule
	commandPatterns    []*regexp.Regexp
	contentPatterns    []*regexp.Regexp
	condition          cel.Program
	message            *template.Template
	filePatterns       []string
	containerPatterns  []string
	treePatterns       []string
	namedTreePatterns  []string
	fileAccess         []shellcmd.Access
	workingDirPatterns []string
	hasCondition       bool
	hasMessageTemplate bool
	contextRefs        []string
	// removed is the validation error of a rule that reads a removed
	// context field. The rule still loads.
	removed error
}

func compileRules(rules []Rule) ([]compiledRule, error) {
	env, err := conditionEnv()
	if err != nil {
		return nil, err
	}

	compiled := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		cr, err := compileRule(env, rule)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, cr)
	}
	return compiled, nil
}

func compileRule(env *cel.Env, rule Rule) (compiledRule, error) {
	cr := compiledRule{
		rule:               rule,
		filePatterns:       normalizePatterns(rule.Match.FilePatterns),
		workingDirPatterns: normalizePatterns(rule.Match.WorkingDirectoryPatterns),
	}

	var err error
	cr.commandPatterns, err = compileRegexps(rule.Match.CommandPatterns)
	if err != nil {
		return cr, fmt.Errorf("rule %q command_patterns: %w", rule.ID, err)
	}
	cr.contentPatterns, err = compileRegexps(rule.Match.ContentPatterns)
	if err != nil {
		return cr, fmt.Errorf("rule %q content_patterns: %w", rule.ID, err)
	}

	if err := validateGlobPatterns("file_patterns", rule.ID, cr.filePatterns); err != nil {
		return cr, err
	}
	cr.containerPatterns = containerPatterns(cr.filePatterns)
	cr.treePatterns = parentPatterns(cr.filePatterns)
	cr.namedTreePatterns = namedTreePatterns(cr.filePatterns)
	cr.fileAccess = defaultFileAccess
	if len(rule.Match.FileAccess) > 0 {
		cr.fileAccess = make([]shellcmd.Access, 0, len(rule.Match.FileAccess))
		for _, a := range rule.Match.FileAccess {
			cr.fileAccess = append(cr.fileAccess, shellcmd.Access(a))
		}
	}
	if err := validateGlobPatterns("working_directory_patterns", rule.ID, cr.workingDirPatterns); err != nil {
		return cr, err
	}

	if strings.TrimSpace(rule.Condition) != "" {
		prg, ast, err := compileCondition(env, rule.ID, rule.Condition)
		if err != nil {
			return cr, err
		}
		cr.condition = prg
		cr.hasCondition = true
		cr.contextRefs = collectContextRefs(ast)
		if field := removedFieldRef(ast); field != "" {
			cr.removed = fmt.Errorf("rule %q condition: context.%s was removed. Remove it from the condition", rule.ID, field)
		}
	}

	if strings.TrimSpace(rule.Message) != "" {
		tmpl, err := template.New(rule.ID).Option("missingkey=error").Parse(rule.Message)
		if err != nil {
			return cr, fmt.Errorf("rule %q message template: %w", rule.ID, err)
		}
		// A field that the template data does not have fails every
		// evaluation, so it fails validation. Other errors on empty data,
		// such as an index out of range, can pass at runtime.
		if err := tmpl.Execute(io.Discard, templateData{}); err != nil && strings.Contains(err.Error(), "can't evaluate field") {
			return cr, fmt.Errorf("rule %q message template: %w", rule.ID, err)
		}
		cr.message = tmpl
		cr.hasMessageTemplate = true
		for _, f := range removedContextFields {
			if cr.removed == nil && f.templateRE.MatchString(rule.Message) {
				cr.removed = fmt.Errorf("rule %q message template: .Context.%s was removed. Remove it from the message", rule.ID, f.template)
			}
		}
	}

	return cr, nil
}

// matchesActionType reports whether a rule selects the action type. A rule
// with no action_types selects every action, but not a user prompt. A prompt
// is not an action, and a broad rule, such as a cap on total_actions, must
// not stop the user from typing. A rule selects prompts by name.
func matchesActionType(types []string, t model.ActionType) bool {
	if len(types) == 0 {
		return t != model.ActionUserPrompt
	}
	return containsFold(types, string(t))
}

func (r compiledRule) matches(action *model.Action, paths *actionPaths) bool {
	match := r.rule.Match
	if !matchesActionType(match.ActionTypes, action.Type) {
		return false
	}
	if len(match.ToolNames) > 0 && !containsFold(match.ToolNames, action.Tool) {
		return false
	}
	if len(r.filePatterns) > 0 && !r.matchesFiles(action, paths) {
		return false
	}
	if len(r.workingDirPatterns) > 0 && !matchesAnyPath(r.workingDirPatterns, action.WorkingDir) {
		return false
	}
	if len(r.commandPatterns) > 0 && !matchesAnyRegex(r.commandPatterns, commandLine(action.Parameters)) {
		return false
	}
	if len(r.contentPatterns) > 0 && !matchesAnyRegex(r.contentPatterns, matchContent(action.Parameters)) {
		return false
	}
	return true
}

// matchContent prefers the untruncated ContentFull so a payload past the
// preview boundary cannot evade a content rule, falling back to the preview.
func matchContent(p model.Parameters) string {
	if p.ContentFull != "" {
		return p.ContentFull
	}
	return p.Content
}

func (r compiledRule) conditionMatches(ctx context.Context, activations map[string]any) (bool, error) {
	out, _, err := r.condition.ContextEval(ctx, activations)
	if err != nil {
		return false, fmt.Errorf("rule %q condition: %w", r.rule.ID, err)
	}
	matched, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("rule %q condition returned %T, want bool", r.rule.ID, out.Value())
	}
	return matched, nil
}

func (r compiledRule) renderMessage(action *model.Action, snapshot *model.ContextSnapshot) (string, error) {
	if !r.hasMessageTemplate {
		return strings.TrimSpace(r.rule.Message), nil
	}

	var buf bytes.Buffer
	if err := r.message.Execute(&buf, templateData{
		Action:  newTemplateAction(action),
		Context: newTemplateContext(snapshot),
		Rule: templateRule{
			ID:          r.rule.ID,
			Description: r.rule.Description,
			Action:      string(r.rule.Action),
			Severity:    string(r.rule.Severity),
			Tags:        r.rule.Tags,
		},
	}); err != nil {
		return "", fmt.Errorf("rule %q message template: %w", r.rule.ID, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func containsFold(values []string, want string) bool {
	return slices.ContainsFunc(values, func(v string) bool {
		return strings.EqualFold(v, want)
	})
}

func matchesAnyPath(patterns []string, value string) bool {
	if value == "" {
		return false
	}
	normalized := filepath.ToSlash(value)
	for _, pattern := range patterns {
		if ok, _ := doublestar.Match(pattern, normalized); ok {
			return true
		}
	}
	return false
}

// commandLine joins the command and its promoted args so command_patterns
// match the full invocation, not just the executable name. Args empty yields
// the bare command, preserving prior behavior.
func commandLine(p model.Parameters) string {
	if len(p.Args) == 0 {
		return p.Command
	}
	if p.Command == "" {
		return strings.Join(p.Args, " ")
	}
	return p.Command + " " + strings.Join(p.Args, " ")
}

func matchesAnyRegex(patterns []*regexp.Regexp, value string) bool {
	if value == "" {
		return false
	}
	for _, re := range patterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

func compileRegexps(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func normalizePatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		if pattern != "" {
			normalized = append(normalized, pattern)
		}
	}
	return normalized
}

func validateGlobPatterns(field, ruleID string, patterns []string) error {
	for _, pattern := range patterns {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("rule %q %s: invalid glob pattern %q", ruleID, field, pattern)
		}
	}
	return nil
}

// conditionCostLimit bounds the CEL cost of a condition. A 90-character
// matches() regex on a prompt at celPromptContentMax costs about 19000 and
// runs in under 1 ms, and the limit leaves room for a regex about five times
// that long. A condition that reads context.entries walks up to
// config.MaxCELEntries entries, and the storage bounds the size of each entry
// field, so it gets entriesCostLimit. The 100 ms timeout bounds both.
const (
	conditionCostLimit = 100_000
	entriesCostLimit   = 5_000_000
)

func compileCondition(env *cel.Env, ruleID, expr string) (cel.Program, *cel.Ast, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return nil, nil, fmt.Errorf("rule %q condition: %w", ruleID, issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, nil, fmt.Errorf("rule %q condition: output type %s, want bool", ruleID, ast.OutputType())
	}
	costLimit := uint64(conditionCostLimit)
	if readsEntries(collectContextRefs(ast)) {
		costLimit = entriesCostLimit
	}
	prg, err := env.Program(ast, cel.CostLimit(costLimit), cel.InterruptCheckFrequency(100))
	if err != nil {
		return nil, nil, fmt.Errorf("rule %q condition program: %w", ruleID, err)
	}
	return prg, ast, nil
}

// collectContextRefs walks the compiled CEL AST and returns the immediate
// `context.<field>` selectors referenced anywhere in the expression. Nested
// selectors (`context.foo.bar`) and non-context selections are ignored. The
// returned slice is deduplicated and sorted for stable hashing or display.
func collectContextRefs(ast *cel.Ast) []string {
	if ast == nil {
		return nil
	}
	native := ast.NativeRep()
	if native == nil {
		return nil
	}
	seen := map[string]struct{}{}
	idents, accesses := 0, 0
	visitor := celast.NewExprVisitor(func(e celast.Expr) {
		switch e.Kind() {
		case celast.IdentKind:
			if e.AsIdent() == "context" {
				idents++
			}
		default:
			if operand, field, ok := fieldAccess(e); ok && isContextIdent(operand) {
				seen[field] = struct{}{}
				accesses++
			}
		}
	})
	celast.PreOrderVisit(native.Expr(), visitor)
	if idents > accesses {
		// The condition uses context in a way that names no field, such
		// as [context].exists(c, c.entries...). It may read any field.
		seen[anyContextField] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fieldAccess returns the operand and the field name of a select such as
// x.f or x.?f, or of an index with a string literal such as x["f"].
func fieldAccess(e celast.Expr) (celast.Expr, string, bool) {
	switch e.Kind() {
	case celast.SelectKind:
		sel := e.AsSelect()
		return sel.Operand(), sel.FieldName(), true
	case celast.CallKind:
		call := e.AsCall()
		switch call.FunctionName() {
		case operators.Index, operators.OptIndex, operators.OptSelect:
		default:
			return nil, "", false
		}
		args := call.Args()
		if len(args) != 2 || args[1].Kind() != celast.LiteralKind {
			return nil, "", false
		}
		if name, ok := args[1].AsLiteral().(types.String); ok {
			return args[0], string(name), true
		}
	}
	return nil, "", false
}

// removedField is a context field that older policies can name. No
// component computes it, so a rule on it can never fire as written. A policy
// load keeps it at zero and warns. Validation rejects it.
type removedField struct {
	cel        string
	template   string
	templateRE *regexp.Regexp
}

var removedContextFields = []removedField{
	{cel: "semantic_drift", template: "SemanticDrift", templateRE: regexp.MustCompile(`\.SemanticDrift\b`)},
}

// removedFieldError returns the error of the first rule that reads a removed
// context field.
func removedFieldError(rules []compiledRule) error {
	for _, r := range rules {
		if r.removed != nil {
			return r.removed
		}
	}
	return nil
}

// removedFieldRef returns a removed context field that the condition reads
// on any value. Only context has these fields, so this also finds a read
// through an alias, as in [context].exists(c, c.semantic_drift > 0).
func removedFieldRef(ast *cel.Ast) string {
	native := ast.NativeRep()
	if native == nil {
		return ""
	}
	var found string
	celast.PreOrderVisit(native.Expr(), celast.NewExprVisitor(func(e celast.Expr) {
		_, field, ok := fieldAccess(e)
		if ok && found == "" && slices.ContainsFunc(removedContextFields, func(f removedField) bool { return f.cel == field }) {
			found = field
		}
	}))
	return found
}

// anyContextField is the reference of a condition that may read any context
// field.
const anyContextField = "*"

func isContextIdent(e celast.Expr) bool {
	return e != nil && e.Kind() == celast.IdentKind && e.AsIdent() == "context"
}

func conditionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("action", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("context", cel.MapType(cel.StringType, cel.DynType)),
		cel.OptionalTypes(),
		cel.Function("glob",
			cel.Overload("glob_string_string", []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
				cel.BinaryBinding(celGlob))),
	)
}

// globPathMaxBytes bounds the path of glob(). CEL gives glob() a fixed cost,
// and the timeout cannot stop one long match, so a padded path must not reach
// the matcher. It is PATH_MAX.
const globPathMaxBytes = 4096

// celGlob implements glob(path, pattern). It matches one path against one
// pattern with the matcher of file_patterns.
func celGlob(path, pattern ref.Val) ref.Val {
	p, ok1 := path.(types.String)
	pat, ok2 := pattern.(types.String)
	if !ok1 || !ok2 {
		return types.NewErr("glob: want (string, string)")
	}
	if len(p) > globPathMaxBytes {
		return types.NewErr("glob: path is longer than %d bytes", globPathMaxBytes)
	}
	if !doublestar.ValidatePattern(string(pat)) {
		return types.NewErr("glob: invalid pattern %q", string(pat))
	}
	return types.Bool(matchesAnyPath([]string{string(pat)}, string(p)))
}

// phaseOrUnknown normalizes the empty ActionPhase zero value to PhaseUnknown
// so action.phase is always one of the three documented strings in CEL.
func phaseOrUnknown(p model.ActionPhase) model.ActionPhase {
	if p == "" {
		return model.PhaseUnknown
	}
	return p
}

// celPromptContentMax bounds the prompt content that a CEL condition reads.
// A CEL string function costs about the length of its input, and matches()
// costs about the length times the regex length. A Gemini prompt with its
// referenced files can be long. content_patterns still match the whole
// prompt, because they run outside CEL.
const celPromptContentMax = 8 << 10

// paramsContent gives a prompt rule the prompt that the agent gets, with the
// referenced file content, up to celPromptContentMax bytes cut on a rune
// boundary. The stored prompt of an agent can hold only the typed part, and a
// user can type text that the agent then cuts from it. It reports whether it
// cut the prompt.
func paramsContent(action *model.Action) (string, bool) {
	if action.Type != model.ActionUserPrompt {
		return action.Parameters.Content, false
	}
	content := matchContent(action.Parameters)
	if len(content) <= celPromptContentMax {
		return content, false
	}
	limit := celPromptContentMax
	for limit > 0 && !utf8.RuneStart(content[limit]) {
		limit--
	}
	return content[:limit], true
}

func actionActivation(action *model.Action, paths *actionPaths) map[string]any {
	if action == nil {
		action = &model.Action{}
	}
	classifications := privacy.Strings(action.DataClassifications)
	if classifications == nil {
		classifications = []string{}
	}
	content, cut := paramsContent(action)
	return map[string]any{
		"type":                 string(action.Type),
		"tool":                 action.Tool,
		"operation":            action.Operation,
		"agent":                action.Agent,
		"working_dir":          action.WorkingDir,
		"project":              action.Project,
		"injection_score":      float64(action.InjectionScore),
		"data_classifications": classifications,
		"phase":                string(phaseOrUnknown(action.Phase)),
		"content_truncated":    action.ContentTruncated || cut,
		"human_principal":      action.HumanPrincipal,
		"service_identity":     action.ServiceIdentity,
		"role_scope":           action.RoleScope,
		"gryph_hook":           paths.runsGryphHook(),
		"kind":                 string(action.Kind),
		"origin":               string(action.Origin),
		"source":               action.Source,
		"sources":              nonNil(action.Sources),
		"hosts":                action.Hosts(),
		"read_paths":           action.ReadPaths(),
		"write_paths":          action.WritePaths(),
		"params": map[string]any{
			"path":          action.Parameters.Path,
			"command":       action.Parameters.Command,
			"args":          action.Parameters.Args,
			"url":           action.Parameters.URL,
			"size_bytes":    action.Parameters.SizeBytes,
			"lines_added":   action.Parameters.LinesAdded,
			"lines_removed": action.Parameters.LinesRemoved,
			"content":       content,
		},
	}
}

func contextActivation(snapshot *model.ContextSnapshot) map[string]any {
	if snapshot == nil {
		snapshot = &model.ContextSnapshot{}
	}
	return map[string]any{
		"total_actions":        snapshot.TotalActions,
		"files_read":           snapshot.FilesRead,
		"files_written":        snapshot.FilesWritten,
		"commands_executed":    snapshot.CommandsExecuted,
		"network_requests":     snapshot.NetworkRequests,
		"errors":               snapshot.Errors,
		"tools_used":           snapshot.ToolsUsed,
		"session_duration_ms":  snapshot.SessionDuration.Milliseconds(),
		"classifications_seen": snapshot.ClassificationsSeen,
		"tags_seen":            tagNames(snapshot.TagsSeen),
		"tag_seq":              tagSeq(snapshot.TagsSeen),
		"origins_seen":         nonNil(snapshot.OriginsSeen),
		"entities_seen":        nonNil(snapshot.EntitiesSeen),
		"semantic_drift":       0.0,
		"egress_hosts":         nonNil(snapshot.EgressHosts),
		"entries":              entryMaps(snapshot.Entries),
		"intent_available":     snapshot.IntentAvailable,
		"actions_since_intent": snapshot.ActionsSinceIntent,
	}
}

// tagNames returns the sorted tag names of seen.
func tagNames(seen map[string]int64) []string {
	return slices.Sorted(maps.Keys(seen))
}

// tagSeq returns seen, or an empty map, so a CEL lookup never meets null.
func tagSeq(seen map[string]int64) map[string]int64 {
	if seen == nil {
		return map[string]int64{}
	}
	return seen
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// entryMaps turns the entry log into CEL maps.
func entryMaps(entries []model.EntryFacts) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"seq":         e.Seq,
			"kind":        e.Kind,
			"action_type": e.ActionType,
			"tool":        e.Tool,
			"path":        e.Path,
			"command":     e.Command,
			"host":        e.Host,
			"mcp_server":  e.MCPServer,
			"origin":      e.Origin,
			"classes":     nonNil(e.Classes),
			"tags":        nonNil(e.Tags),
			"decision":    e.Decision,
			"result":      e.Result,
		})
	}
	return out
}

// NeedsEntries reports whether a rule reads context.entries. The Mediator
// loads the entry log only then.
func (p *PDP) NeedsEntries() bool {
	for _, r := range p.rules {
		if readsEntries(r.contextRefs) {
			return true
		}
	}
	return false
}

func readsEntries(refs []string) bool {
	return slices.Contains(refs, "entries") || slices.Contains(refs, anyContextField)
}

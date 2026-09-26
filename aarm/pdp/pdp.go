package pdp

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
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
		tier := precedence(rule.rule.Action)
		if conflictDetection {
			tiers[tier] = append(tiers[tier], matchedTier{
				decision: rule.rule.Action,
				ruleID:   rule.rule.ID,
				severity: rule.rule.Severity,
				tags:     rule.rule.Tags,
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

	if freshSessionDeferred && len(result.MatchedRuleIDs) == 0 {
		return &model.EvaluationResult{
			Decision:       model.DecisionDefer,
			MatchedRuleIDs: []string{freshDeferRule},
			Message:        DeferReasonFreshSession,
			DeferReason:    DeferReasonFreshSession,
		}, nil
	}

	if conflictDetection {
		if conflict, ruleIDs := detectConflict(tiers, result.Decision); conflict {
			return &model.EvaluationResult{
				Decision:       model.DecisionDefer,
				MatchedRuleIDs: ruleIDs,
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

// detectConflict returns true when more than one rule matched at the
// winning precedence tier with structurally different output. Same-tier
// matches share a decision by construction (precedence is per-decision), so
// the meaningful disagreement is between severity or tags. Two matches
// conflict when their (severity, sorted-tags) fingerprint differs. Comparing
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
	tags     []string
}

// fingerprint returns the structural identity of a matched rule used to
// detect conflicts at the same precedence tier. Tags are sorted into a
// stable order so author-side tag ordering does not flip the result.
func (m matchedTier) fingerprint() string {
	sortedTags := append([]string(nil), m.tags...)
	sort.Strings(sortedTags)
	return string(m.decision) + "|" + string(m.severity) + "|" + strings.Join(sortedTags, ",")
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
// intent fields are never empty. A session with no intent is a fact that a
// rule can act on, and the fresh-session defer must not hide it.
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
		return s.SemanticDrift == 0
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
		prg, refs, err := compileCondition(env, rule.ID, rule.Condition)
		if err != nil {
			return cr, err
		}
		cr.condition = prg
		cr.hasCondition = true
		cr.contextRefs = refs
	}

	if strings.TrimSpace(rule.Message) != "" {
		tmpl, err := template.New(rule.ID).Option("missingkey=error").Parse(rule.Message)
		if err != nil {
			return cr, fmt.Errorf("rule %q message template: %w", rule.ID, err)
		}
		cr.message = tmpl
		cr.hasMessageTemplate = true
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

func compileCondition(env *cel.Env, ruleID, expr string) (cel.Program, []string, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return nil, nil, fmt.Errorf("rule %q condition: %w", ruleID, issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, nil, fmt.Errorf("rule %q condition: output type %s, want bool", ruleID, ast.OutputType())
	}
	prg, err := env.Program(ast, cel.CostLimit(celCostLimit), cel.InterruptCheckFrequency(100))
	if err != nil {
		return nil, nil, fmt.Errorf("rule %q condition program: %w", ruleID, err)
	}
	refs := collectContextRefs(ast)
	return prg, refs, nil
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
	visitor := celast.NewExprVisitor(func(e celast.Expr) {
		if e.Kind() != celast.SelectKind {
			return
		}
		sel := e.AsSelect()
		operand := sel.Operand()
		if operand == nil || operand.Kind() != celast.IdentKind {
			return
		}
		if operand.AsIdent() != "context" {
			return
		}
		seen[sel.FieldName()] = struct{}{}
	})
	celast.PreOrderVisit(native.Expr(), visitor)
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

func conditionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("action", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("context", cel.MapType(cel.StringType, cel.DynType)),
	)
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

// celCostLimit bounds the work of one condition. A 90-character regex on a
// prompt at celPromptContentMax costs about 19000 and runs in under 1 ms.
// The limit leaves room for a regex about five times that long.
const celCostLimit = 100000

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
		"entities_seen":        snapshot.EntitiesSeen,
		"semantic_drift":       snapshot.SemanticDrift,
		"intent_available":     snapshot.IntentAvailable,
		"actions_since_intent": snapshot.ActionsSinceIntent,
	}
}

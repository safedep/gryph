package pdp

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/cel-go/cel"
	"github.com/safedep/gryph/aarm/model"
)

const conditionTimeout = 100 * time.Millisecond

// PDP evaluates actions against policy rules.
type PDP struct {
	rules []compiledRule
}

// New creates a PDP from a validated policy.
func New(policy *Policy) (*PDP, error) {
	rules := make([]Rule, 0)
	if policy != nil {
		rules = append(rules, policy.Rules...)
	}
	compiled, err := compileRules(rules)
	if err != nil {
		return nil, err
	}
	return &PDP{rules: compiled}, nil
}

// Evaluate computes the final decision and matched rule IDs.
func (p *PDP) Evaluate(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot) (*model.EvaluationResult, error) {
	result := &model.EvaluationResult{Decision: model.DecisionAllow, MatchedRuleIDs: []string{}}
	if action == nil {
		return result, nil
	}

	for _, rule := range p.rules {
		if !isRuleEnabled(rule.rule) {
			continue
		}
		if !matchesScope(rule.rule.Scope, action) {
			continue
		}
		if !rule.matches(action) {
			continue
		}
		ok, err := rule.conditionMatches(ctx, action, snapshot)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.rule.ID)
		if precedence(rule.rule.Action) > precedence(result.Decision) {
			msg, err := rule.renderMessage(action, snapshot)
			if err != nil {
				return nil, err
			}
			result.Decision = rule.rule.Action
			result.Message = msg
			result.Severity = rule.rule.Severity
			result.Tags = append([]string(nil), rule.rule.Tags...)
		}
	}

	if result.Decision == model.DecisionEscalate {
		result.Decision = model.DecisionBlock
		if result.Message == "" {
			result.Message = "This action requires approval (not yet implemented)."
		}
	}

	return result, nil
}

func precedence(d model.Decision) int {
	switch d {
	case model.DecisionBlock:
		return 5
	case model.DecisionEscalate:
		return 4
	case model.DecisionGuidance:
		return 3
	case model.DecisionWarn:
		return 2
	default:
		return 1
	}
}

func matchesScope(scope Scope, action *model.Action) bool {
	if len(scope.Agents) > 0 && !contains(scope.Agents, action.Agent) {
		return false
	}
	if len(scope.Projects) > 0 && !contains(scope.Projects, action.Project) {
		return false
	}
	if len(scope.Tools) > 0 && !contains(scope.Tools, action.Tool) {
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
	workingDirPatterns []string
	hasCondition       bool
	hasMessageTemplate bool
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
	if err := validateGlobPatterns("working_directory_patterns", rule.ID, cr.workingDirPatterns); err != nil {
		return cr, err
	}

	if strings.TrimSpace(rule.Condition) != "" {
		prg, err := compileCondition(env, rule.ID, rule.Condition)
		if err != nil {
			return cr, err
		}
		cr.condition = prg
		cr.hasCondition = true
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

func (r compiledRule) matches(action *model.Action) bool {
	match := r.rule.Match
	if len(match.ActionTypes) > 0 && !contains(match.ActionTypes, string(action.Type)) {
		return false
	}
	if len(match.ToolNames) > 0 && !contains(match.ToolNames, action.Tool) {
		return false
	}
	if len(r.filePatterns) > 0 && !matchesAnyPath(r.filePatterns, action.Parameters.Path) {
		return false
	}
	if len(r.workingDirPatterns) > 0 && !matchesAnyPath(r.workingDirPatterns, action.WorkingDir) {
		return false
	}
	if len(r.commandPatterns) > 0 && !matchesAnyRegex(r.commandPatterns, action.Parameters.Command) {
		return false
	}
	if len(r.contentPatterns) > 0 && !matchesAnyRegex(r.contentPatterns, action.Parameters.Content) {
		return false
	}
	return true
}

func (r compiledRule) conditionMatches(ctx context.Context, action *model.Action, snapshot *model.ContextSnapshot) (bool, error) {
	if !r.hasCondition {
		return true, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	evalCtx, cancel := context.WithTimeout(ctx, conditionTimeout)
	defer cancel()

	out, _, err := r.condition.ContextEval(evalCtx, map[string]any{
		"action":  actionActivation(action),
		"context": contextActivation(snapshot),
	})
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
			Severity:    r.rule.Severity,
			Tags:        r.rule.Tags,
		},
	}); err != nil {
		return "", fmt.Errorf("rule %q message template: %w", r.rule.ID, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
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

func compileCondition(env *cel.Env, ruleID, expr string) (cel.Program, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return nil, fmt.Errorf("rule %q condition: %w", ruleID, issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("rule %q condition: output type %s, want bool", ruleID, ast.OutputType())
	}
	prg, err := env.Program(ast, cel.CostLimit(10000), cel.InterruptCheckFrequency(100))
	if err != nil {
		return nil, fmt.Errorf("rule %q condition program: %w", ruleID, err)
	}
	return prg, nil
}

func conditionEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("action", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("context", cel.MapType(cel.StringType, cel.DynType)),
	)
}

func actionActivation(action *model.Action) map[string]any {
	if action == nil {
		action = &model.Action{}
	}
	return map[string]any{
		"type":        string(action.Type),
		"tool":        action.Tool,
		"operation":   action.Operation,
		"agent":       action.Agent,
		"working_dir": action.WorkingDir,
		"project":     action.Project,
		"params": map[string]any{
			"path":          action.Parameters.Path,
			"command":       action.Parameters.Command,
			"args":          action.Parameters.Args,
			"url":           action.Parameters.URL,
			"size_bytes":    action.Parameters.SizeBytes,
			"lines_added":   action.Parameters.LinesAdded,
			"lines_removed": action.Parameters.LinesRemoved,
			"content":       action.Parameters.Content,
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
	}
}

package pdp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/aarm/model"
	"gopkg.in/yaml.v3"
)

// Policy is the YAML policy document consumed by the PDP. It must be
// treated as immutable once returned by ParsePolicy or LoadPolicyFile.
// Hash caches its result via sync.Once on first call, so any mutation of
// Rules or Disabled afterwards leaves the cached hash diverged from the
// actual policy contents.
type Policy struct {
	Version  string   `yaml:"version"`
	Rules    []Rule   `yaml:"rules"`
	Disabled []string `yaml:"disabled,omitempty"`

	hashOnce sync.Once
	hash     []byte
	hashErr  error
}

// Rule is a single policy rule.
type Rule struct {
	ID          string         `yaml:"id"`
	Description string         `yaml:"description,omitempty"`
	Action      model.Decision `yaml:"action"`
	Severity    model.Severity `yaml:"severity,omitempty"`
	Enabled     *bool          `yaml:"enabled,omitempty"`
	Tags        []string       `yaml:"tags,omitempty"`
	Message     string         `yaml:"message,omitempty"`
	Match       Match          `yaml:"match,omitempty"`
	Scope       Scope          `yaml:"scope,omitempty"`
	Condition   string         `yaml:"condition,omitempty"`
}

// Match declares rule match criteria.
type Match struct {
	ActionTypes              []string `yaml:"action_types,omitempty"`
	FilePatterns             []string `yaml:"file_patterns,omitempty"`
	CommandPatterns          []string `yaml:"command_patterns,omitempty"`
	ToolNames                []string `yaml:"tool_names,omitempty"`
	ContentPatterns          []string `yaml:"content_patterns,omitempty"`
	WorkingDirectoryPatterns []string `yaml:"working_directory_patterns,omitempty"`
}

// Scope narrows rules to selected agents, projects, or tools.
type Scope struct {
	Agents   []string `yaml:"agents,omitempty"`
	Projects []string `yaml:"projects,omitempty"`
	Tools    []string `yaml:"tools,omitempty"`
}

// ParsePolicy parses and validates a policy document.
func ParsePolicy(data []byte) (*Policy, error) {
	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("pdp: parse policy yaml: %w", err)
	}
	if policy.Rules == nil {
		policy.Rules = []Rule{}
	}
	if err := validatePolicy(&policy); err != nil {
		return nil, err
	}
	if _, err := compileRules(policy.Rules); err != nil {
		return nil, fmt.Errorf("pdp: compile policy: %w", err)
	}
	return &policy, nil
}

// LoadPolicyFile loads and validates a policy file.
func LoadPolicyFile(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pdp: read policy file %q: %w", path, err)
	}
	return ParsePolicy(data)
}

func validatePolicy(policy *Policy) error {
	seen := make(map[string]struct{}, len(policy.Rules))
	for i, rule := range policy.Rules {
		if err := validateRule(rule); err != nil {
			return fmt.Errorf("pdp: invalid rule at index %d: %w", i, err)
		}
		if _, ok := seen[rule.ID]; ok {
			return fmt.Errorf("pdp: duplicate rule id %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}
	return nil
}

func validateRule(rule Rule) error {
	if strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("id must not be empty")
	}
	if !isValidDecision(rule.Action) {
		return fmt.Errorf("rule %q has invalid action %q", rule.ID, rule.Action)
	}
	if !rule.Severity.IsValid() {
		return fmt.Errorf("rule %q has invalid severity %q: must be one of %v", rule.ID, rule.Severity, model.AllSeverities)
	}
	return nil
}

func isRuleEnabled(rule Rule) bool {
	if rule.Enabled == nil {
		return true
	}
	return *rule.Enabled
}

func isValidDecision(d model.Decision) bool {
	switch d {
	case model.DecisionAllow, model.DecisionWarn, model.DecisionGuidance, model.DecisionEscalate, model.DecisionBlock:
		return true
	default:
		return false
	}
}

// Hash returns the SHA-256 over a canonical serialization of the loaded
// policy. The canonical form sorts rules by ID and emits each rule's fields
// in a fixed order. The hash is computed once and cached on the Policy.
// A nil Policy returns a nil hash.
func (p *Policy) Hash() []byte {
	if p == nil {
		return nil
	}
	p.hashOnce.Do(func() {
		p.hash, p.hashErr = computePolicyHash(p)
	})
	if p.hashErr != nil {
		return nil
	}
	out := make([]byte, len(p.hash))
	copy(out, p.hash)
	return out
}

// computePolicyHash builds the canonical JSON representation of the policy
// and returns its SHA-256. Rules are sorted by ID, slices are emitted in a
// stable order so semantic-preserving reorderings hash identically.
func computePolicyHash(p *Policy) ([]byte, error) {
	c := policyCanonical(p)
	buf, err := canonical.MarshalJSON(c)
	if err != nil {
		return nil, fmt.Errorf("pdp: canonicalize policy: %w", err)
	}
	sum := sha256.Sum256(buf)
	return sum[:], nil
}

// policyCanonical materializes the policy into the canonical map shape used
// for hashing. Rules are sorted by ID. Disabled IDs are sorted lexically.
// Empty optional fields are omitted to keep the canonical form stable across
// YAML representations that vary only in whether the optional field is
// present with a zero value.
func policyCanonical(p *Policy) map[string]interface{} {
	out := map[string]interface{}{
		"version": p.Version,
	}
	rules := make([]Rule, len(p.Rules))
	copy(rules, p.Rules)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	canonicalRules := make([]map[string]interface{}, 0, len(rules))
	for _, r := range rules {
		canonicalRules = append(canonicalRules, ruleCanonical(r))
	}
	out["rules"] = canonicalRules
	if len(p.Disabled) > 0 {
		disabled := make([]string, len(p.Disabled))
		copy(disabled, p.Disabled)
		sort.Strings(disabled)
		out["disabled"] = disabled
	}
	return out
}

func ruleCanonical(r Rule) map[string]interface{} {
	m := map[string]interface{}{
		"id":     r.ID,
		"action": string(r.Action),
	}
	if r.Description != "" {
		m["description"] = r.Description
	}
	if r.Severity != "" {
		m["severity"] = string(r.Severity)
	}
	if r.Enabled != nil {
		m["enabled"] = *r.Enabled
	}
	if len(r.Tags) > 0 {
		tags := append([]string(nil), r.Tags...)
		sort.Strings(tags)
		m["tags"] = tags
	}
	if r.Message != "" {
		m["message"] = r.Message
	}
	if mc := matchCanonical(r.Match); len(mc) > 0 {
		m["match"] = mc
	}
	if sc := scopeCanonical(r.Scope); len(sc) > 0 {
		m["scope"] = sc
	}
	if r.Condition != "" {
		m["condition"] = r.Condition
	}
	return m
}

func matchCanonical(m Match) map[string]interface{} {
	out := map[string]interface{}{}
	addSortedStrings(out, "action_types", m.ActionTypes)
	addSortedStrings(out, "file_patterns", m.FilePatterns)
	addSortedStrings(out, "command_patterns", m.CommandPatterns)
	addSortedStrings(out, "tool_names", m.ToolNames)
	addSortedStrings(out, "content_patterns", m.ContentPatterns)
	addSortedStrings(out, "working_directory_patterns", m.WorkingDirectoryPatterns)
	return out
}

func scopeCanonical(s Scope) map[string]interface{} {
	out := map[string]interface{}{}
	addSortedStrings(out, "agents", s.Agents)
	addSortedStrings(out, "projects", s.Projects)
	addSortedStrings(out, "tools", s.Tools)
	return out
}

func addSortedStrings(dst map[string]interface{}, key string, values []string) {
	if len(values) == 0 {
		return
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	dst[key] = sorted
}

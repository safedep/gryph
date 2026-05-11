package pdp

import (
	"fmt"
	"os"
	"strings"

	"github.com/safedep/gryph/aarm/model"
	"gopkg.in/yaml.v3"
)

// Policy is the YAML policy document consumed by the PDP.
type Policy struct {
	Version string `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// Rule is a single policy rule.
type Rule struct {
	ID          string         `yaml:"id"`
	Description string         `yaml:"description,omitempty"`
	Action      model.Decision `yaml:"action"`
	Severity    string         `yaml:"severity,omitempty"`
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

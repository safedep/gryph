// Package security provides a plugin-based security layer for evaluating events.
package security

import "fmt"

// Decision represents the outcome of a security check.
type Decision int

const (
	// DecisionAllow allows the action to proceed.
	DecisionAllow Decision = iota
	// DecisionBlock blocks the action from proceeding.
	DecisionBlock
	// DecisionGuidance allows the action but provides advisory guidance.
	DecisionGuidance

	decisionCount
)

// decisionNames is the one source of decision names. The names travel on the
// wire, so a decision does not depend on the order of the constants.
var decisionNames = [decisionCount]string{
	DecisionAllow:    "allow",
	DecisionBlock:    "block",
	DecisionGuidance: "guidance",
}

// ParseDecision returns the decision with the given name. It reports false
// for a name that this binary does not know.
func ParseDecision(name string) (Decision, bool) {
	for d, n := range decisionNames {
		if n != "" && n == name {
			return Decision(d), true
		}
	}
	return 0, false
}

func (d Decision) name() (string, bool) {
	if d < 0 || d >= decisionCount {
		return "", false
	}
	name := decisionNames[d]
	return name, name != ""
}

// String returns the string representation of the decision.
func (d Decision) String() string {
	if name, ok := d.name(); ok {
		return name
	}
	return "unknown"
}

// MarshalText encodes the decision as its name.
func (d Decision) MarshalText() ([]byte, error) {
	name, ok := d.name()
	if !ok {
		return nil, fmt.Errorf("security: unknown decision %d", int(d))
	}
	return []byte(name), nil
}

// UnmarshalText decodes a decision name.
func (d *Decision) UnmarshalText(text []byte) error {
	parsed, ok := ParseDecision(string(text))
	if !ok {
		return fmt.Errorf("security: unknown decision %q", string(text))
	}
	*d = parsed
	return nil
}

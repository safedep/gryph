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
)

// String returns the string representation of the decision.
func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionBlock:
		return "block"
	case DecisionGuidance:
		return "guidance"
	default:
		return "unknown"
	}
}

// MarshalText encodes the decision as its name, so a decision on the wire
// does not depend on the order of the constants.
func (d Decision) MarshalText() ([]byte, error) {
	switch d {
	case DecisionAllow, DecisionBlock, DecisionGuidance:
		return []byte(d.String()), nil
	default:
		return nil, fmt.Errorf("security: unknown decision %d", int(d))
	}
}

// UnmarshalText decodes a decision name.
func (d *Decision) UnmarshalText(text []byte) error {
	switch string(text) {
	case "allow":
		*d = DecisionAllow
	case "block":
		*d = DecisionBlock
	case "guidance":
		*d = DecisionGuidance
	default:
		return fmt.Errorf("security: unknown decision %q", string(text))
	}
	return nil
}

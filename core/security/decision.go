// Package security provides a plugin-based security layer for evaluating events.
package security

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

package aarm

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// ActionType is the category of an agent action. It re-uses the existing
// events.ActionType vocabulary so the canonical Action stays in sync with
// the event model.
type ActionType = events.ActionType

// Action is the canonical, protocol-agnostic representation of an agent
// operation. The Action Mediation Layer (AML) builds one per incoming
// event; the PDP, Context Accumulator and Receipt Generator all consume
// it. Fields tagged "reserved" are part of the schema for future signals
// (data classification, injection score, original-request capture) — they
// are not populated today and are not surfaced to CEL until they are.
type Action struct {
	ID        uuid.UUID
	Timestamp time.Time
	SessionID uuid.UUID
	EventID   uuid.UUID

	Type       ActionType
	Tool       string
	Operation  string
	Parameters Parameters

	Agent          string
	AgentSessionID string
	WorkingDir     string
	Project        string

	// OriginalRequest is the first user prompt of the session. Reserved.
	OriginalRequest string
	// DataClassifications tags the action with classifications inferred
	// from its parameters or referenced data (e.g. "secret", "pii").
	// Reserved.
	DataClassifications []string
	// InjectionScore is a preliminary prompt-injection risk score in [0,1].
	// Reserved.
	InjectionScore float32
}

// Parameters carries the typed payload of an Action. The full event
// payload remains addressable on the source Event; Parameters holds the
// commonly-matched fields plus a Raw escape hatch for adapter-specific
// data.
type Parameters struct {
	Path         string
	Command      string
	Args         []string
	URL          string
	SizeBytes    int64
	LinesAdded   int
	LinesRemoved int
	Content      string
	Raw          map[string]any
}

// Decision is the outcome of policy evaluation for an Action.
type Decision string

const (
	// DecisionAllow lets the action proceed; no message is surfaced.
	DecisionAllow Decision = "allow"
	// DecisionWarn lets the action proceed silently; the match is
	// recorded only in the audit log (silent detection).
	DecisionWarn Decision = "warn"
	// DecisionGuidance lets the action proceed but surfaces a
	// non-blocking advisory message to the agent or user.
	DecisionGuidance Decision = "guidance"
	// DecisionBlock prevents the action from executing.
	DecisionBlock Decision = "block"
	// DecisionEscalate reserves the slot for human-in-the-loop approval.
	// Until the Approval Service is implemented (Phase 3), this degrades
	// to DecisionBlock with a fixed "requires approval" message.
	DecisionEscalate Decision = "escalate"
)

// Result is the post-execution outcome of an Action, recorded by the
// Context Accumulator on post-hook callbacks. It is intentionally
// minimal; richer signals can be added as the schema evolves.
type Result struct {
	Status   events.ResultStatus
	ExitCode int
	Error    string
	Duration time.Duration
}

// FailMode controls how the engine behaves when policy evaluation
// encounters an internal error.
type FailMode string

const (
	// FailClosed blocks the action on engine error. Default.
	FailClosed FailMode = "closed"
	// FailOpen allows the action on engine error.
	FailOpen FailMode = "open"
)

// parseFailMode converts a config string into a FailMode. Empty defaults
// to FailClosed. Kept unexported — the only caller is the Mediator
// constructor inside this package.
func parseFailMode(s string) (FailMode, error) {
	switch FailMode(s) {
	case "":
		return FailClosed, nil
	case FailClosed:
		return FailClosed, nil
	case FailOpen:
		return FailOpen, nil
	default:
		return "", fmt.Errorf("invalid fail mode: %q", s)
	}
}

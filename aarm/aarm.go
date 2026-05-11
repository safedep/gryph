package aarm

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// ActionType is the canonical action category.
type ActionType = events.ActionType

// Action is the canonical representation of an agent operation.
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

	OriginalRequest string
	DataClassifications []string
	InjectionScore float32
}

// Parameters carries normalized action payload fields.
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

// Decision is the policy evaluation outcome for an action.
type Decision string

const (
	// DecisionAllow permits the action.
	DecisionAllow Decision = "allow"
	// DecisionWarn permits the action and records a warning.
	DecisionWarn Decision = "warn"
	// DecisionGuidance permits the action with guidance.
	DecisionGuidance Decision = "guidance"
	// DecisionBlock denies the action.
	DecisionBlock Decision = "block"
	// DecisionEscalate is reserved for approval workflows.
	DecisionEscalate Decision = "escalate"
)

// Result is the post-execution outcome recorded for an action.
type Result struct {
	Status   events.ResultStatus
	ExitCode int
	Error    string
	Duration time.Duration
}

// FailMode controls behavior on internal evaluation errors.
type FailMode string

const (
	// FailClosed blocks the action on engine errors.
	FailClosed FailMode = "closed"
	// FailOpen allows the action on engine errors.
	FailOpen FailMode = "open"
)

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

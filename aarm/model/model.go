// Package model contains the shared AARM data model.
package model

import (
	"time"

	"github.com/google/uuid"
)

// ActionType is the canonical action category.
type ActionType string

const (
	// ActionFileRead indicates an agent read a file.
	ActionFileRead ActionType = "file_read"
	// ActionFileWrite indicates an agent wrote or modified a file.
	ActionFileWrite ActionType = "file_write"
	// ActionFileDelete indicates an agent deleted a file.
	ActionFileDelete ActionType = "file_delete"
	// ActionCommandExec indicates an agent executed a shell command.
	ActionCommandExec ActionType = "command_exec"
	// ActionNetworkRequest indicates an agent made a network request.
	ActionNetworkRequest ActionType = "network_request"
	// ActionToolUse indicates a generic tool invocation.
	ActionToolUse ActionType = "tool_use"
	// ActionSessionStart indicates a session start action.
	ActionSessionStart ActionType = "session_start"
	// ActionSessionEnd indicates a session end action.
	ActionSessionEnd ActionType = "session_end"
	// ActionNotification indicates an agent notification.
	ActionNotification ActionType = "notification"
	// ActionSubagentStart indicates a subagent start action.
	ActionSubagentStart ActionType = "subagent_start"
	// ActionSubagentStop indicates a subagent stop action.
	ActionSubagentStop ActionType = "subagent_stop"
	// ActionUnknown indicates an unrecognized action.
	ActionUnknown ActionType = "unknown"
)

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

	SubagentID   string
	SubagentType string

	OriginalRequest     string
	DataClassifications []string
	InjectionScore      float32
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
	Status   ResultStatus
	ExitCode int
	Error    string
	Duration time.Duration
}

// ResultStatus is the normalized outcome of a mediated action.
type ResultStatus string

const (
	// ResultSuccess indicates the action completed successfully.
	ResultSuccess ResultStatus = "success"
	// ResultError indicates the action failed.
	ResultError ResultStatus = "error"
	// ResultBlocked indicates the action was blocked.
	ResultBlocked ResultStatus = "blocked"
	// ResultRejected indicates the action was rejected.
	ResultRejected ResultStatus = "rejected"
)

// Severity classifies how serious a rule's decision is. The zero value
// (SeverityUnspecified) means the rule did not assign a severity. The PEP
// boundary maps this onto core/security.Severity. The two types intentionally
// stay independent so AARM internals do not depend on core/security.
type Severity string

const (
	SeverityUnspecified Severity = ""
	SeverityInfo        Severity = "info"
	SeverityLow         Severity = "low"
	SeverityMedium      Severity = "medium"
	SeverityHigh        Severity = "high"
	SeverityCritical    Severity = "critical"
)

// AllSeverities lists every defined severity in ascending order.
var AllSeverities = []Severity{
	SeverityInfo,
	SeverityLow,
	SeverityMedium,
	SeverityHigh,
	SeverityCritical,
}

// IsValid reports whether the severity is unspecified or one of the known
// constants.
func (s Severity) IsValid() bool {
	if s == SeverityUnspecified {
		return true
	}
	for _, known := range AllSeverities {
		if s == known {
			return true
		}
	}
	return false
}

// EvaluationResult is the aggregated PDP decision for an action.
type EvaluationResult struct {
	Decision       Decision
	MatchedRuleIDs []string
	Message        string
	Severity       Severity
	Tags           []string
}

// ContextSnapshot is the point-in-time session context exposed to the PDP.
type ContextSnapshot struct {
	TotalActions     int
	FilesRead        int
	FilesWritten     int
	CommandsExecuted int
	NetworkRequests  int
	Errors           int
	ToolsUsed        []string
	SessionDuration  time.Duration

	ClassificationsSeen []string
	EntitiesSeen        []string
	SemanticDrift       float64
}

// FailMode controls behavior on internal evaluation errors.
type FailMode string

const (
	// FailClosed blocks the action on engine errors.
	FailClosed FailMode = "closed"
	// FailOpen allows the action on engine errors.
	FailOpen FailMode = "open"
)

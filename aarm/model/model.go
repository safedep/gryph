// Package model contains the shared AARM data model.
package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
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
	// ActionUserPrompt indicates a prompt that the user submitted.
	ActionUserPrompt ActionType = "user_prompt"
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

	HumanPrincipal  string
	ServiceIdentity string
	RoleScope       string

	DataClassifications []privacy.Class
	InjectionScore      float32

	// Kind is the kind of the event in the session context. CEL reads it as
	// action.kind.
	Kind EntryKind
	// Origin is where the content of the event came from, as the adapter
	// claims it. Source names the MCP server of an MCP origin. Sources lists
	// every server that the MCP tool name can name, so a deny rule matches
	// when Source is empty.
	Origin  privacy.Origin
	Source  string
	Sources []string

	// Shell is the parsed shell command of a command_exec action. The
	// mediator parses the command once, and the PDP reads the result. It is
	// not part of the receipt.
	Shell *shellcmd.Analysis

	// Phase is the source hook's execution phase, surfaced to CEL as
	// action.phase and persisted on the receipt's action_payload.
	Phase ActionPhase

	// ContentTruncated is true when content matching hit the cap and only a
	// prefix was inspected.
	ContentTruncated bool
}

// ActionPhase records whether the source hook fired before the operation
// executed (PhasePre, enforceable) or after (PhasePost, detection only).
type ActionPhase = events.Phase

const (
	PhaseUnknown = events.PhaseUnknown
	PhasePre     = events.PhasePre
	PhasePost    = events.PhasePost
)

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

	// ContentFull is the untruncated content used only for content_patterns
	// matching. Never persisted, dropped after evaluation, and empty for
	// sensitive paths.
	ContentFull string
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
	// DecisionDefer pauses execution pending operator resolution or timeout.
	DecisionDefer Decision = "defer"
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
	// ResultDeferred indicates the action is paused pending operator resolution.
	ResultDeferred ResultStatus = "deferred"
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
	// Message is the rule message that the receipt and the stored event
	// record. The PDP renders it from the stored action, so it never holds
	// a value that the stored action drops.
	Message string
	// FullMessage is the rule message rendered from the full action. Only
	// the agent and the operator see it. Gryph never stores it.
	FullMessage string
	Severity    Severity
	Tags        []string
	// MatchedTags is the sorted union of the tags of every matched rule, at
	// any decision. The context entry stores it.
	MatchedTags []string

	// DeferReason is set when Decision == DecisionDefer. Either the rule's
	// reason field for explicit defer rules, or one of the synthetic reasons
	// (fresh_session_insufficient_context, conflicting_policies) for the
	// auto-defer triggers.
	DeferReason string
}

// AgentMessage returns the message for the agent and the operator. A result
// with no FullMessage returns Message.
func (r *EvaluationResult) AgentMessage() string {
	if r.FullMessage != "" {
		return r.FullMessage
	}
	return r.Message
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
	// TagsSeen maps each tag that a rule put on an earlier entry to the
	// sequence of the first entry that has it.
	TagsSeen     map[string]int64
	OriginsSeen  []string
	EntitiesSeen []string
	// EgressHosts are the hosts that earlier actions contacted. The pending
	// action is not in it, because a rule decides whether it may.
	EgressHosts []string
	// Entries are the latest stored entries, oldest first. The Mediator
	// loads them only when a rule reads context.entries.
	Entries []EntryFacts

	// IntentAvailable is true when the session has at least one intent
	// entry. An agent with no prompt hook never has one.
	IntentAvailable bool
	// ActionsSinceIntent counts the action entries after the latest intent.
	ActionsSinceIntent int

	// SessionStartedAt is the start time of the agent session. It is zero
	// when the caller has no session. The fresh-session defer trigger reads
	// it. It is not part of the receipt snapshot, the CEL context or the
	// message template context. Adding it to the receipt snapshot changes the
	// receipt hash format.
	SessionStartedAt time.Time
}

// FailMode controls behavior on internal evaluation errors.
type FailMode string

const (
	// FailClosed blocks the action on engine errors.
	FailClosed FailMode = "closed"
	// FailOpen allows the action on engine errors.
	FailOpen FailMode = "open"
)

// EntryKind is the kind of a context entry: intent, action, or observation.
type EntryKind = events.Kind

// DerivedTarget holds the target values that Gryph computes for an entry.
// Host is the lower-case host from a URL, a network command, or an MCP
// server URL.
type DerivedTarget struct {
	Host      string
	MCPServer string
	MCPTool   string
}

// ContextEntry is one entry of the session context. It holds facts only.
// The path, the command, and the content stay on the audit event.
type ContextEntry struct {
	ID              uuid.UUID
	SessionID       uuid.UUID
	EventID         uuid.UUID
	LinkedEventID   uuid.UUID
	Sequence        int64
	Kind            EntryKind
	Timestamp       time.Time
	ActionType      ActionType
	Tool            string
	ToolCallID      string
	Phase           ActionPhase
	Target          DerivedTarget
	Origin          privacy.Origin
	Tags            []string
	Classifications []privacy.Class
	InjectionScore  float32
	Decision        Decision
	MatchedRuleIDs  []string
	// ContentDigest is the SHA-256 of the event content before redaction.
	// The entry never holds the content.
	ContentDigest string
	Result        ResultStatus

	// Hosts and Entities feed the session state. The entry stores neither,
	// and the hash does not cover them. Target.Host is the first host.
	// Entities holds path:, host: and mcp: keys.
	Hosts    []string
	Entities []string
}

// EntryFacts is one item of context.entries. Path and Command come from the
// audit event of the entry. Command is the stored command, after redaction.
// No item holds content.
type EntryFacts struct {
	Seq        int64
	Kind       string
	ActionType string
	Tool       string
	Path       string
	Command    string
	Host       string
	MCPServer  string
	Origin     string
	Classes    []string
	Tags       []string
	Decision   string
	Result     string
}

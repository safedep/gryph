// Package session provides the session model for agent working sessions.
package session

import (
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/cost"
)

// Session represents a single agent working session.
type Session struct {
	// ID is the unique identifier for this session.
	// For agents that provide their own session_id, this is a deterministic UUID derived from it.
	ID uuid.UUID `json:"id"`
	// AgentSessionID is the original session ID from the agent (e.g., Claude Code's session_id).
	// Stored for correlation with agent's own storage. May be empty if agent doesn't provide one.
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// AgentName is the agent identifier (e.g., "claude-code").
	AgentName string `json:"agent_name"`
	// AgentVersion is the agent version if detectable.
	AgentVersion string `json:"agent_version,omitempty"`
	// StartedAt is the session start time (UTC).
	StartedAt time.Time `json:"started_at"`
	// EndedAt is the session end time (UTC), zero if ongoing.
	EndedAt time.Time `json:"ended_at,omitempty"`
	// WorkingDirectory is the absolute path where agent was invoked.
	WorkingDirectory string `json:"working_directory,omitempty"`
	// ProjectName is detected from package.json, Cargo.toml, etc.
	ProjectName string `json:"project_name,omitempty"`
	// TotalActions is the count of events (denormalized).
	TotalActions int `json:"total_actions"`
	// FilesRead is the count of file_read actions.
	FilesRead int `json:"files_read"`
	// FilesWritten is the count of file_write actions.
	FilesWritten int `json:"files_written"`
	// CommandsExecuted is the count of command_exec actions.
	CommandsExecuted int `json:"commands_executed"`
	// Errors is the count of events with error status.
	Errors int `json:"errors"`
	SensitiveActions int `json:"sensitive_actions"`
	BlockedActions int `json:"blocked_actions"`
	// TranscriptPath is the path to the agent's transcript file.
	TranscriptPath string `json:"transcript_path,omitempty"`
	// InputTokens is the total input tokens across all models (denormalized).
	InputTokens int64 `json:"input_tokens,omitempty"`
	// OutputTokens is the total output tokens across all models (denormalized).
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// CacheReadTokens is the total cache read tokens (denormalized).
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens is the total cache write tokens (denormalized).
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	// EstimatedCostUSD is the estimated total cost in USD (denormalized).
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`
	// ModelUsage stores the per-model token breakdown (source of truth).
	ModelUsage []cost.ModelUsage `json:"model_usage,omitempty"`
	// CostSource indicates where cost data came from.
	CostSource string `json:"cost_source,omitempty"`
	// CostComputedAt is when cost was last computed.
	CostComputedAt *time.Time `json:"cost_computed_at,omitempty"`
}

// NewSession creates a new Session with a generated UUID and current timestamp.
func NewSession(agentName string) *Session {
	return &Session{
		ID:        uuid.New(),
		AgentName: agentName,
		StartedAt: time.Now().UTC(),
	}
}

// NewSessionWithID creates a new Session with the given ID.
func NewSessionWithID(id uuid.UUID, agentName string) *Session {
	return &Session{
		ID:        id,
		AgentName: agentName,
		StartedAt: time.Now().UTC(),
	}
}

// IsActive returns true if the session has not ended.
func (s *Session) IsActive() bool {
	return s.EndedAt.IsZero()
}

// Duration returns the duration of the session.
// If the session is still active, it returns the duration since start.
func (s *Session) Duration() time.Duration {
	if s.IsActive() {
		return time.Since(s.StartedAt)
	}
	return s.EndedAt.Sub(s.StartedAt)
}

// End marks the session as ended with the current timestamp.
func (s *Session) End() {
	s.EndedAt = time.Now().UTC()
}

// HasCostData returns true if cost data has been computed for this session.
func (s *Session) HasCostData() bool {
	return s.CostComputedAt != nil
}

// SessionFilter provides filtering criteria for querying sessions.
type SessionFilter struct {
	// AgentName filters by a specific agent.
	AgentName string
	// Since filters sessions started after this time.
	Since *time.Time
	// Until filters sessions started before this time.
	Until *time.Time
	// ActiveOnly filters to only active sessions.
	ActiveOnly bool
	// Limit is the maximum number of results.
	Limit int
	// Offset is the number of results to skip.
	Offset int
	AgentNames     []string
	HasErrors      *bool
	HasSensitive   *bool
	HasBlocked     *bool
	EventSince     *time.Time
	EventUntil     *time.Time
	EventActions   []string
	EventStatuses  []string
	FilePattern    string
	CommandPattern string
}

// NewSessionFilter creates a new SessionFilter with default values.
func NewSessionFilter() *SessionFilter {
	return &SessionFilter{
		Limit: 20,
	}
}

// WithAgent sets the AgentName filter.
func (f *SessionFilter) WithAgent(agent string) *SessionFilter {
	f.AgentName = agent
	return f
}

// WithSince sets the Since filter.
func (f *SessionFilter) WithSince(t time.Time) *SessionFilter {
	f.Since = &t
	return f
}

// WithUntil sets the Until filter.
func (f *SessionFilter) WithUntil(t time.Time) *SessionFilter {
	f.Until = &t
	return f
}

// WithLimit sets the Limit.
func (f *SessionFilter) WithLimit(limit int) *SessionFilter {
	f.Limit = limit
	return f
}

func (f *SessionFilter) WithAgents(agents []string) *SessionFilter {
	f.AgentNames = agents
	return f
}

func (f *SessionFilter) WithHasErrors(v bool) *SessionFilter {
	f.HasErrors = &v
	return f
}

func (f *SessionFilter) WithHasSensitive(v bool) *SessionFilter {
	f.HasSensitive = &v
	return f
}

func (f *SessionFilter) WithHasBlocked(v bool) *SessionFilter {
	f.HasBlocked = &v
	return f
}

func (f *SessionFilter) WithEventSince(t time.Time) *SessionFilter {
	f.EventSince = &t
	return f
}

func (f *SessionFilter) WithEventUntil(t time.Time) *SessionFilter {
	f.EventUntil = &t
	return f
}

func (f *SessionFilter) WithEventActions(actions []string) *SessionFilter {
	f.EventActions = actions
	return f
}

func (f *SessionFilter) WithEventStatuses(statuses []string) *SessionFilter {
	f.EventStatuses = statuses
	return f
}

func (f *SessionFilter) WithFilePattern(p string) *SessionFilter {
	f.FilePattern = p
	return f
}

func (f *SessionFilter) WithCommandPattern(p string) *SessionFilter {
	f.CommandPattern = p
	return f
}

// Package session provides the session model for agent working sessions.
package session

import (
	"time"

	"github.com/google/uuid"
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

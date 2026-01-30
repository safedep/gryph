package session

import (
	"time"
)

// SessionStats provides aggregated statistics about sessions.
type SessionStats struct {
	// TotalSessions is the total number of sessions.
	TotalSessions int `json:"total_sessions"`
	// ActiveSessions is the number of currently active sessions.
	ActiveSessions int `json:"active_sessions"`
	// TotalEvents is the total number of events across all sessions.
	TotalEvents int `json:"total_events"`
	// TotalFilesRead is the total files read across all sessions.
	TotalFilesRead int `json:"total_files_read"`
	// TotalFilesWritten is the total files written across all sessions.
	TotalFilesWritten int `json:"total_files_written"`
	// TotalCommandsExecuted is the total commands executed across all sessions.
	TotalCommandsExecuted int `json:"total_commands_executed"`
	// TotalErrors is the total errors across all sessions.
	TotalErrors int `json:"total_errors"`
	// OldestSession is the timestamp of the oldest session.
	OldestSession time.Time `json:"oldest_session,omitempty"`
	// NewestSession is the timestamp of the newest session.
	NewestSession time.Time `json:"newest_session,omitempty"`
	// SessionsByAgent maps agent names to session counts.
	SessionsByAgent map[string]int `json:"sessions_by_agent,omitempty"`
}

// NewSessionStats creates a new empty SessionStats.
func NewSessionStats() *SessionStats {
	return &SessionStats{
		SessionsByAgent: make(map[string]int),
	}
}

// AddSession updates the stats with data from a session.
func (s *SessionStats) AddSession(session *Session) {
	s.TotalSessions++
	if session.IsActive() {
		s.ActiveSessions++
	}

	s.TotalEvents += session.TotalActions
	s.TotalFilesRead += session.FilesRead
	s.TotalFilesWritten += session.FilesWritten
	s.TotalCommandsExecuted += session.CommandsExecuted
	s.TotalErrors += session.Errors

	if s.OldestSession.IsZero() || session.StartedAt.Before(s.OldestSession) {
		s.OldestSession = session.StartedAt
	}
	if s.NewestSession.IsZero() || session.StartedAt.After(s.NewestSession) {
		s.NewestSession = session.StartedAt
	}

	s.SessionsByAgent[session.AgentName]++
}

// Summary provides a human-readable summary of session activity.
type Summary struct {
	// ActionsCount is the total number of actions.
	ActionsCount int
	// LinesChanged is the total lines added + removed.
	LinesChanged int
	// LinesAdded is the total lines added.
	LinesAdded int
	// LinesRemoved is the total lines removed.
	LinesRemoved int
	// CommandsCount is the number of commands executed.
	CommandsCount int
	// CommandsPassed is the number of commands that passed (exit 0).
	CommandsPassed int
	// CommandsFailed is the number of commands that failed (non-zero exit).
	CommandsFailed int
	// FilesReadCount is the number of files read.
	FilesReadCount int
	// FilesWrittenCount is the number of files written.
	FilesWrittenCount int
}

// NewSummary creates a new empty Summary.
func NewSummary() *Summary {
	return &Summary{}
}

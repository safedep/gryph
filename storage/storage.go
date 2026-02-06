// Package storage provides database storage interfaces and implementations.
package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// EventStore defines the interface for storing and querying audit events.
type EventStore interface {
	// SaveEvent persists a new audit event.
	SaveEvent(ctx context.Context, event *events.Event) error

	// GetEvent retrieves an event by ID.
	GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error)

	// GetEventByPrefix retrieves an event by ID prefix.
	GetEventByPrefix(ctx context.Context, prefix string) (*events.Event, error)

	// QueryEvents retrieves events matching the given filter.
	QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error)

	// CountEvents returns the count of events matching the given filter.
	CountEvents(ctx context.Context, filter *events.EventFilter) (int, error)

	// GetEventsBySession retrieves all events for a session.
	GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error)

	// DeleteEventsBefore deletes events older than the given time.
	DeleteEventsBefore(ctx context.Context, before time.Time) (int, error)

	// CountEventsBefore returns the count of events older than the given time.
	CountEventsBefore(ctx context.Context, before time.Time) (int, error)

	// QueryEventsAfter retrieves events after the given time, ordered ascending.
	// When afterID is non-nil, a compound cursor (timestamp, id) is used so that
	// events sharing the same timestamp as after are only included when their ID
	// is greater than afterID. This prevents skipping records at batch boundaries.
	QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error)
}

// SessionStore defines the interface for storing and querying sessions.
type SessionStore interface {
	// SaveSession persists a new session.
	SaveSession(ctx context.Context, sess *session.Session) error

	// UpdateSession updates an existing session.
	UpdateSession(ctx context.Context, sess *session.Session) error

	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error)

	// GetSessionByPrefix retrieves a session by ID prefix.
	GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error)

	// QuerySessions retrieves sessions matching the given filter.
	QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error)

	// GetActiveSession retrieves the active session for an agent, if any.
	GetActiveSession(ctx context.Context, agentName string) (*session.Session, error)

	// GetSessionStats retrieves aggregated session statistics.
	GetSessionStats(ctx context.Context) (*session.SessionStats, error)
}

// SelfAuditStore defines the interface for storing self-audit entries.
type SelfAuditStore interface {
	// SaveSelfAudit persists a self-audit entry.
	SaveSelfAudit(ctx context.Context, entry *SelfAuditEntry) error

	// QuerySelfAudits retrieves self-audit entries matching the filter.
	QuerySelfAudits(ctx context.Context, filter *SelfAuditFilter) ([]*SelfAuditEntry, error)

	// QuerySelfAuditsAfter retrieves self-audit entries after the given time, ordered ascending.
	// When afterID is non-nil, a compound cursor (timestamp, id) is used to avoid
	// skipping records that share the same timestamp at batch boundaries.
	QuerySelfAuditsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*SelfAuditEntry, error)
}

// StreamCheckpointStore defines the interface for stream sync checkpoints.
type StreamCheckpointStore interface {
	// GetStreamCheckpoint retrieves a checkpoint by target name.
	GetStreamCheckpoint(ctx context.Context, targetName string) (*StreamCheckpoint, error)

	// SaveStreamCheckpoint persists a stream checkpoint.
	SaveStreamCheckpoint(ctx context.Context, checkpoint *StreamCheckpoint) error
}

// StreamCheckpoint represents the sync state for a stream target.
type StreamCheckpoint struct {
	TargetName      string
	LastSyncedAt    time.Time
	LastEventID     string
	LastSelfAuditID string
}

// SelfAuditEntry represents a self-audit log entry for storage.
type SelfAuditEntry struct {
	ID           uuid.UUID
	Timestamp    time.Time
	Action       string
	AgentName    string
	Details      map[string]interface{}
	Result       string
	ErrorMessage string
	ToolVersion  string
}

// SelfAuditFilter provides filtering for self-audit queries.
type SelfAuditFilter struct {
	Since  *time.Time
	Action string
	Limit  int
}

// Store combines all storage interfaces.
type Store interface {
	EventStore
	SessionStore
	SelfAuditStore
	StreamCheckpointStore

	// Init initializes the database schema.
	Init(ctx context.Context) error

	// Close closes the database connection.
	Close() error
}

// DatabaseInfo contains information about the database.
type DatabaseInfo struct {
	Path         string
	SizeBytes    int64
	EventCount   int
	SessionCount int
	OldestEvent  time.Time
	NewestEvent  time.Time
}

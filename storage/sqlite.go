package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage/ent"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite via ent.
type SQLiteStore struct {
	client *ent.Client
	db     *sql.DB
	path   string
}

// NewSQLiteStore creates a new SQLite store at the given path.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(getDir(path), 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with modernc.org/sqlite driver
	// Use _pragma=foreign_keys(1) for modernc.org/sqlite
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", path))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create ent driver from sql.DB
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	return &SQLiteStore{
		client: client,
		db:     db,
		path:   path,
	}, nil
}

// Init initializes the database schema.
func (s *SQLiteStore) Init(ctx context.Context) error {
	if err := s.client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if err := s.client.Close(); err != nil {
		return err
	}
	return s.db.Close()
}

// SaveEvent persists a new audit event.
func (s *SQLiteStore) SaveEvent(ctx context.Context, event *events.Event) error {
	// TODO: Implement actual storage logic
	return nil
}

// GetEvent retrieves an event by ID.
func (s *SQLiteStore) GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error) {
	// TODO: Implement actual retrieval logic
	return nil, nil
}

// QueryEvents retrieves events matching the given filter.
func (s *SQLiteStore) QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error) {
	// TODO: Implement actual query logic
	return []*events.Event{}, nil
}

// CountEvents returns the count of events matching the given filter.
func (s *SQLiteStore) CountEvents(ctx context.Context, filter *events.EventFilter) (int, error) {
	// TODO: Implement actual count logic
	return 0, nil
}

// GetEventsBySession retrieves all events for a session.
func (s *SQLiteStore) GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error) {
	// TODO: Implement actual retrieval logic
	return []*events.Event{}, nil
}

// DeleteEventsBefore deletes events older than the given time.
func (s *SQLiteStore) DeleteEventsBefore(ctx context.Context, before time.Time) (int, error) {
	// TODO: Implement actual deletion logic
	return 0, nil
}

// SaveSession persists a new session.
func (s *SQLiteStore) SaveSession(ctx context.Context, sess *session.Session) error {
	// TODO: Implement actual storage logic
	return nil
}

// UpdateSession updates an existing session.
func (s *SQLiteStore) UpdateSession(ctx context.Context, sess *session.Session) error {
	// TODO: Implement actual update logic
	return nil
}

// GetSession retrieves a session by ID.
func (s *SQLiteStore) GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error) {
	// TODO: Implement actual retrieval logic
	return nil, nil
}

// GetSessionByPrefix retrieves a session by ID prefix.
func (s *SQLiteStore) GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error) {
	// TODO: Implement actual retrieval logic
	return nil, nil
}

// QuerySessions retrieves sessions matching the given filter.
func (s *SQLiteStore) QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error) {
	// TODO: Implement actual query logic
	return []*session.Session{}, nil
}

// GetActiveSession retrieves the active session for an agent, if any.
func (s *SQLiteStore) GetActiveSession(ctx context.Context, agentName string) (*session.Session, error) {
	// TODO: Implement actual retrieval logic
	return nil, nil
}

// GetSessionStats retrieves aggregated session statistics.
func (s *SQLiteStore) GetSessionStats(ctx context.Context) (*session.SessionStats, error) {
	// TODO: Implement actual stats calculation
	return session.NewSessionStats(), nil
}

// SaveSelfAudit persists a self-audit entry.
func (s *SQLiteStore) SaveSelfAudit(ctx context.Context, entry *SelfAuditEntry) error {
	// TODO: Implement actual storage logic
	return nil
}

// QuerySelfAudits retrieves self-audit entries matching the filter.
func (s *SQLiteStore) QuerySelfAudits(ctx context.Context, filter *SelfAuditFilter) ([]*SelfAuditEntry, error) {
	// TODO: Implement actual query logic
	return []*SelfAuditEntry{}, nil
}

// GetDatabaseInfo returns information about the database.
func (s *SQLiteStore) GetDatabaseInfo(ctx context.Context) (*DatabaseInfo, error) {
	info := &DatabaseInfo{
		Path: s.path,
	}

	// Get file size
	if stat, err := os.Stat(s.path); err == nil {
		info.SizeBytes = stat.Size()
	}

	// TODO: Get actual counts and timestamps
	info.EventCount = 0
	info.SessionCount = 0

	return info, nil
}

// getDir returns the directory portion of a path.
func getDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// Ensure SQLiteStore implements Store
var _ Store = (*SQLiteStore)(nil)

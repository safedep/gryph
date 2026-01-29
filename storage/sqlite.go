package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage/ent"
	"github.com/safedep/gryph/storage/ent/auditevent"
	"github.com/safedep/gryph/storage/ent/predicate"
	"github.com/safedep/gryph/storage/ent/selfaudit"
	entsession "github.com/safedep/gryph/storage/ent/session"

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
	// Convert payload to map
	var payload map[string]interface{}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	// Convert raw event to map
	var rawEvent map[string]interface{}
	if len(event.RawEvent) > 0 {
		if err := json.Unmarshal(event.RawEvent, &rawEvent); err != nil {
			return fmt.Errorf("failed to unmarshal raw event: %w", err)
		}
	}

	// Build the create query
	create := s.client.AuditEvent.Create().
		SetID(event.ID).
		SetSessionID(event.SessionID).
		SetSequence(event.Sequence).
		SetTimestamp(event.Timestamp).
		SetAgentName(event.AgentName).
		SetActionType(auditevent.ActionType(event.ActionType)).
		SetResultStatus(auditevent.ResultStatus(event.ResultStatus)).
		SetIsSensitive(event.IsSensitive)

	// Set optional fields
	if event.DurationMs > 0 {
		create.SetDurationMs(event.DurationMs)
	}
	if event.AgentVersion != "" {
		create.SetAgentVersion(event.AgentVersion)
	}
	if event.WorkingDirectory != "" {
		create.SetWorkingDirectory(event.WorkingDirectory)
	}
	if event.ToolName != "" {
		create.SetToolName(event.ToolName)
	}
	if event.ErrorMessage != "" {
		create.SetErrorMessage(event.ErrorMessage)
	}
	if payload != nil {
		create.SetPayload(payload)
	}
	if event.DiffContent != "" {
		create.SetDiffContent(event.DiffContent)
	}
	if rawEvent != nil {
		create.SetRawEvent(rawEvent)
	}
	if event.ConversationContext != "" {
		create.SetConversationContext(event.ConversationContext)
	}

	_, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	return nil
}

// GetEvent retrieves an event by ID.
func (s *SQLiteStore) GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error) {
	entEvent, err := s.client.AuditEvent.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	return entToEvent(entEvent), nil
}

// QueryEvents retrieves events matching the given filter.
func (s *SQLiteStore) QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error) {
	query := s.client.AuditEvent.Query()

	// Apply predicates from filter
	predicates := buildEventPredicates(filter)
	if len(predicates) > 0 {
		query.Where(predicates...)
	}

	// Apply ordering (newest first)
	query.Order(auditevent.ByTimestamp(entsql.OrderDesc()))

	// Apply limit and offset
	if filter.Limit > 0 {
		query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query.Offset(filter.Offset)
	}

	entEvents, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	result := make([]*events.Event, len(entEvents))
	for i, e := range entEvents {
		result[i] = entToEvent(e)
	}

	return result, nil
}

// CountEvents returns the count of events matching the given filter.
func (s *SQLiteStore) CountEvents(ctx context.Context, filter *events.EventFilter) (int, error) {
	query := s.client.AuditEvent.Query()

	// Apply predicates from filter
	predicates := buildEventPredicates(filter)
	if len(predicates) > 0 {
		query.Where(predicates...)
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}

	return count, nil
}

// GetEventsBySession retrieves all events for a session.
func (s *SQLiteStore) GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error) {
	entEvents, err := s.client.AuditEvent.Query().
		Where(auditevent.SessionIDEQ(sessionID)).
		Order(auditevent.BySequence()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get events by session: %w", err)
	}

	result := make([]*events.Event, len(entEvents))
	for i, e := range entEvents {
		result[i] = entToEvent(e)
	}

	return result, nil
}

// DeleteEventsBefore deletes events older than the given time.
func (s *SQLiteStore) DeleteEventsBefore(ctx context.Context, before time.Time) (int, error) {
	deleted, err := s.client.AuditEvent.Delete().
		Where(auditevent.TimestampLT(before)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete events: %w", err)
	}

	return deleted, nil
}

// SaveSession persists a new session.
func (s *SQLiteStore) SaveSession(ctx context.Context, sess *session.Session) error {
	create := s.client.Session.Create().
		SetID(sess.ID).
		SetAgentName(sess.AgentName).
		SetStartedAt(sess.StartedAt).
		SetTotalActions(sess.TotalActions).
		SetFilesRead(sess.FilesRead).
		SetFilesWritten(sess.FilesWritten).
		SetCommandsExecuted(sess.CommandsExecuted).
		SetErrors(sess.Errors)

	// Set optional fields
	if sess.AgentSessionID != "" {
		create.SetAgentSessionID(sess.AgentSessionID)
	}
	if sess.AgentVersion != "" {
		create.SetAgentVersion(sess.AgentVersion)
	}
	if sess.WorkingDirectory != "" {
		create.SetWorkingDirectory(sess.WorkingDirectory)
	}
	if sess.ProjectName != "" {
		create.SetProjectName(sess.ProjectName)
	}
	if !sess.EndedAt.IsZero() {
		create.SetEndedAt(sess.EndedAt)
	}

	_, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// UpdateSession updates an existing session.
func (s *SQLiteStore) UpdateSession(ctx context.Context, sess *session.Session) error {
	update := s.client.Session.UpdateOneID(sess.ID).
		SetTotalActions(sess.TotalActions).
		SetFilesRead(sess.FilesRead).
		SetFilesWritten(sess.FilesWritten).
		SetCommandsExecuted(sess.CommandsExecuted).
		SetErrors(sess.Errors)

	// Update optional fields
	if sess.AgentVersion != "" {
		update.SetAgentVersion(sess.AgentVersion)
	}
	if sess.WorkingDirectory != "" {
		update.SetWorkingDirectory(sess.WorkingDirectory)
	}
	if sess.ProjectName != "" {
		update.SetProjectName(sess.ProjectName)
	}
	if !sess.EndedAt.IsZero() {
		update.SetEndedAt(sess.EndedAt)
	}

	_, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID.
func (s *SQLiteStore) GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error) {
	entSession, err := s.client.Session.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return entToSession(entSession), nil
}

// GetSessionByPrefix retrieves a session by ID prefix.
func (s *SQLiteStore) GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error) {
	// Use raw SQL to match UUID string prefix since ent doesn't support this natively
	sessions, err := s.client.Session.Query().
		Where(func(s *entsql.Selector) {
			s.Where(entsql.Like(entsession.FieldID, prefix+"%"))
		}).
		Limit(1).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get session by prefix: %w", err)
	}

	if len(sessions) == 0 {
		return nil, nil
	}

	return entToSession(sessions[0]), nil
}

// QuerySessions retrieves sessions matching the given filter.
func (s *SQLiteStore) QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error) {
	query := s.client.Session.Query()

	// Apply predicates from filter
	predicates := buildSessionPredicates(filter)
	if len(predicates) > 0 {
		query.Where(predicates...)
	}

	// Apply ordering (newest first)
	query.Order(entsession.ByStartedAt(entsql.OrderDesc()))

	// Apply limit and offset
	if filter.Limit > 0 {
		query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query.Offset(filter.Offset)
	}

	entSessions, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	result := make([]*session.Session, len(entSessions))
	for i, s := range entSessions {
		result[i] = entToSession(s)
	}

	return result, nil
}

// GetActiveSession retrieves the active session for an agent, if any.
func (s *SQLiteStore) GetActiveSession(ctx context.Context, agentName string) (*session.Session, error) {
	entSession, err := s.client.Session.Query().
		Where(
			entsession.AgentNameEQ(agentName),
			entsession.EndedAtIsNil(),
		).
		Order(entsession.ByStartedAt(entsql.OrderDesc())).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}

	return entToSession(entSession), nil
}

// GetSessionStats retrieves aggregated session statistics.
func (s *SQLiteStore) GetSessionStats(ctx context.Context) (*session.SessionStats, error) {
	stats := session.NewSessionStats()

	// Get all sessions for aggregation
	sessions, err := s.client.Session.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions for stats: %w", err)
	}

	for _, entSess := range sessions {
		sess := entToSession(entSess)
		stats.AddSession(sess)
	}

	return stats, nil
}

// SaveSelfAudit persists a self-audit entry.
func (s *SQLiteStore) SaveSelfAudit(ctx context.Context, entry *SelfAuditEntry) error {
	create := s.client.SelfAudit.Create().
		SetID(entry.ID).
		SetTimestamp(entry.Timestamp).
		SetAction(selfaudit.Action(entry.Action)).
		SetResult(selfaudit.Result(entry.Result)).
		SetToolVersion(entry.ToolVersion)

	// Set optional fields
	if entry.AgentName != "" {
		create.SetAgentName(entry.AgentName)
	}
	if entry.Details != nil {
		create.SetDetails(entry.Details)
	}
	if entry.ErrorMessage != "" {
		create.SetErrorMessage(entry.ErrorMessage)
	}

	_, err := create.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to save self-audit: %w", err)
	}

	return nil
}

// QuerySelfAudits retrieves self-audit entries matching the filter.
func (s *SQLiteStore) QuerySelfAudits(ctx context.Context, filter *SelfAuditFilter) ([]*SelfAuditEntry, error) {
	query := s.client.SelfAudit.Query()

	// Apply predicates from filter
	var predicates []predicate.SelfAudit
	if filter.Since != nil {
		predicates = append(predicates, selfaudit.TimestampGTE(*filter.Since))
	}
	if filter.Action != "" {
		predicates = append(predicates, selfaudit.ActionEQ(selfaudit.Action(filter.Action)))
	}
	if len(predicates) > 0 {
		query.Where(predicates...)
	}

	// Apply ordering (newest first)
	query.Order(selfaudit.ByTimestamp(entsql.OrderDesc()))

	// Apply limit
	if filter.Limit > 0 {
		query.Limit(filter.Limit)
	}

	entAudits, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query self-audits: %w", err)
	}

	result := make([]*SelfAuditEntry, len(entAudits))
	for i, a := range entAudits {
		result[i] = entToSelfAudit(a)
	}

	return result, nil
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

	// Get event count
	eventCount, err := s.client.AuditEvent.Query().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count events: %w", err)
	}
	info.EventCount = eventCount

	// Get session count
	sessionCount, err := s.client.Session.Query().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count sessions: %w", err)
	}
	info.SessionCount = sessionCount

	// Get oldest event timestamp
	oldestEvent, err := s.client.AuditEvent.Query().
		Order(auditevent.ByTimestamp()).
		First(ctx)
	if err == nil {
		info.OldestEvent = oldestEvent.Timestamp
	}

	// Get newest event timestamp
	newestEvent, err := s.client.AuditEvent.Query().
		Order(auditevent.ByTimestamp(entsql.OrderDesc())).
		First(ctx)
	if err == nil {
		info.NewestEvent = newestEvent.Timestamp
	}

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

// Helper functions for mapping between domain types and ent types

// entToEvent converts an ent AuditEvent to a domain Event.
func entToEvent(e *ent.AuditEvent) *events.Event {
	event := &events.Event{
		ID:                  e.ID,
		SessionID:           e.SessionID,
		Sequence:            e.Sequence,
		Timestamp:           e.Timestamp,
		AgentName:           e.AgentName,
		AgentVersion:        e.AgentVersion,
		WorkingDirectory:    e.WorkingDirectory,
		ActionType:          events.ActionType(e.ActionType),
		ToolName:            e.ToolName,
		ResultStatus:        events.ResultStatus(e.ResultStatus),
		ErrorMessage:        e.ErrorMessage,
		DiffContent:         e.DiffContent,
		ConversationContext: e.ConversationContext,
		IsSensitive:         e.IsSensitive,
	}

	if e.DurationMs != nil {
		event.DurationMs = *e.DurationMs
	}

	// Convert payload map to JSON
	if e.Payload != nil {
		if data, err := json.Marshal(e.Payload); err == nil {
			event.Payload = data
		}
	}

	// Convert raw event map to JSON
	if e.RawEvent != nil {
		if data, err := json.Marshal(e.RawEvent); err == nil {
			event.RawEvent = data
		}
	}

	return event
}

// entToSession converts an ent Session to a domain Session.
func entToSession(e *ent.Session) *session.Session {
	sess := &session.Session{
		ID:               e.ID,
		AgentSessionID:   e.AgentSessionID,
		AgentName:        e.AgentName,
		AgentVersion:     e.AgentVersion,
		StartedAt:        e.StartedAt,
		WorkingDirectory: e.WorkingDirectory,
		ProjectName:      e.ProjectName,
		TotalActions:     e.TotalActions,
		FilesRead:        e.FilesRead,
		FilesWritten:     e.FilesWritten,
		CommandsExecuted: e.CommandsExecuted,
		Errors:           e.Errors,
	}

	if e.EndedAt != nil {
		sess.EndedAt = *e.EndedAt
	}

	return sess
}

// entToSelfAudit converts an ent SelfAudit to a domain SelfAuditEntry.
func entToSelfAudit(e *ent.SelfAudit) *SelfAuditEntry {
	return &SelfAuditEntry{
		ID:           e.ID,
		Timestamp:    e.Timestamp,
		Action:       string(e.Action),
		AgentName:    e.AgentName,
		Details:      e.Details,
		Result:       string(e.Result),
		ErrorMessage: e.ErrorMessage,
		ToolVersion:  e.ToolVersion,
	}
}

// buildEventPredicates builds ent predicates from an EventFilter.
func buildEventPredicates(filter *events.EventFilter) []predicate.AuditEvent {
	var predicates []predicate.AuditEvent

	if filter.Since != nil {
		predicates = append(predicates, auditevent.TimestampGTE(*filter.Since))
	}
	if filter.Until != nil {
		predicates = append(predicates, auditevent.TimestampLTE(*filter.Until))
	}
	if len(filter.AgentNames) > 0 {
		predicates = append(predicates, auditevent.AgentNameIn(filter.AgentNames...))
	}
	if filter.SessionID != nil {
		predicates = append(predicates, auditevent.SessionIDEQ(*filter.SessionID))
	}
	if len(filter.ActionTypes) > 0 {
		types := make([]auditevent.ActionType, len(filter.ActionTypes))
		for i, t := range filter.ActionTypes {
			types[i] = auditevent.ActionType(t)
		}
		predicates = append(predicates, auditevent.ActionTypeIn(types...))
	}
	if len(filter.ResultStatuses) > 0 {
		statuses := make([]auditevent.ResultStatus, len(filter.ResultStatuses))
		for i, s := range filter.ResultStatuses {
			statuses[i] = auditevent.ResultStatus(s)
		}
		predicates = append(predicates, auditevent.ResultStatusIn(statuses...))
	}
	// FilePattern and CommandPattern would need custom SQL; skipping for now

	return predicates
}

// buildSessionPredicates builds ent predicates from a SessionFilter.
func buildSessionPredicates(filter *session.SessionFilter) []predicate.Session {
	var predicates []predicate.Session

	if filter.AgentName != "" {
		predicates = append(predicates, entsession.AgentNameEQ(filter.AgentName))
	}
	if filter.Since != nil {
		predicates = append(predicates, entsession.StartedAtGTE(*filter.Since))
	}
	if filter.Until != nil {
		predicates = append(predicates, entsession.StartedAtLTE(*filter.Until))
	}
	if filter.ActiveOnly {
		predicates = append(predicates, entsession.EndedAtIsNil())
	}

	return predicates
}

// Ensure SQLiteStore implements Store
var _ Store = (*SQLiteStore)(nil)

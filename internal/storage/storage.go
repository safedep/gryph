package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
	data *database
}

type database struct {
	Sessions   []Session    `json:"sessions"`
	Events     []AuditEvent `json:"events"`
	SelfAudits []SelfAudit  `json:"self_audits"`
}

type DatabaseView struct {
	Sessions   []Session
	Events     []AuditEvent
	SelfAudits []SelfAudit
}

type Session struct {
	ID               string     `json:"id"`
	AgentName        string     `json:"agent_name"`
	AgentVersion     string     `json:"agent_version"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	WorkingDirectory string     `json:"working_directory"`
	ProjectName      string     `json:"project_name"`
	TotalActions     int        `json:"total_actions"`
	FilesRead        int        `json:"files_read"`
	FilesWritten     int        `json:"files_written"`
	CommandsExecuted int        `json:"commands_executed"`
	Errors           int        `json:"errors"`
}

type AuditEvent struct {
	ID                  string          `json:"id"`
	SessionID           string          `json:"session_id"`
	Sequence            int             `json:"sequence"`
	Timestamp           time.Time       `json:"timestamp"`
	DurationMs          int64           `json:"duration_ms,omitempty"`
	AgentName           string          `json:"agent_name"`
	AgentVersion        string          `json:"agent_version"`
	WorkingDirectory    string          `json:"working_directory"`
	ActionType          string          `json:"action_type"`
	ToolName            string          `json:"tool_name"`
	ResultStatus        string          `json:"result_status"`
	ErrorMessage        string          `json:"error_message"`
	Payload             json.RawMessage `json:"payload,omitempty"`
	DiffContent         string          `json:"diff_content,omitempty"`
	RawEvent            json.RawMessage `json:"raw_event,omitempty"`
	ConversationContext string          `json:"conversation_context,omitempty"`
	IsSensitive         bool            `json:"is_sensitive"`
}

type SelfAudit struct {
	ID           string          `json:"id"`
	Timestamp    time.Time       `json:"timestamp"`
	Action       string          `json:"action"`
	AgentName    string          `json:"agent_name"`
	Details      json.RawMessage `json:"details,omitempty"`
	Result       string          `json:"result"`
	ErrorMessage string          `json:"error_message,omitempty"`
	ToolVersion  string          `json:"tool_version"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ensure data dir: %w", err)
	}
	store := &Store{path: path, data: &database{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) Data() DatabaseView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return DatabaseView{
		Sessions:   append([]Session{}, s.data.Sessions...),
		Events:     append([]AuditEvent{}, s.data.Events...),
		SelfAudits: append([]SelfAudit{}, s.data.SelfAudits...),
	}
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.save()
}

func (s *Store) UpsertSession(ctx context.Context, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.data.Sessions {
		if existing.ID == session.ID {
			s.data.Sessions[i] = session
			return s.save()
		}
	}
	s.data.Sessions = append(s.data.Sessions, session)
	return s.save()
}

func (s *Store) InsertEvent(ctx context.Context, event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Events = append(s.data.Events, event)
	return s.save()
}

func (s *Store) InsertSelfAudit(ctx context.Context, audit SelfAudit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.SelfAudits = append(s.data.SelfAudits, audit)
	return s.save()
}

func (s *Store) ListSessions(ctx context.Context, limit int, agent string) ([]Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sessions []Session
	for _, session := range s.data.Sessions {
		if agent != "" && session.AgentName != agent {
			continue
		}
		sessions = append(sessions, session)
		if limit > 0 && len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.data.Sessions {
		if session.ID == id {
			copy := session
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, limit int) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []AuditEvent
	for _, event := range s.data.Events {
		if event.SessionID != sessionID {
			continue
		}
		events = append(events, event)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return events, nil
}

func (s *Store) QueryEvents(ctx context.Context, filters QueryFilters) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []AuditEvent
	for _, event := range s.data.Events {
		if filters.AgentName != "" && event.AgentName != filters.AgentName {
			continue
		}
		if filters.SessionID != "" && event.SessionID != filters.SessionID {
			continue
		}
		if filters.ActionType != "" && event.ActionType != filters.ActionType {
			continue
		}
		if filters.ResultStatus != "" && event.ResultStatus != filters.ResultStatus {
			continue
		}
		if filters.Since != nil && event.Timestamp.Before(*filters.Since) {
			continue
		}
		if filters.Until != nil && event.Timestamp.After(*filters.Until) {
			continue
		}
		events = append(events, event)
		if filters.Limit > 0 && len(events) >= filters.Limit {
			break
		}
	}
	if filters.Offset > 0 && filters.Offset < len(events) {
		events = events[filters.Offset:]
	}
	return events, nil
}

func (s *Store) ListSelfAudits(ctx context.Context, limit int) ([]SelfAudit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var audits []SelfAudit
	for _, audit := range s.data.SelfAudits {
		audits = append(audits, audit)
		if limit > 0 && len(audits) >= limit {
			break
		}
	}
	return audits, nil
}

func (s *Store) GetDiff(ctx context.Context, eventID string) (*AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range s.data.Events {
		if event.ID == eventID {
			copy := event
			return &copy, nil
		}
	}
	return nil, nil
}

type QueryFilters struct {
	AgentName    string
	SessionID    string
	ActionType   string
	ResultStatus string
	Since        *time.Time
	Until        *time.Time
	Limit        int
	Offset       int
}

func (s *Store) load() error {
	file, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.data = &database{}
			return nil
		}
		return fmt.Errorf("open data: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&s.data); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	if s.data == nil {
		s.data = &database{}
	}
	return nil
}

func (s *Store) save() error {
	file, err := os.Create(s.path)
	if err != nil {
		return fmt.Errorf("create data: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.data); err != nil {
		return fmt.Errorf("encode data: %w", err)
	}
	return nil
}

package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/safedep/gryph/internal/config"
	"github.com/safedep/gryph/internal/storage"
)

type Service struct {
	Storage     *storage.Store
	Config      *config.Config
	ToolVersion string
}

func NewService(store *storage.Store, cfg *config.Config, version string) *Service {
	return &Service{Storage: store, Config: cfg, ToolVersion: version}
}

func (s *Service) Initialize(ctx context.Context) error {
	return s.Storage.Migrate(ctx)
}

func (s *Service) LogSelfAudit(ctx context.Context, action string, agent string, details any, result string, errMsg string) error {
	payload, _ := json.Marshal(details)
	audit := storage.SelfAudit{
		ID:           NewID(),
		Timestamp:    time.Now().UTC(),
		Action:       action,
		AgentName:    agent,
		Details:      payload,
		Result:       result,
		ErrorMessage: errMsg,
		ToolVersion:  s.ToolVersion,
	}
	return s.Storage.InsertSelfAudit(ctx, audit)
}

func (s *Service) RecordEvent(ctx context.Context, input HookEvent) error {
	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = NewID()
	}

	payload, _ := json.Marshal(input.Payload)
	rawEvent, _ := json.Marshal(input.Raw)
	session := storage.Session{
		ID:               sessionID,
		AgentName:        input.AgentName,
		AgentVersion:     input.AgentVersion,
		StartedAt:        input.Timestamp,
		WorkingDirectory: input.WorkingDirectory,
		ProjectName:      input.ProjectName,
		TotalActions:     input.Sequence,
		FilesRead:        input.FilesRead,
		FilesWritten:     input.FilesWritten,
		CommandsExecuted: input.CommandsExecuted,
		Errors:           input.Errors,
	}
	if input.SessionEnded {
		ended := input.Timestamp
		session.EndedAt = &ended
	}
	if err := s.Storage.UpsertSession(ctx, session); err != nil {
		return err
	}

	event := storage.AuditEvent{
		ID:                  input.EventID,
		SessionID:           sessionID,
		Sequence:            input.Sequence,
		Timestamp:           input.Timestamp,
		AgentName:           input.AgentName,
		AgentVersion:        input.AgentVersion,
		ActionType:          input.ActionType,
		ToolName:            input.ToolName,
		ResultStatus:        input.ResultStatus,
		ErrorMessage:        input.ErrorMessage,
		Payload:             payload,
		DiffContent:         input.DiffContent,
		RawEvent:            rawEvent,
		ConversationContext: input.ConversationContext,
		IsSensitive:         input.IsSensitive,
	}
	if input.WorkingDirectory != "" {
		event.WorkingDirectory = input.WorkingDirectory
	}
	return s.Storage.InsertEvent(ctx, event)
}

func (s *Service) HashContent(content []byte) string {
	if !s.Config.Privacy.HashFileContents {
		return ""
	}
	checksum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(checksum[:])
}

func (s *Service) IsSensitivePath(path string) bool {
	for _, pattern := range s.Config.Privacy.SensitivePaths {
		match, _ := filepath.Match(pattern, path)
		if match {
			return true
		}
	}
	return false
}

func ParseTimeFilter(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if duration, err := time.ParseDuration(value); err == nil {
		when := time.Now().Add(-duration)
		return &when, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return &parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed, nil
	}
	return nil, fmt.Errorf("invalid time format: %s", value)
}

func ResolveProjectName(workdir string) string {
	base := filepath.Base(workdir)
	if base == "." || base == "/" {
		return ""
	}
	return strings.TrimSpace(base)
}

func NewID() string {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}

// HookEvent is a normalized representation of agent hook data.
// Fields are intentionally minimal for MVP.
type HookEvent struct {
	EventID             string
	SessionID           string
	Sequence            int
	Timestamp           time.Time
	AgentName           string
	AgentVersion        string
	WorkingDirectory    string
	ProjectName         string
	ActionType          string
	ToolName            string
	ResultStatus        string
	ErrorMessage        string
	Payload             map[string]any
	Raw                 map[string]any
	DiffContent         string
	ConversationContext string
	IsSensitive         bool
	FilesRead           int
	FilesWritten        int
	CommandsExecuted    int
	Errors              int
	SessionEnded        bool
}

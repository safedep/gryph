package tui

import (
	"encoding/json"
	"time"
)

// StatusView represents the status output data.
type StatusView struct {
	Version  string
	Agents   []AgentStatusView
	Database DatabaseView
	Config   ConfigStatusView
}

// AgentStatusView represents an agent's status.
type AgentStatusView struct {
	Name        string
	DisplayName string
	Installed   bool
	Version     string
	HooksCount  int
	HooksActive bool
}

// DatabaseView represents database information.
type DatabaseView struct {
	Location     string
	SizeBytes    int64
	SizeHuman    string
	EventCount   int
	SessionCount int
	OldestEvent  time.Time
	NewestEvent  time.Time
}

// ConfigStatusView represents configuration status.
type ConfigStatusView struct {
	Location        string
	LoggingLevel    string
	RetentionDays   int
	EventsToClean   int       // Events that would be deleted by retention policy
	RetentionCutoff time.Time // The cutoff date for retention
}

// SessionView represents a session for display.
type SessionView struct {
	ID               string
	ShortID          string
	AgentName        string
	AgentDisplayName string
	AgentVersion     string
	StartedAt        time.Time
	EndedAt          time.Time
	Duration         time.Duration
	WorkingDirectory string
	ProjectName      string
	TotalActions     int
	FilesRead        int
	FilesWritten     int
	CommandsExecuted int
	Errors           int
	LinesAdded       int
	LinesRemoved     int
}

// EventView represents an event for display.
type EventView struct {
	ID               string
	ShortID          string
	SessionID        string
	ShortSessionID   string
	Sequence         int
	Timestamp        time.Time
	AgentName        string
	AgentDisplayName string
	ActionType       string
	ActionDisplay    string
	ToolName         string
	ResultStatus     string
	ErrorMessage     string
	Path             string
	Command          string
	LinesAdded       int
	LinesRemoved     int
	ExitCode         int
	DurationMs       int64
	IsSensitive      bool
	HasDiff          bool
}

// EventDetailView represents the full details of a single event.
type EventDetailView struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	AgentSessionID   string          `json:"agent_session_id,omitempty"`
	Sequence         int             `json:"sequence"`
	Timestamp        time.Time       `json:"timestamp"`
	DurationMs       int64           `json:"duration_ms,omitempty"`
	AgentName        string          `json:"agent_name"`
	AgentDisplayName string          `json:"agent_display_name"`
	AgentVersion     string          `json:"agent_version,omitempty"`
	WorkingDirectory string          `json:"working_directory,omitempty"`
	ActionType       string          `json:"action_type"`
	ActionDisplay    string          `json:"action_display"`
	ToolName         string          `json:"tool_name,omitempty"`
	ResultStatus     string          `json:"result_status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	IsSensitive      bool            `json:"is_sensitive"`
	Payload          any             `json:"payload,omitempty"`
	DiffContent      string          `json:"diff_content,omitempty"`
	RawEvent         json.RawMessage `json:"raw_event,omitempty"`
	ConvContext      string          `json:"conversation_context,omitempty"`
}

// InstallView represents installation results.
type InstallView struct {
	Agents   []AgentInstallView
	Database string
	Config   string
}

// AgentInstallView represents an agent's installation result.
type AgentInstallView struct {
	Name           string
	DisplayName    string
	Installed      bool
	Version        string
	Path           string
	HooksInstalled []string
	Warnings       []string
	Error          string
}

// UninstallView represents uninstallation results.
type UninstallView struct {
	Agents []AgentUninstallView
	Purged bool
}

// AgentUninstallView represents an agent's uninstallation result.
type AgentUninstallView struct {
	Name            string
	DisplayName     string
	HooksRemoved    []string
	BackupsRestored bool
	Error           string
}

// DoctorView represents doctor check results.
type DoctorView struct {
	Checks []DoctorCheck
	AllOK  bool
}

// DoctorCheck represents a single doctor check.
type DoctorCheck struct {
	Name        string
	Description string
	Status      CheckStatus
	Message     string
	Suggestion  string
}

// CheckStatus represents the status of a doctor check.
type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

// ConfigView represents configuration for display.
type ConfigView struct {
	Location string
	Values   map[string]interface{}
}

// SelfAuditView represents a self-audit entry for display.
type SelfAuditView struct {
	ID           string
	Timestamp    time.Time
	Action       string
	AgentName    string
	Result       string
	ErrorMessage string
	ToolVersion  string
	Details      map[string]interface{}
}

// DiffView represents a diff for display.
type DiffView struct {
	EventID   string
	SessionID string
	FilePath  string
	Timestamp time.Time
	Content   string
	Available bool
	Message   string
}

// StreamSyncView represents stream sync results for display.
type StreamSyncView struct {
	TargetResults []StreamTargetResultView
	TotalEvents   int
	TotalAudits   int
	HasErrors     bool
}

// StreamTargetResultView represents a single target's sync result.
type StreamTargetResultView struct {
	TargetName string
	EventsSent int
	AuditsSent int
	Error      string
}

// UpdateNoticeView represents an available update for display.
type UpdateNoticeView struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseURL     string `json:"release_url"`
}

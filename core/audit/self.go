// Package audit provides self-audit logging for the tool's own actions.
package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SelfAuditAction represents the type of action the tool performed.
type SelfAuditAction string

const (
	// ActionInstall indicates hooks were installed for an agent.
	ActionInstall SelfAuditAction = "install"
	// ActionUninstall indicates hooks were removed from an agent.
	ActionUninstall SelfAuditAction = "uninstall"
	// ActionConfigChange indicates configuration was modified.
	ActionConfigChange SelfAuditAction = "config_change"
	// ActionExport indicates data was exported.
	ActionExport SelfAuditAction = "export"
	// ActionPurge indicates database or config was purged.
	ActionPurge SelfAuditAction = "purge"
	// ActionUpgrade indicates the tool was upgraded.
	ActionUpgrade SelfAuditAction = "upgrade"
	// ActionDatabaseInit indicates the database was initialized.
	ActionDatabaseInit SelfAuditAction = "database_init"
	// ActionRetentionCleanup indicates old events were deleted.
	ActionRetentionCleanup SelfAuditAction = "retention_cleanup"
)

// String returns the string representation of a SelfAuditAction.
func (a SelfAuditAction) String() string {
	return string(a)
}

// SelfAuditResult represents the outcome of a self-audit action.
type SelfAuditResult string

const (
	// ResultSuccess indicates the action completed successfully.
	ResultSuccess SelfAuditResult = "success"
	// ResultError indicates the action failed with an error.
	ResultError SelfAuditResult = "error"
	// ResultSkipped indicates the action was skipped.
	ResultSkipped SelfAuditResult = "skipped"
)

// String returns the string representation of a SelfAuditResult.
func (r SelfAuditResult) String() string {
	return string(r)
}

// SelfAudit represents a log entry for the tool's own actions.
type SelfAudit struct {
	// ID is the unique identifier for this audit entry.
	ID uuid.UUID `json:"id"`
	// Timestamp is when the action occurred (UTC).
	Timestamp time.Time `json:"timestamp"`
	// Action is the type of action performed.
	Action SelfAuditAction `json:"action"`
	// AgentName is the relevant agent, if applicable.
	AgentName string `json:"agent_name,omitempty"`
	// Details contains action-specific data.
	Details json.RawMessage `json:"details,omitempty"`
	// Result is the outcome of the action.
	Result SelfAuditResult `json:"result"`
	// ErrorMessage contains error details if failed.
	ErrorMessage string `json:"error_message,omitempty"`
	// ToolVersion is the version of the tool that performed the action.
	ToolVersion string `json:"tool_version"`
}

// NewSelfAudit creates a new SelfAudit entry with a generated UUID.
func NewSelfAudit(action SelfAuditAction, toolVersion string) *SelfAudit {
	return &SelfAudit{
		ID:          uuid.New(),
		Timestamp:   time.Now().UTC(),
		Action:      action,
		Result:      ResultSuccess,
		ToolVersion: toolVersion,
	}
}

// WithAgent sets the AgentName.
func (s *SelfAudit) WithAgent(agent string) *SelfAudit {
	s.AgentName = agent
	return s
}

// WithDetails sets the Details from a given struct.
func (s *SelfAudit) WithDetails(details interface{}) *SelfAudit {
	data, _ := json.Marshal(details)
	s.Details = data
	return s
}

// WithError marks the audit as failed with the given error.
func (s *SelfAudit) WithError(err error) *SelfAudit {
	s.Result = ResultError
	s.ErrorMessage = err.Error()
	return s
}

// MarkSkipped marks the audit as skipped.
func (s *SelfAudit) MarkSkipped() *SelfAudit {
	s.Result = ResultSkipped
	return s
}

// InstallDetails contains details for install actions.
type InstallDetails struct {
	HooksInstalled []string `json:"hooks_installed"`
	BackupPath     string   `json:"backup_path,omitempty"`
}

// UninstallDetails contains details for uninstall actions.
type UninstallDetails struct {
	HooksRemoved    []string `json:"hooks_removed"`
	BackupRestored  bool     `json:"backup_restored"`
}

// ConfigChangeDetails contains details for config change actions.
type ConfigChangeDetails struct {
	Key      string `json:"key"`
	OldValue string `json:"old_value,omitempty"`
	NewValue string `json:"new_value"`
}

// ExportDetails contains details for export actions.
type ExportDetails struct {
	Format     string `json:"format"`
	EventCount int    `json:"event_count"`
	OutputPath string `json:"output_path,omitempty"`
}

// RetentionCleanupDetails contains details for retention cleanup actions.
type RetentionCleanupDetails struct {
	EventsDeleted    int       `json:"events_deleted"`
	OldestRemaining  time.Time `json:"oldest_remaining,omitempty"`
}

// DatabaseInitDetails contains details for database initialization.
type DatabaseInitDetails struct {
	Path          string `json:"path"`
	SchemaVersion string `json:"schema_version"`
}

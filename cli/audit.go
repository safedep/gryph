package cli

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/storage"
)

// SelfAuditAction constants for self-audit logging.
const (
	SelfAuditActionInstall        = "install"
	SelfAuditActionUninstall      = "uninstall"
	SelfAuditActionConfigChange   = "config_change"
	SelfAuditActionPurge          = "purge"
	SelfAuditActionDatabaseInit   = "database_init"
	SelfAuditResultSuccess        = "success"
	SelfAuditResultError          = "error"
	SelfAuditResultSkipped        = "skipped"
)

// logSelfAudit logs a self-audit entry.
func logSelfAudit(ctx context.Context, store storage.Store, action string, agentName string, details map[string]interface{}, result string, errorMsg string) error {
	if store == nil {
		return nil
	}

	entry := &storage.SelfAuditEntry{
		ID:           uuid.New(),
		Timestamp:    time.Now().UTC(),
		Action:       action,
		AgentName:    agentName,
		Details:      details,
		Result:       result,
		ErrorMessage: errorMsg,
		ToolVersion:  getVersion(),
	}

	return store.SaveSelfAudit(ctx, entry)
}

// getVersion returns the tool version, with a fallback for dev builds.
func getVersion() string {
	if version.Version != "" && version.Version != "(devel)" {
		return version.Version
	}
	return "dev"
}

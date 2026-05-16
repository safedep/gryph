package cli

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/approval"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/storage"
)

// SelfAuditAction constants for self-audit logging.
//
// IMPORTANT: these values MUST stay in sync with the `action` field enum on
// storage/ent/schema/selfaudit.go. Ent validates writes against that enum and
// rejects unknown values at runtime, so adding a constant here without also
// adding it to the schema (and running `make generate`) will fail saves
// silently in production and only surface under careful testing.
const (
	SelfAuditActionInstall                 = "install"
	SelfAuditActionUninstall               = "uninstall"
	SelfAuditActionConfigChange            = "config_change"
	SelfAuditActionPurge                   = "purge"
	SelfAuditActionDatabaseInit            = "database_init"
	SelfAuditActionRetentionCleanup        = "retention_cleanup"
	SelfAuditActionHookError               = "hook_error"
	SelfAuditActionPolicyLoadError         = "policy_load_error"
	SelfAuditActionContextCleanup          = "context_cleanup"
	SelfAuditActionContextSnapshotError    = "context_snapshot_error"
	SelfAuditActionReceiptCleanup          = "receipt_cleanup"
	SelfAuditActionReceiptInsertError      = "receipt_insert_error"
	SelfAuditActionReceiptChainBroken      = "receipt_chain_broken"
	SelfAuditActionReceiptSigned           = "receipt_signed"
	SelfAuditActionReceiptSignatureInvalid = "receipt_signature_invalid"
	SelfAuditActionReceiptKeyRotated       = "receipt_key_rotated"
	SelfAuditActionApprovalRequested       = approval.AuditActionRequested
	SelfAuditActionApprovalGranted         = approval.AuditActionGranted
	SelfAuditActionApprovalDenied          = approval.AuditActionDenied
	SelfAuditActionApprovalTimeout         = approval.AuditActionTimeout
	SelfAuditResultSuccess                 = "success"
	SelfAuditResultError                   = "error"
	SelfAuditResultSkipped                 = "skipped"
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

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SelfAudit holds the schema definition for the SelfAudit entity.
type SelfAudit struct {
	ent.Schema
}

// Fields of the SelfAudit.
func (SelfAudit) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Time("timestamp").
			Default(time.Now).
			Immutable(),
		// Keep these values in sync with the SelfAuditAction* constants in
		// cli/audit.go. Ent validates writes against this enum at runtime.
		field.Enum("action").
			Values("install", "uninstall", "config_change", "export", "purge", "upgrade", "database_init", "retention_cleanup", "hook_error", "policy_load_error", "context_cleanup", "context_snapshot_error", "receipt_cleanup", "receipt_insert_error", "receipt_chain_broken", "approval_requested", "approval_granted", "approval_denied", "approval_timeout"),
		field.String("agent_name").
			Optional(),
		field.JSON("details", map[string]interface{}{}).
			Optional(),
		field.Enum("result").
			Values("success", "error", "skipped").
			Default("success"),
		field.String("error_message").
			Optional(),
		field.String("tool_version").
			NotEmpty(),
	}
}

// Edges of the SelfAudit.
func (SelfAudit) Edges() []ent.Edge {
	return nil
}

// Indexes of the SelfAudit.
func (SelfAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("timestamp"),
		index.Fields("action"),
	}
}

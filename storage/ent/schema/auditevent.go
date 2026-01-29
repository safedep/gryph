package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AuditEvent holds the schema definition for the AuditEvent entity.
type AuditEvent struct {
	ent.Schema
}

// Fields of the AuditEvent.
func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.Int("sequence").
			Positive(),
		field.Time("timestamp").
			Default(time.Now).
			Immutable(),
		field.Int64("duration_ms").
			Optional().
			Nillable(),
		field.String("agent_name").
			NotEmpty(),
		field.String("agent_version").
			Optional(),
		field.String("working_directory").
			Optional(),
		field.Enum("action_type").
			Values("file_read", "file_write", "file_delete", "command_exec", "network_request", "tool_use", "session_start", "session_end", "notification", "unknown"),
		field.String("tool_name").
			Optional(),
		field.Enum("result_status").
			Values("success", "error", "blocked", "rejected").
			Default("success"),
		field.String("error_message").
			Optional(),
		field.JSON("payload", map[string]interface{}{}).
			Optional(),
		field.Text("diff_content").
			Optional().
			SchemaType(map[string]string{
				dialect.SQLite: "text",
			}),
		field.JSON("raw_event", map[string]interface{}{}).
			Optional(),
		field.Text("conversation_context").
			Optional().
			SchemaType(map[string]string{
				dialect.SQLite: "text",
			}),
		field.Bool("is_sensitive").
			Default(false),
	}
}

// Edges of the AuditEvent.
func (AuditEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("session", Session.Type).
			Ref("events").
			Field("session_id").
			Required().
			Unique(),
	}
}

// Indexes of the AuditEvent.
func (AuditEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("timestamp"),
		index.Fields("session_id"),
		index.Fields("agent_name"),
		index.Fields("action_type"),
		index.Fields("result_status"),
	}
}

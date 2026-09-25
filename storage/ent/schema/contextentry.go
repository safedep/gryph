package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ContextEntry is one entry of a session context: an intent, an action, or
// an observation. It holds facts only. The path, the command, and the
// content stay on audit_events.
type ContextEntry struct {
	ent.Schema
}

func (ContextEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("event_id", uuid.UUID{}).
			Optional(),
		field.Int64("sequence").Positive(),
		field.String("kind"),
		field.Time("timestamp").
			Default(time.Now).
			Immutable(),

		field.String("action_type"),
		field.String("tool").Optional(),
		field.String("tool_call_id").Optional(),
		field.UUID("linked_event_id", uuid.UUID{}).Optional().Nillable(),
		field.String("phase").Optional(),

		field.String("target_host").Optional(),
		field.String("target_mcp_server").Optional(),
		field.String("target_mcp_tool").Optional(),
		field.String("origin").Optional(),
		field.JSON("tags", []string{}).Optional(),
		field.JSON("classifications", []string{}).Optional(),
		field.Float32("injection_score").Optional().Nillable(),

		field.String("decision").Optional(),
		field.JSON("matched_rule_ids", []string{}).Optional(),
		field.String("content_digest").Optional(),

		field.String("result_status").Default("pending"),
		field.Int64("duration_ms").Optional().Nillable(),
		field.String("error_message").Optional(),

		field.Int("hash_version"),
		field.Bytes("prev_hash").Optional().MaxLen(32),
		field.Bytes("hash").MaxLen(32),
	}
}

func (ContextEntry) Edges() []ent.Edge {
	return nil
}

func (ContextEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "sequence").Unique(),
		index.Fields("session_id", "tool_call_id"),
		index.Fields("timestamp"),
	}
}

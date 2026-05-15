package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmContextAction holds the schema definition for a single mediated action
// recorded by the AARM Context Accumulator. Rows are append-only and indexed
// by session for fast per-session reads. Action types align with
// model.ActionType in aarm/model/model.go.
type AarmContextAction struct {
	ent.Schema
}

// Fields of the AarmContextAction.
func (AarmContextAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("event_id", uuid.UUID{}).
			Optional(),

		field.Time("timestamp").
			Default(time.Now).
			Immutable(),

		field.Enum("action_type").
			Values("file_read", "file_write", "file_delete", "command_exec",
				"network_request", "tool_use", "session_start", "session_end",
				"notification", "subagent_start", "subagent_stop", "unknown"),
		field.String("tool").Optional(),
		field.String("agent").Optional(),
		field.String("project").Optional(),
		field.String("working_dir").Optional(),

		field.Enum("result_status").
			Values("success", "error", "blocked", "rejected", "pending").
			Default("pending"),
		field.Int64("duration_ms").Optional().Nillable(),
		field.String("error_message").Optional(),

		field.JSON("data_classifications", []string{}).Optional(),
		field.Float32("injection_score").Optional().Nillable(),
	}
}

// Edges of the AarmContextAction.
func (AarmContextAction) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmContextAction.
func (AarmContextAction) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "timestamp"),
		index.Fields("timestamp"),
		index.Fields("session_id", "action_type"),
	}
}

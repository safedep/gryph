package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Session holds the schema definition for the Session entity.
type Session struct {
	ent.Schema
}

// Fields of the Session.
func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("agent_session_id").
			Optional().
			Comment("Original session ID from the agent for correlation"),
		field.String("agent_name").
			NotEmpty(),
		field.String("agent_version").
			Optional(),
		field.Time("started_at").
			Default(time.Now).
			Immutable(),
		field.Time("ended_at").
			Optional().
			Nillable(),
		field.String("working_directory").
			Optional(),
		field.String("project_name").
			Optional(),
		field.Int("total_actions").
			Default(0),
		field.Int("files_read").
			Default(0),
		field.Int("files_written").
			Default(0),
		field.Int("commands_executed").
			Default(0),
		field.Int("errors").
			Default(0),
	}
}

// Edges of the Session.
func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("events", AuditEvent.Type),
	}
}

// Indexes of the Session.
func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("started_at"),
		index.Fields("agent_name"),
		index.Fields("agent_name", "started_at"),
	}
}

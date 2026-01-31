package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// StreamCheckpoint holds the schema definition for the StreamCheckpoint entity.
type StreamCheckpoint struct {
	ent.Schema
}

// Fields of the StreamCheckpoint.
func (StreamCheckpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			Comment("target_name used as primary key"),
		field.Time("last_synced_at").
			Default(time.Now),
		field.String("last_event_id").
			Optional(),
		field.String("last_self_audit_id").
			Optional(),
	}
}

// Edges of the StreamCheckpoint.
func (StreamCheckpoint) Edges() []ent.Edge {
	return nil
}

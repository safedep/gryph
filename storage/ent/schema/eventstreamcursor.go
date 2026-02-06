package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// EventStreamCursor holds the schema definition for the EventStreamCursor entity.
type EventStreamCursor struct {
	ent.Schema
}

// Fields of the EventStreamCursor.
func (EventStreamCursor) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			Comment("target_name used as primary key"),
		field.Time("last_synced_at").
			Default(time.Now),
		field.String("last_id").
			Optional(),
	}
}

// Edges of the EventStreamCursor.
func (EventStreamCursor) Edges() []ent.Edge {
	return nil
}

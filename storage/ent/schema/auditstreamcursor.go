package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AuditStreamCursor holds the schema definition for the AuditStreamCursor entity.
type AuditStreamCursor struct {
	ent.Schema
}

// Fields of the AuditStreamCursor.
func (AuditStreamCursor) Fields() []ent.Field {
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

// Edges of the AuditStreamCursor.
func (AuditStreamCursor) Edges() []ent.Edge {
	return nil
}

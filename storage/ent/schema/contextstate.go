package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ContextState holds the per-session facts that are costly to compute from
// the entries. The activity counters live on sessions.
type ContextState struct {
	ent.Schema
}

func (ContextState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}).
			Immutable().
			Unique(),
		field.Time("last_entry_at").
			Default(time.Now),

		field.JSON("tools_used", []string{}).Optional(),
		field.JSON("classifications_seen", []string{}).Optional(),
		field.JSON("tags_seen", map[string]int64{}).Optional(),
		field.JSON("origins_seen", []string{}).Optional(),
		field.JSON("entities_seen", []string{}).Optional(),
		field.JSON("egress_hosts", []string{}).Optional(),
		field.Int64("last_intent_seq").Optional().Nillable(),
		field.Time("last_intent_at").Optional().Nillable(),
	}
}

func (ContextState) Edges() []ent.Edge {
	return nil
}

func (ContextState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("last_entry_at"),
	}
}

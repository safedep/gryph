package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmContextState holds the schema definition for the denormalized per-session
// counters consulted by the PDP via context.* CEL variables. One row per
// session_id. Counter updates happen via SQLite UPSERT so AppendContextAction
// is race-free across concurrent same-session writes.
type AarmContextState struct {
	ent.Schema
}

// Fields of the AarmContextState.
func (AarmContextState) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("session_id", uuid.UUID{}).
			Immutable().
			Unique(),
		field.Time("first_seen_at").
			Default(time.Now).
			Immutable(),
		field.Time("last_action_at").
			Default(time.Now),

		field.Int("total_actions").Default(0),
		field.Int("files_read").Default(0),
		field.Int("files_written").Default(0),
		field.Int("commands_executed").Default(0),
		field.Int("network_requests").Default(0),
		field.Int("errors").Default(0),

		field.JSON("tools_used", []string{}).Optional(),
		field.JSON("classifications_seen", []string{}).Optional(),
		field.JSON("entities_seen", []string{}).Optional(),
		field.Float("semantic_drift").Default(0),
	}
}

// Edges of the AarmContextState.
func (AarmContextState) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmContextState.
func (AarmContextState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("last_action_at"),
	}
}

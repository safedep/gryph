package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmDeferredAction holds the schema definition for the persistent
// pending-deferral queue. Each row references the originating receipt by
// (session_id, receipt_sequence). Status transitions are durable so an
// operator can resolve a deferral out-of-band or the periodic sweep can
// flip it to resolved_timeout.
type AarmDeferredAction struct {
	ent.Schema
}

// Fields of the AarmDeferredAction.
func (AarmDeferredAction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.Int64("receipt_sequence").Positive(),
		field.UUID("action_id", uuid.UUID{}),

		field.Time("deferred_at").
			Default(time.Now).
			Immutable(),
		field.Time("expires_at"),
		field.String("reason"),

		field.Enum("status").
			Values("pending", "resolved_allow", "resolved_deny", "resolved_timeout").
			Default("pending"),

		field.Time("resolved_at").Optional().Nillable(),
		field.String("resolver").Optional(),
		field.String("resolution_note").Optional(),
	}
}

// Edges of the AarmDeferredAction.
func (AarmDeferredAction) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmDeferredAction.
func (AarmDeferredAction) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "deferred_at"),
		index.Fields("status"),
		index.Fields("expires_at"),
		index.Fields("session_id", "receipt_sequence").Unique(),
	}
}

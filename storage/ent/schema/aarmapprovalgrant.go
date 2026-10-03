package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmApprovalGrant is a stored approval that a retry of the same action
// can use. It binds to the account's partition, the action digest, the
// agent session and the scope. Nothing wider than the digest ever
// matches.
type AarmApprovalGrant struct {
	ent.Schema
}

// Fields of the AarmApprovalGrant.
func (AarmApprovalGrant) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("request_id", uuid.UUID{}),
		field.UUID("session_id", uuid.UUID{}),
		field.String("action_digest"),
		field.Enum("scope").
			Values("once", "session", "window").
			Default("once"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("expires_at"),
		field.Time("used_at").Optional().Nillable(),
		field.Int("uses").Default(0),
		field.String("approver").Optional(),
		field.String("assurance").Optional(),
		field.String("channel").Optional(),
	}
}

// Edges of the AarmApprovalGrant.
func (AarmApprovalGrant) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmApprovalGrant.
func (AarmApprovalGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "action_digest"),
		index.Fields("action_digest", "expires_at"),
	}
}

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmApprovalRequest is one escalated action that waits for, or got, an
// approval. The decision service keeps the row, decides it once, and
// records the answer with its channel and assurance. The row references
// the request receipt by (session_id, receipt_sequence).
type AarmApprovalRequest struct {
	ent.Schema
}

// Fields of the AarmApprovalRequest.
func (AarmApprovalRequest) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("action_id", uuid.UUID{}),
		field.Int64("receipt_sequence").Positive(),

		field.String("action_digest"),
		field.JSON("rule_ids", []string{}).Optional(),
		field.String("requester").Optional(),
		field.String("host").Optional(),
		field.String("agent").Optional(),
		field.String("summary").Optional(),
		field.String("project").Optional(),
		field.String("min_assurance").Optional(),

		field.Enum("state").
			Values("pending", "approved", "denied", "expired").
			Default("pending"),
		field.Time("requested_at").
			Default(time.Now).
			Immutable(),
		field.Time("expires_at"),
		// inline marks a request that offered the prompt on the hook's own
		// connection.
		field.Bool("inline").Default(false),
		// review marks a request from a hook that ran after the action. An
		// answer records a review and stores no grant.
		field.Bool("review").Default(false),
		// requester_audit is the login identity of the process that asked,
		// for the comparison with the one that answers.
		field.String("requester_audit").Optional(),
		// requester_trust is the trust of the connection that asked.
		field.String("requester_trust").Optional(),

		field.Time("decided_at").Optional().Nillable(),
		field.String("channel").Optional(),
		field.String("assurance").Optional(),
		field.String("approver").Optional(),
		field.String("peer_trust").Optional(),
		field.String("note").Optional(),
		field.String("scope").Optional(),
		// notified_at is when a later hook of the session told the agent
		// the outcome.
		field.Time("notified_at").Optional().Nillable(),
	}
}

// Edges of the AarmApprovalRequest.
func (AarmApprovalRequest) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmApprovalRequest.
func (AarmApprovalRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "requested_at"),
		index.Fields("state", "expires_at"),
		index.Fields("session_id", "receipt_sequence").Unique(),
	}
}

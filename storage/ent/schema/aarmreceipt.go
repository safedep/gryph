package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// AarmReceipt holds the schema definition for a single append-only,
// hash-chained record of a mediated action. Rows outlive their originating
// session and audit event. Per-session ordering is enforced by the unique
// (session_id, sequence) index, which doubles as a crash-recovery rail.
type AarmReceipt struct {
	ent.Schema
}

// Fields of the AarmReceipt.
func (AarmReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("session_id", uuid.UUID{}),
		field.UUID("action_id", uuid.UUID{}).Optional(),
		field.UUID("event_id", uuid.UUID{}).Optional(),

		field.Time("recorded_at").
			Default(time.Now).
			Immutable(),
		field.Int64("sequence").Positive(),

		field.String("agent").Optional(),
		field.String("tool").Optional(),
		field.String("action_type"),
		field.String("project").Optional(),

		field.String("decision"),
		field.JSON("matched_rule_ids", []string{}).Optional(),
		field.String("severity").Optional(),
		field.String("message").Optional(),

		field.Enum("result_status").
			Values("success", "error", "blocked", "rejected", "pending").
			Default("pending"),
		field.Int64("duration_ms").Optional().Nillable(),
		field.String("error_message").Optional(),

		field.JSON("snapshot", map[string]interface{}{}).Optional(),
		field.JSON("action_payload", map[string]interface{}{}).Optional(),

		field.Bytes("prev_hash").Optional().MaxLen(32),
		field.Bytes("hash").MaxLen(32),

		field.Bytes("signature").Optional(),
		field.String("signer_key_id").Optional(),
	}
}

// Edges of the AarmReceipt.
func (AarmReceipt) Edges() []ent.Edge {
	return nil
}

// Indexes of the AarmReceipt.
func (AarmReceipt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "sequence").Unique(),
		index.Fields("session_id", "recorded_at"),
		index.Fields("recorded_at"),
		index.Fields("decision"),
		index.Fields("action_id"),
	}
}

// Package mediation implements the AARM Action Mediation Layer (AML)
// for Gryph. Adapters normalise incoming protocol-specific events into
// the canonical aarm.Action representation consumed by the rest of the
// AARM pipeline.
package mediation

import (
	"context"

	"github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// Adapter normalises a protocol-specific event into a canonical Action.
// Gryph currently has one adapter (HookAdapter, for agent hooks); future
// adapters (MCP proxy, HTTP proxy) will implement the same interface.
type Adapter interface {
	Normalize(ctx context.Context, event *events.Event, sess *session.Session) (*aarm.Action, error)
}

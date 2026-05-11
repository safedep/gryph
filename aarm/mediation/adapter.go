// Package mediation normalizes adapter-specific events into aarm.Action.
package mediation

import (
	"context"

	"github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// Adapter normalizes an event into a canonical action.
type Adapter interface {
	Normalize(ctx context.Context, event *events.Event, sess *session.Session) (*aarm.Action, error)
}

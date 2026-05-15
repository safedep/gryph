// Package mediation normalizes adapter-specific events into model.Action.
package mediation

import (
	"context"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// Classifier tags an action with high-level data classification labels.
// Implementations live in aarm/classify. The Adapter accepts it via
// WithClassifier so the mediation layer can populate
// Action.DataClassifications without taking a direct dependency on the
// classify package (avoids an import cycle with aarm/check.go which holds
// the Mediator).
type Classifier interface {
	Classify(action *model.Action) []string
}

// InjectionScorer assigns a 0..1 injection-likelihood score to a tool-use
// action. Implementations live in aarm/injectscore.
type InjectionScorer interface {
	Score(action *model.Action) float32
}

// Adapter normalizes an event into a canonical action.
type Adapter interface {
	Normalize(ctx context.Context, event *events.Event, sess *session.Session) (*model.Action, error)
}

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

// populateWellKnownParams promotes a handful of well-known argument keys
// (url, file_path / path, command) onto typed fields on Parameters when the
// caller has not already set them. Both the hook adapter and the MCP adapter
// use this to keep the canonical parameter shape consistent.
func populateWellKnownParams(p *model.Parameters, args map[string]any) {
	if p == nil || len(args) == 0 {
		return
	}
	if p.URL == "" {
		if v, ok := args["url"].(string); ok && v != "" {
			p.URL = v
		}
	}
	if p.Path == "" {
		if v, ok := args["file_path"].(string); ok && v != "" {
			p.Path = v
		} else if v, ok := args["path"].(string); ok && v != "" {
			p.Path = v
		}
	}
	if p.Command == "" {
		if v, ok := args["command"].(string); ok && v != "" {
			p.Command = v
		}
	}
}

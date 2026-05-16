// Package mediation normalizes adapter-specific events into model.Action.
package mediation

import (
	"context"

	"github.com/safedep/gryph/aarm/identity"
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

// Common collects the optional dependencies every adapter needs to enrich a
// normalized action: a Classifier for data classifications, an InjectionScorer
// for tool-use risk scoring, and an identity Capturer for the principal /
// service identity / role scope fields. HookAdapter and MCPAdapter embed
// Common so option helpers and the runtime call sites that use them stay in
// one place.
type Common struct {
	Classifier      Classifier
	Scorer          InjectionScorer
	IdentityCapture identity.Capturer
}

// CommonOption mutates a Common. Each option is a no-op when its argument is
// nil so callers can wire options unconditionally.
type CommonOption func(*Common)

// WithClassifier wires a Classifier into the adapter. Nil is ignored.
func WithClassifier(c Classifier) CommonOption {
	return func(o *Common) {
		if c != nil {
			o.Classifier = c
		}
	}
}

// WithInjectionScorer wires an InjectionScorer into the adapter. Nil is
// ignored. The score is populated only when Action.Type == ActionToolUse.
func WithInjectionScorer(s InjectionScorer) CommonOption {
	return func(o *Common) {
		if s != nil {
			o.Scorer = s
		}
	}
}

// WithIdentityCapturer wires an identity.Capturer into the adapter. The three
// identity fields (HumanPrincipal, ServiceIdentity, RoleScope) populate every
// normalized Action. Nil is ignored.
func WithIdentityCapturer(c identity.Capturer) CommonOption {
	return func(o *Common) {
		if c != nil {
			o.IdentityCapture = c
		}
	}
}

// applyEnrichment populates the classification, injection score, and identity
// fields on a freshly normalized Action from the Common's wired collaborators.
// Adapters call this from Normalize after they have populated the
// adapter-specific fields. metaOverrides is consulted for the three identity
// fields when non-nil, mirroring the MCP adapter's per-event overrides; pass
// nil to skip.
func (c *Common) applyEnrichment(ctx context.Context, action *model.Action, metaOverrides map[string]any) {
	if c == nil || action == nil {
		return
	}
	if c.Classifier != nil {
		action.DataClassifications = c.Classifier.Classify(action)
	}
	if c.Scorer != nil && action.Type == model.ActionToolUse {
		action.InjectionScore = c.Scorer.Score(action)
	}
	if c.IdentityCapture != nil {
		ident := c.IdentityCapture.Capture(ctx)
		action.HumanPrincipal = ident.HumanPrincipal
		action.ServiceIdentity = ident.ServiceIdentity
		action.RoleScope = ident.RoleScope
	}
	if len(metaOverrides) == 0 {
		return
	}
	if v, ok := metaOverrides["human_principal"].(string); ok && v != "" {
		action.HumanPrincipal = v
	}
	if v, ok := metaOverrides["service_identity"].(string); ok && v != "" {
		action.ServiceIdentity = v
	}
	if v, ok := metaOverrides["role_scope"].(string); ok && v != "" {
		action.RoleScope = v
	}
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

// Package injectscore computes a heuristic prompt-injection score for
// tool-use actions. The score is intentionally a lower-bound: false positives
// are acceptable, false negatives are expected. It exists so policies can
// gate on "looks injection-ish" without us pretending we have a reliable
// detector.
package injectscore

import (
	"strings"

	"github.com/safedep/gryph/aarm/model"
)

const (
	// PerMatchWeight is the score contribution of a single indicator match.
	PerMatchWeight float32 = 0.15
	// MaxScore is the cap returned by Heuristic.Score.
	MaxScore float32 = 1.0
)

// Scorer returns an injection-likelihood score in the range [0.0, 1.0] for an
// action. Implementations should return 0 for non-tool-use actions and for
// empty content.
type Scorer interface {
	Score(action *model.Action) float32
}

// Heuristic counts substring matches against a fixed indicator list. Each
// match contributes PerMatchWeight, capped at MaxScore.
type Heuristic struct {
	indicators []string
}

// NewHeuristic returns the default Heuristic with the built-in indicator set.
func NewHeuristic() *Heuristic {
	return &Heuristic{
		indicators: []string{
			"ignore previous instructions",
			"disregard previous",
			"you are now",
			"system prompt",
			"act as",
			"prompt injection",
		},
	}
}

// Score implements Scorer.
func (h *Heuristic) Score(action *model.Action) float32 {
	if h == nil || action == nil {
		return 0
	}
	if action.Type != model.ActionToolUse {
		return 0
	}

	content := strings.ToLower(action.Parameters.Content)
	if action.Parameters.Raw != nil {
		if v, ok := action.Parameters.Raw["text"].(string); ok && v != "" {
			content = content + "\n" + strings.ToLower(v)
		}
		if v, ok := action.Parameters.Raw["prompt"].(string); ok && v != "" {
			content = content + "\n" + strings.ToLower(v)
		}
	}
	if content == "" {
		return 0
	}

	var score float32
	for _, ind := range h.indicators {
		count := strings.Count(content, ind)
		if count == 0 {
			continue
		}
		score += PerMatchWeight * float32(count)
		if score >= MaxScore {
			return MaxScore
		}
	}
	return score
}

// Nop is a Scorer that always returns 0. Used when scoring is disabled in
// config.
type Nop struct{}

// NewNop returns a Nop scorer.
func NewNop() *Nop { return &Nop{} }

// Score implements Scorer.
func (*Nop) Score(*model.Action) float32 { return 0 }

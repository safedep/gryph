// Package injectscore computes a heuristic prompt-injection score for
// tool calls, observations and intents. The score is intentionally a lower-bound: false positives
// are acceptable, false negatives are expected. It exists so policies can
// gate on "looks injection-ish" without us pretending we have a reliable
// detector.
package injectscore

import (
	"cmp"
	"regexp"
	"strings"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
)

const (
	// PerMatchWeight is the score contribution of a single indicator match.
	PerMatchWeight float32 = 0.15
	// MaxScore is the cap returned by Heuristic.Score.
	MaxScore float32 = 1.0
	// MaxHitsPerIndicator caps the matches that one indicator adds. Four
	// hits of one phrase pass the documented threshold of 0.5, and only a
	// second phrase takes the score higher. So a long file that names one
	// indicator often does not reach MaxScore.
	MaxHitsPerIndicator = 4
)

// Scorer returns an injection-likelihood score in the range [0.0, 1.0] for an
// action. Implementations should return 0 for actions that are not a tool
// call, an observation or an intent, and for empty content.
type Scorer interface {
	Score(action *model.Action) float32
}

// Heuristic matches a fixed indicator list. A phrase starts and ends at a
// word boundary, "_" and white space separate its words, and its last word
// can take a common suffix (s, es, d, ed, ly). Each match contributes
// PerMatchWeight, up to MaxHitsPerIndicator for one indicator, capped at
// MaxScore.
type Heuristic struct {
	indicators []*regexp.Regexp
}

// NewHeuristic returns the default Heuristic with the built-in indicator set.
func NewHeuristic() *Heuristic {
	phrases := []string{
		"ignore previous instructions",
		"disregard previous",
		"you are now",
		"system prompt",
		"act as",
		"prompt injection",
	}
	h := &Heuristic{}
	for _, p := range phrases {
		words := strings.Fields(p)
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		pattern := `\b` + strings.Join(words, `\s+`) + `(?:s|es|d|ed|ly)?\b`
		h.indicators = append(h.indicators, regexp.MustCompile(pattern))
	}
	return h
}

// Score implements Scorer.
func (h *Heuristic) Score(action *model.Action) float32 {
	if h == nil || action == nil {
		return 0
	}
	if !scored(action) {
		return 0
	}

	// ContentFull holds the whole content when the adapter has it. Content
	// is then a preview of it, so the scorer reads one of the two.
	content := strings.ToLower(cmp.Or(action.Parameters.ContentFull, action.Parameters.Content))
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
	content = strings.ReplaceAll(content, "_", " ")

	var score float32
	for _, ind := range h.indicators {
		hits := len(ind.FindAllStringIndex(content, MaxHitsPerIndicator))
		score += PerMatchWeight * float32(hits)
		if score >= MaxScore {
			return MaxScore
		}
	}
	return score
}

// scored reports whether the scorer reads the action. It reads a tool call,
// the content that the agent received, and the text of an intent. A post
// event with no linked pre event has the kind action, but it still holds
// what the agent received, so the phase post also counts.
func scored(action *model.Action) bool {
	return action.Type == model.ActionToolUse ||
		action.Kind == events.KindObservation ||
		action.Kind == events.KindIntent ||
		action.Phase == model.PhasePost
}

// Nop is a Scorer that always returns 0. Used when scoring is disabled in
// config.
type Nop struct{}

// NewNop returns a Nop scorer.
func NewNop() *Nop { return &Nop{} }

// Score implements Scorer.
func (*Nop) Score(*model.Action) float32 { return 0 }

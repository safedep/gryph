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
	"unicode"

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
// word boundary. White space, "-" and "_" separate its words, and one filler
// word, such as "all" or "the", can come between two words. The first word
// can take s, ed or ing. The last word can take a common suffix (s, es, d,
// ed, ly). Each match contributes PerMatchWeight, up to MaxHitsPerIndicator
// for one indicator, capped at MaxScore.
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
		words[0] = inflectFirst(words[0])
		pattern := `\b` + strings.Join(words, ` (?:`+fillerWords+` )?`) + `(?:s|es|d|ed|ly)?\b`
		h.indicators = append(h.indicators, regexp.MustCompile(pattern))
	}
	return h
}

const fillerWords = `(?:all|the|any|your|my|prior|above)`

func inflectFirst(word string) string {
	if stem, ok := strings.CutSuffix(word, "e"); ok {
		return stem + `(?:e|es|ed|ing)`
	}
	return word + `(?:s|ed|ing)?`
}

// normalize folds the content so that one ASCII space separates words. A
// zero-width joiner or a byte order mark inside a word can hide a phrase, so
// normalize removes it. A zero-width space separates words, as a space does.
func normalize(content string) string {
	content = strings.Map(func(r rune) rune {
		switch {
		case r == '\u200c', r == '\u200d', r == '\u2060', r == '\ufeff':
			return -1
		case r == '-', r == '_', r == '\u200b', unicode.IsSpace(r):
			return ' '
		}
		return r
	}, strings.ToLower(content))
	return strings.Join(strings.Fields(content), " ")
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
	content := cmp.Or(action.Parameters.ContentFull, action.Parameters.Content)
	if action.Parameters.Raw != nil {
		if v, ok := action.Parameters.Raw["text"].(string); ok && v != "" {
			content = content + "\n" + v
		}
		if v, ok := action.Parameters.Raw["prompt"].(string); ok && v != "" {
			content = content + "\n" + v
		}
	}
	if content == "" {
		return 0
	}
	content = normalize(content)

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

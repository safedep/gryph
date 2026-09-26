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
	// MaxHitsPerIndicator caps the matches that one strong indicator adds.
	// Four hits of one strong phrase pass the documented threshold of 0.5,
	// and only a second phrase takes the score higher. So a long file that
	// names one indicator often does not reach MaxScore.
	MaxHitsPerIndicator = 4
	// MaxHitsPerWeakIndicator caps the matches that one weak indicator adds.
	// Normal text, such as "acts as" in a README, often holds a weak phrase.
	// Two hits stay below the threshold, so only a mix of phrases passes it.
	MaxHitsPerWeakIndicator = 2
)

// Scorer returns an injection-likelihood score in the range [0.0, 1.0] for an
// action. Implementations should return 0 for actions that are not a tool
// call, an observation or an intent, and for empty content.
type Scorer interface {
	Score(action *model.Action) float32
}

// Heuristic matches a fixed indicator list. A phrase starts and ends at a
// word boundary. White space, a dash, "*", "`" and "~" separate its words.
// "_" also separates the words of a strong phrase. It does not separate the
// words of a weak phrase, so an identifier such as system_prompt does not
// count. One filler word, such as "all" or "the", can come between two
// words. The first word can take s, ed or ing. The last word can take a
// common suffix (s, es, d, ed, ly). Each match contributes PerMatchWeight,
// up to MaxHitsPerIndicator for a strong indicator and
// MaxHitsPerWeakIndicator for a weak one, capped at MaxScore.
type Heuristic struct {
	indicators []indicator
}

type indicator struct {
	pattern *regexp.Regexp
	maxHits int
	weak    bool
}

var (
	strongPhrases = []string{
		"ignore previous instructions",
		"disregard previous",
		"forget instructions",
	}
	weakPhrases = []string{
		"you are now",
		"system prompt",
		"act as",
		"prompt injection",
	}
)

// NewHeuristic returns the default Heuristic with the built-in indicator set.
func NewHeuristic() *Heuristic {
	h := &Heuristic{}
	for _, p := range strongPhrases {
		h.indicators = append(h.indicators, indicator{pattern: phrasePattern(p), maxHits: MaxHitsPerIndicator})
	}
	for _, p := range weakPhrases {
		h.indicators = append(h.indicators, indicator{pattern: phrasePattern(p), maxHits: MaxHitsPerWeakIndicator, weak: true})
	}
	return h
}

const fillerWords = `(?:all|the|any|your|my|prior|above)`

func phrasePattern(phrase string) *regexp.Regexp {
	words := strings.Fields(phrase)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	words[0] = inflectFirst(words[0])
	return regexp.MustCompile(`\b` + strings.Join(words, ` (?:`+fillerWords+` )?`) + `(?:s|es|d|ed|ly)?\b`)
}

func inflectFirst(word string) string {
	if stem, ok := strings.CutSuffix(word, "e"); ok {
		return stem + `(?:e|es|ed|ing)`
	}
	return word + `(?:s|ed|ing)?`
}

// lookalikes maps Cyrillic and Greek letters that look like ASCII letters
// to those ASCII letters. It holds only the common ones. The map applies
// before the text is lower case, because a capital can look like a
// different ASCII letter than its lower case form. Greek capital nu looks
// like "N", but Greek small nu looks like "v".
var lookalikes = map[rune]rune{
	'\u0430': 'a', '\u0435': 'e', '\u043e': 'o', '\u0440': 'p', '\u0441': 'c',
	'\u0443': 'y', '\u0445': 'x', '\u0456': 'i', '\u0458': 'j', '\u0455': 's',
	'\u0410': 'a', '\u0415': 'e', '\u041e': 'o', '\u0420': 'p', '\u0421': 'c',
	'\u0423': 'y', '\u0425': 'x', '\u0406': 'i', '\u0408': 'j', '\u0405': 's',
	'\u03bf': 'o', '\u03b1': 'a', '\u03b5': 'e', '\u03b9': 'i', '\u03bd': 'v',
	'\u03c1': 'p', '\u03c4': 't', '\u03c5': 'u', '\u03ba': 'k',
	'\u039f': 'o', '\u0391': 'a', '\u0395': 'e', '\u0399': 'i', '\u039d': 'n',
	'\u03a1': 'p', '\u03a4': 't', '\u03a5': 'y', '\u039a': 'k',
}

var htmlSpaces = strings.NewReplacer("&nbsp;", " ", "&#160;", " ", "&#xa0;", " ")

// normalize folds the content so that one ASCII space separates words. An
// invisible format character, such as a zero-width space or a soft hyphen,
// or a combining mark inside a word can hide a phrase, so normalize removes
// it. A dash, markdown emphasis and a non-breaking space entity separate
// words. When foldUnderscore is true, "_" also separates words.
func normalize(content string, foldUnderscore bool) string {
	content = strings.Map(func(r rune) rune {
		if ascii, ok := lookalikes[r]; ok {
			return ascii
		}
		return r
	}, content)
	content = htmlSpaces.Replace(strings.ToLower(content))
	content = strings.Map(func(r rune) rune {
		switch {
		case unicode.In(r, unicode.Cf, unicode.Mn):
			return -1
		case r == '*', r == '`', r == '~', unicode.Is(unicode.Pd, r), unicode.IsSpace(r):
			return ' '
		case r == '_' && foldUnderscore:
			return ' '
		}
		return r
	}, content)
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
	strong, weak := normalize(content, true), normalize(content, false)

	var score float32
	for _, ind := range h.indicators {
		text := strong
		if ind.weak {
			text = weak
		}
		hits := len(ind.pattern.FindAllStringIndex(text, ind.maxHits))
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

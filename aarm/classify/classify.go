// Package classify tags actions with high-level data classification labels
// (secret, pii, source_code, etc). Used by the AARM mediation adapter to
// populate Action.DataClassifications which the PDP exposes as
// action.data_classifications and the accumulator unions into
// context.classifications_seen.
package classify

import (
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
)

// Classification labels emitted by the heuristic classifier.
const (
	LabelSecret      = "secret"
	LabelPII         = "pii"
	LabelSourceCode  = "source_code"
	LabelConfig      = "config"
	LabelGitInternal = "git_internal"
	LabelExternalURL = "external_url"

	// LabelUnknownSensitive is the AARM-required fail-safe default applied
	// by the mediation adapter when no classifier label was produced (the
	// classifier is disabled, the classifier returned an empty set, or the
	// action carries no classifiable surface). AARM R2 requires defaulting
	// to the highest sensitivity level when no classification mechanism
	// produces a result so policies that gate on classification fail safe.
	// Operators who explicitly want classification off and do not want this
	// label can flip policy.classify.fail_open to true.
	LabelUnknownSensitive = "unknown_sensitive"
)

// Classifier returns zero or more classification labels for an action.
type Classifier interface {
	Classify(action *model.Action) []string
}

// Heuristic is a pattern-matching Classifier. Patterns are compiled at
// construction time. Multiple labels can apply to one action.
type Heuristic struct {
	patterns map[string][]string
}

// HeuristicOption configures a Heuristic at construction time.
type HeuristicOption func(*heuristicConfig)

type heuristicConfig struct {
	extraPatterns map[string][]string
	secretBase    []string
}

// WithExtraPatterns merges additional glob patterns into the built-in pattern
// map keyed by label.
func WithExtraPatterns(extra map[string][]string) HeuristicOption {
	return func(c *heuristicConfig) {
		if c.extraPatterns == nil {
			c.extraPatterns = map[string][]string{}
		}
		for label, pats := range extra {
			c.extraPatterns[label] = append(c.extraPatterns[label], pats...)
		}
	}
}

// WithSecretPaths overrides the default secret pattern set with the supplied
// list. Typically populated from config.privacy.sensitive_paths so the
// classifier mirrors the privacy redaction surface.
func WithSecretPaths(paths []string) HeuristicOption {
	return func(c *heuristicConfig) {
		if len(paths) > 0 {
			c.secretBase = append([]string(nil), paths...)
		}
	}
}

// NewHeuristic returns a Heuristic with the built-in pattern set, optionally
// extended by callers. The default secret pattern set is sourced from
// core/events.DefaultSensitivePatterns so the classifier and the privacy
// redaction surface share a single source of truth.
func NewHeuristic(opts ...HeuristicOption) *Heuristic {
	cfg := &heuristicConfig{secretBase: events.DefaultSensitivePatterns()}
	for _, opt := range opts {
		opt(cfg)
	}

	patterns := map[string][]string{
		LabelSecret:      append([]string(nil), cfg.secretBase...),
		LabelPII:         defaultPIIPatterns(),
		LabelSourceCode:  defaultSourceCodePatterns(),
		LabelConfig:      defaultConfigPatterns(),
		LabelGitInternal: defaultGitInternalPatterns(),
	}
	for label, extra := range cfg.extraPatterns {
		patterns[label] = append(patterns[label], extra...)
	}

	return &Heuristic{patterns: patterns}
}

// Classify returns the labels applicable to action. The slice is sorted for
// deterministic output. Returns nil when no label matches.
func (h *Heuristic) Classify(action *model.Action) []string {
	if h == nil || action == nil {
		return nil
	}

	labels := map[string]struct{}{}

	paths := candidatePaths(action)
	for label, pats := range h.patterns {
		for _, p := range paths {
			if matchesAny(pats, p) {
				labels[label] = struct{}{}
				break
			}
		}
	}

	if isExternalURL(action.Parameters.URL) {
		labels[LabelExternalURL] = struct{}{}
	}

	if len(labels) == 0 {
		return nil
	}
	out := make([]string, 0, len(labels))
	for label := range labels {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// Nop is a Classifier that returns nil for every action. Used when
// classification is disabled in config.
type Nop struct{}

// NewNop returns a Nop classifier.
func NewNop() *Nop { return &Nop{} }

// Classify implements Classifier.
func (*Nop) Classify(*model.Action) []string { return nil }

// FailSafe wraps an inner Classifier so that any action the inner Classifier
// leaves unlabeled (including the case where inner is nil) is tagged with a
// single fallback label. The mediation adapter wires this in by default so
// policies that gate on classification fail safe. Operators who explicitly
// want classification off and do not want this label can construct the
// adapter without wrapping the inner classifier in FailSafe.
type FailSafe struct {
	inner Classifier
	label string
}

// NewFailSafe returns a Classifier that delegates to inner and, when inner
// produces no labels (or inner is nil), returns []string{label}.
func NewFailSafe(inner Classifier, label string) Classifier {
	return &FailSafe{inner: inner, label: label}
}

// Classify implements Classifier.
func (f *FailSafe) Classify(action *model.Action) []string {
	if f == nil {
		return nil
	}
	var labels []string
	if f.inner != nil {
		labels = f.inner.Classify(action)
	}
	if len(labels) == 0 {
		return []string{f.label}
	}
	return labels
}

func candidatePaths(action *model.Action) []string {
	out := make([]string, 0, 4)
	if p := strings.TrimSpace(action.Parameters.Path); p != "" {
		out = append(out, filepath.ToSlash(p))
	}
	if u := strings.TrimSpace(action.Parameters.URL); u != "" {
		if parsed, err := url.Parse(u); err == nil && parsed.Path != "" {
			out = append(out, filepath.ToSlash(parsed.Path))
		}
	}
	if action.Type == model.ActionToolUse && action.Parameters.Raw != nil {
		for _, key := range []string{"file_path", "path", "url"} {
			if v, ok := action.Parameters.Raw[key].(string); ok && v != "" {
				out = append(out, filepath.ToSlash(v))
			}
		}
	}
	return out
}

func matchesAny(patterns []string, value string) bool {
	if value == "" {
		return false
	}
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if ok, _ := doublestar.Match(pat, value); ok {
			return true
		}
		base := filepath.Base(value)
		if ok, _ := doublestar.Match(pat, base); ok {
			return true
		}
	}
	return false
}

func isExternalURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	return true
}

func defaultPIIPatterns() []string {
	return []string{
		"**/data/users/**",
		"**/customers/**",
		"**/*personal*",
		"**/*pii*",
	}
}

func defaultSourceCodePatterns() []string {
	return []string{
		"**/*.go",
		"**/*.py",
		"**/*.ts",
		"**/*.tsx",
		"**/*.js",
		"**/*.jsx",
		"**/*.rs",
		"**/*.java",
		"**/*.c",
		"**/*.cc",
		"**/*.cpp",
		"**/*.h",
		"**/*.hpp",
		"**/*.rb",
		"**/*.php",
		"**/*.swift",
		"**/*.kt",
		"**/*.scala",
		"**/*.cs",
	}
}

func defaultConfigPatterns() []string {
	return []string{
		"**/*.yaml",
		"**/*.yml",
		"**/*.toml",
		"**/*.json",
		"**/*.tf",
		"**/Dockerfile",
		"**/Makefile",
	}
}

func defaultGitInternalPatterns() []string {
	return []string{
		"**/.git/**",
	}
}

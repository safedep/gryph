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
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
)

// privacy.ClassUnknownSensitive is the AARM R2 fail-safe class that FailSafe
// adds when no class was produced. Content labels use the Heuristic without
// FailSafe (see docs/content-labels.md).

// Classifier returns zero or more classes for an action.
type Classifier interface {
	Classify(action *model.Action) []privacy.Class
}

// Heuristic is a pattern-matching Classifier. Patterns are compiled at
// construction time. Multiple labels can apply to one action.
type Heuristic struct {
	patterns map[privacy.Class][]string
}

// HeuristicOption configures a Heuristic at construction time.
type HeuristicOption func(*heuristicConfig)

type heuristicConfig struct {
	extraPatterns map[privacy.Class][]string
	secretBase    []string
}

// WithExtraPatterns merges additional glob patterns into the built-in pattern
// map keyed by label. A key outside privacy.AllClasses is a custom policy
// label. Classify returns it, so CEL reads it in action.data_classifications.
// ClassifyEvent drops it, so it never becomes a content label.
func WithExtraPatterns(extra map[privacy.Class][]string) HeuristicOption {
	return func(c *heuristicConfig) {
		if c.extraPatterns == nil {
			c.extraPatterns = map[privacy.Class][]string{}
		}
		for class, pats := range extra {
			c.extraPatterns[class] = append(c.extraPatterns[class], pats...)
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
// core/privacy.DefaultSensitivePatterns so the classifier and the privacy
// redaction surface share a single source of truth.
func NewHeuristic(opts ...HeuristicOption) *Heuristic {
	cfg := &heuristicConfig{secretBase: privacy.DefaultSensitivePatterns()}
	for _, opt := range opts {
		opt(cfg)
	}

	patterns := map[privacy.Class][]string{
		privacy.ClassSecret:      append([]string(nil), cfg.secretBase...),
		privacy.ClassPII:         defaultPIIPatterns(),
		privacy.ClassSourceCode:  defaultSourceCodePatterns(),
		privacy.ClassConfig:      defaultConfigPatterns(),
		privacy.ClassGitInternal: defaultGitInternalPatterns(),
	}
	for label, extra := range cfg.extraPatterns {
		patterns[label] = append(patterns[label], extra...)
	}

	return &Heuristic{patterns: patterns}
}

// Classify returns the classes of action. The slice is sorted for
// deterministic output. Returns nil when no class matches.
func (h *Heuristic) Classify(action *model.Action) []privacy.Class {
	if h == nil || action == nil {
		return nil
	}
	return h.ClassifyPaths(candidatePaths(action), action.Parameters.URL)
}

// ClassifyEvent returns the classes of the paths and the URL that an event
// acts on. The decision service uses it to label content, so it returns
// only the closed set of privacy.Class. A custom extra_patterns key is a
// policy label that only Classify returns.
func (h *Heuristic) ClassifyEvent(event *events.Event) []privacy.Class {
	if h == nil || event == nil {
		return nil
	}
	paths, rawURL := event.Targets()
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		paths = append(paths, u.Path)
	}
	for i, p := range paths {
		paths[i] = filepath.ToSlash(p)
	}
	classes := slices.DeleteFunc(h.ClassifyPaths(paths, rawURL), func(c privacy.Class) bool {
		return !c.Valid()
	})
	if len(classes) == 0 {
		return nil
	}
	return classes
}

// ClassifyPaths returns the classes of paths and rawURL, sorted.
func (h *Heuristic) ClassifyPaths(paths []string, rawURL string) []privacy.Class {
	classes := map[privacy.Class]struct{}{}
	for class, pats := range h.patterns {
		for _, p := range paths {
			if matchesAny(pats, p) {
				classes[class] = struct{}{}
				break
			}
		}
	}
	if isExternalURL(rawURL) {
		classes[privacy.ClassExternalURL] = struct{}{}
	}
	if len(classes) == 0 {
		return nil
	}
	out := make([]privacy.Class, 0, len(classes))
	for class := range classes {
		out = append(out, class)
	}
	slices.Sort(out)
	return out
}

// Nop is a Classifier that returns nil for every action. Used when
// classification is disabled in config.
type Nop struct{}

// NewNop returns a Nop classifier.
func NewNop() *Nop { return &Nop{} }

// Classify implements Classifier.
func (*Nop) Classify(*model.Action) []privacy.Class { return nil }

// FailSafe wraps an inner Classifier so that any action the inner Classifier
// leaves unlabeled (including the case where inner is nil) is tagged with a
// single fallback label. The mediation adapter wires this in by default so
// policies that gate on classification fail safe. Operators who explicitly
// want classification off and do not want this label can construct the
// adapter without wrapping the inner classifier in FailSafe.
type FailSafe struct {
	inner Classifier
	class privacy.Class
}

// NewFailSafe returns a Classifier that delegates to inner and, when inner
// produces no classes (or inner is nil), returns []privacy.Class{class}.
func NewFailSafe(inner Classifier, class privacy.Class) Classifier {
	return &FailSafe{inner: inner, class: class}
}

// Classify implements Classifier.
func (f *FailSafe) Classify(action *model.Action) []privacy.Class {
	if f == nil {
		return nil
	}
	var classes []privacy.Class
	if f.inner != nil {
		classes = f.inner.Classify(action)
	}
	if len(classes) == 0 {
		return []privacy.Class{f.class}
	}
	return classes
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

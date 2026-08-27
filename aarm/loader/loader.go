// Package loader assembles a pdp.Policy from one or more policy Sources.
package loader

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/pdp"
)

// Source produces parsed policy documents.
type Source interface {
	Name() string
	Load(ctx context.Context) ([]*pdp.Policy, error)
}

// Loader merges Sources into a single pdp.Policy.
type Loader struct {
	sources []Source
}

func New(sources ...Source) *Loader {
	return &Loader{sources: sources}
}

func (l *Loader) Sources() []Source {
	return l.sources
}

// Load merges all sources in order. Duplicate rule IDs across sources are an
// error. A document's disabled: removes only the rules that the same document
// defines. This scope is uniform for every source, so the file that declares a
// rule is the only file that can disable it. It matches the file an author or
// agent edits. It stops one file from changing another file's rules.
//
// Built-in rules (from a *BuiltinSource) are isolated from user sources: they
// are appended after user rules and are never removed by a disabled: entry, so
// a user policy cannot weaken the self-protection rules. A user rule may not use
// the reserved BuiltinRuleIDPrefix. Any that does is a load error, even when the
// same file also lists that ID under disabled:.
func (l *Loader) Load(ctx context.Context) (*pdp.Policy, error) {
	owner := make(map[string]string)
	appliedDisabled := make(map[string]struct{})
	var userRules []pdp.Rule
	var builtinRules []pdp.Rule

	for _, src := range l.sources {
		_, isBuiltin := src.(*BuiltinSource)
		docs, err := src.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("loader: source %s: %w", src.Name(), err)
		}
		for _, doc := range docs {
			if doc == nil {
				continue
			}
			disabled := docDisabledSet(doc, isBuiltin)
			matchedDisabled := make(map[string]struct{}, len(disabled))
			for _, rule := range doc.Rules {
				if !isBuiltin && strings.HasPrefix(rule.ID, BuiltinRuleIDPrefix) {
					return nil, fmt.Errorf("loader: rule id %q in %s uses reserved prefix %q", rule.ID, src.Name(), BuiltinRuleIDPrefix)
				}
				if _, off := disabled[rule.ID]; off {
					matchedDisabled[rule.ID] = struct{}{}
					appliedDisabled[rule.ID] = struct{}{}
					continue
				}
				if prev, ok := owner[rule.ID]; ok {
					return nil, fmt.Errorf("loader: duplicate rule id %q in %s (already defined in %s)", rule.ID, src.Name(), prev)
				}
				owner[rule.ID] = src.Name()
				if isBuiltin {
					builtinRules = append(builtinRules, rule)
					continue
				}
				userRules = append(userRules, rule)
			}
			warnUnmatchedDisabled(src.Name(), disabled, matchedDisabled)
		}
	}

	merged := &pdp.Policy{Rules: make([]pdp.Rule, 0, len(userRules)+len(builtinRules))}
	merged.Rules = append(merged.Rules, userRules...)
	// Built-in rules are appended last and are never filtered by disabled:.
	merged.Rules = append(merged.Rules, builtinRules...)
	if len(appliedDisabled) > 0 {
		merged.Disabled = sortedKeys(appliedDisabled)
	}

	if _, err := pdp.New(merged); err != nil {
		return nil, fmt.Errorf("loader: compile merged policy: %w", err)
	}
	return merged, nil
}

// docDisabledSet returns the non-empty disabled IDs a user document declares.
// A built-in document has no disabled scope, so it returns an empty set.
func docDisabledSet(doc *pdp.Policy, isBuiltin bool) map[string]struct{} {
	if isBuiltin {
		return nil
	}
	set := make(map[string]struct{}, len(doc.Disabled))
	for _, id := range doc.Disabled {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	return set
}

// warnUnmatchedDisabled reports a disabled: entry that names no rule in the
// same file. disabled: is scoped to the declaring file, so such an entry has no
// effect. The warning surfaces a typo or a misplaced entry.
func warnUnmatchedDisabled(source string, disabled, matched map[string]struct{}) {
	for id := range disabled {
		if _, ok := matched[id]; !ok {
			log.Warnf("loader: source %s lists rule %q under disabled but does not define it. A disabled entry removes only a rule from the same file", source, id)
		}
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

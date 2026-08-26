// Package loader assembles a pdp.Policy from one or more policy Sources.
package loader

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
// error. Rule IDs listed in a user source's Disabled are removed from the
// merged rule set.
//
// Built-in rules (from a *BuiltinSource) are isolated from user sources: they
// are appended after user rules and are never removed by a disabled: entry, so
// a repo-local policy cannot weaken the self-protection rules. User rules may
// not use the reserved BuiltinRuleIDPrefix; any that do are a load error.
func (l *Loader) Load(ctx context.Context) (*pdp.Policy, error) {
	owner := make(map[string]string)
	disabled := make(map[string]struct{})
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
			if !isBuiltin {
				for _, id := range doc.Disabled {
					if id == "" {
						continue
					}
					disabled[id] = struct{}{}
				}
			}
			for _, rule := range doc.Rules {
				if prev, ok := owner[rule.ID]; ok {
					return nil, fmt.Errorf("loader: duplicate rule id %q in %s (already defined in %s)", rule.ID, src.Name(), prev)
				}
				owner[rule.ID] = src.Name()
				if isBuiltin {
					builtinRules = append(builtinRules, rule)
					continue
				}
				if strings.HasPrefix(rule.ID, BuiltinRuleIDPrefix) {
					return nil, fmt.Errorf("loader: rule id %q in %s uses reserved prefix %q", rule.ID, src.Name(), BuiltinRuleIDPrefix)
				}
				userRules = append(userRules, rule)
			}
		}
	}

	merged := &pdp.Policy{Rules: make([]pdp.Rule, 0, len(userRules)+len(builtinRules))}
	for _, rule := range userRules {
		if _, drop := disabled[rule.ID]; drop {
			continue
		}
		merged.Rules = append(merged.Rules, rule)
	}
	// Built-in rules are appended last and are never filtered by disabled:.
	merged.Rules = append(merged.Rules, builtinRules...)
	if len(disabled) > 0 {
		merged.Disabled = sortedKeys(disabled)
	}

	if _, err := pdp.New(merged); err != nil {
		return nil, fmt.Errorf("loader: compile merged policy: %w", err)
	}
	return merged, nil
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

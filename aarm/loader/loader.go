// Package loader assembles a pdp.Policy from one or more policy Sources.
package loader

import (
	"context"
	"fmt"

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
// error. Rule IDs listed in any source's Disabled are removed from the merged
// rule set.
func (l *Loader) Load(ctx context.Context) (*pdp.Policy, error) {
	merged := &pdp.Policy{Rules: []pdp.Rule{}}
	owner := make(map[string]string)
	disabled := make(map[string]struct{})

	for _, src := range l.sources {
		docs, err := src.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("loader: source %s: %w", src.Name(), err)
		}
		for _, doc := range docs {
			if doc == nil {
				continue
			}
			for _, id := range doc.Disabled {
				if id == "" {
					continue
				}
				disabled[id] = struct{}{}
			}
			for _, rule := range doc.Rules {
				if prev, ok := owner[rule.ID]; ok {
					return nil, fmt.Errorf("loader: duplicate rule id %q in %s (already defined in %s)", rule.ID, src.Name(), prev)
				}
				owner[rule.ID] = src.Name()
				merged.Rules = append(merged.Rules, rule)
			}
		}
	}

	if len(disabled) > 0 {
		filtered := merged.Rules[:0]
		for _, rule := range merged.Rules {
			if _, drop := disabled[rule.ID]; drop {
				continue
			}
			filtered = append(filtered, rule)
		}
		merged.Rules = filtered
		merged.Disabled = sortedKeys(disabled)
	}

	if _, err := pdp.New(merged); err != nil {
		return nil, fmt.Errorf("loader: compile merged policy: %w", err)
	}
	return merged, nil
}

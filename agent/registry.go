package agent

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/safedep/gryph/core/events"
)

// Registry manages registered agent adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates a new adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter to the registry.
func (r *Registry) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Name()] = adapter
}

// Get retrieves an adapter by name.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[name]
	return adapter, ok
}

// List returns all registered adapter names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

// All returns all registered adapters.
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapters := make([]Adapter, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		adapters = append(adapters, adapter)
	}
	return adapters
}

// HookConfigGlobs returns the hook config globs of all registered adapters,
// sorted and without duplicates. The order is stable because the policy hash
// includes it.
func (r *Registry) HookConfigGlobs() []string {
	var globs []string
	for _, adapter := range r.All() {
		globs = append(globs, adapter.HookConfigPaths()...)
	}
	slices.Sort(globs)
	return slices.Compact(globs)
}

// HookSpec returns the declared spec of a hook type for an agent.
func (r *Registry) HookSpec(agentName string, hookType events.HookType) (events.HookSpec, bool) {
	adapter, ok := r.Get(agentName)
	if !ok {
		return events.HookSpec{}, false
	}
	for _, spec := range adapter.Hooks() {
		if spec.Type == hookType {
			return spec, true
		}
	}
	return events.HookSpec{}, false
}

// DetectAll runs detection on all registered adapters.
func (r *Registry) DetectAll(ctx context.Context) map[string]*DetectionResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]*DetectionResult)
	for name, adapter := range r.adapters {
		result, err := adapter.Detect(ctx)
		if err != nil {
			results[name] = &DetectionResult{
				Installed: false,
				Message:   fmt.Sprintf("detection error: %v", err),
			}
		} else {
			results[name] = result
		}
	}
	return results
}

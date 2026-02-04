package agent

import (
	"context"
	"fmt"
	"sync"
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

// SupportedAgents returns the list of supported agent names.
func SupportedAgents() []string {
	return []string{
		"claude-code",
		"cursor",
		"gemini",
		"opencode",
		"openclaw",
		"windsurf",
	}
}

// DefaultRegistry returns a registry with all default adapters registered.
func DefaultRegistry() *Registry {
	registry := NewRegistry()
	// Adapters are registered in their respective packages
	return registry
}

package stream

import (
	"sync"

	corestream "github.com/safedep/gryph/core/stream"
)

// Registry manages registered stream targets.
type Registry struct {
	mu      sync.RWMutex
	targets map[string]corestream.Target
}

// NewRegistry creates a new stream target registry.
func NewRegistry() *Registry {
	return &Registry{
		targets: make(map[string]corestream.Target),
	}
}

// Register adds a target to the registry.
func (r *Registry) Register(target corestream.Target) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.targets[target.Name()] = target
}

// Get retrieves a target by name.
func (r *Registry) Get(name string) (corestream.Target, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	target, ok := r.targets[name]
	return target, ok
}

// All returns all registered targets.
func (r *Registry) All() []corestream.Target {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targets := make([]corestream.Target, 0, len(r.targets))
	for _, t := range r.targets {
		targets = append(targets, t)
	}

	return targets
}

// Enabled returns all enabled targets.
func (r *Registry) Enabled() []corestream.Target {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var targets []corestream.Target
	for _, t := range r.targets {
		if t.Enabled() {
			targets = append(targets, t)
		}
	}

	return targets
}

package projectdetection

import (
	"path/filepath"
	"sync"
)

// Registry holds an ordered list of detectors; first success wins.
type Registry struct {
	mu        sync.RWMutex
	detectors []Detector
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		detectors: make([]Detector, 0),
	}
}

// Register appends a detector to the registry (order matters).
func (r *Registry) Register(d Detector) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.detectors = append(r.detectors, d)
}

// Detect runs all detectors in order; returns first non-nil ProjectInfo with
// non-empty Name, or (nil, ErrNoProjectDetected) if none succeed.
func (r *Registry) Detect(path string) (*ProjectInfo, error) {
	r.mu.RLock()
	detectors := make([]Detector, len(r.detectors))
	copy(detectors, r.detectors)
	r.mu.RUnlock()

	path = filepath.Clean(path)
	for _, d := range detectors {
		info, err := d.Detect(path)
		if err != nil {
			continue
		}

		if info != nil && info.Name != "" {
			return info, nil
		}
	}

	return nil, ErrNoProjectDetected
}

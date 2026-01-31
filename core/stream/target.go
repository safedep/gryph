package stream

import (
	"context"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
)

// StreamItem represents a single item to be streamed to a target.
type StreamItem struct {
	Event     *events.Event           `json:"event,omitempty"`
	SelfAudit *storage.SelfAuditEntry `json:"self_audit,omitempty"`
}

// Target defines the interface for a stream destination.
type Target interface {
	// Name returns the name of the stream target.
	Name() string
	// Enabled returns true if the target is enabled.
	Enabled() bool
	// Send sends the stream items to the target.
	Send(ctx context.Context, items []StreamItem) error
	// Close allows the target to clean up any resources.
	Close() error
}

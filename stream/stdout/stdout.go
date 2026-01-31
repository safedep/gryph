package stdout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corestream "github.com/safedep/gryph/core/stream"
	"github.com/safedep/gryph/stream"
)

// Target implements stream.Target by printing JSON to stdout.
type Target struct {
	name    string
	enabled bool
}

// New creates a new stdout stream target.
func New(name string, enabled bool) *Target {
	return &Target{
		name:    name,
		enabled: enabled,
	}
}

func (t *Target) Name() string  { return t.name }
func (t *Target) Type() string  { return stream.TargetTypeStdout }
func (t *Target) Enabled() bool { return t.enabled }

func (t *Target) Send(_ context.Context, items []corestream.StreamItem) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return fmt.Errorf("failed to encode stream item: %w", err)
		}
	}

	return nil
}

func (t *Target) Close() error { return nil }

var _ corestream.Target = (*Target)(nil)

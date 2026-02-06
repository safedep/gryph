package nop

import (
	"context"

	corestream "github.com/safedep/gryph/core/stream"
	"github.com/safedep/gryph/stream"
)

type Target struct {
	name    string
	enabled bool
}

func New(name string, enabled bool) *Target {
	return &Target{
		name:    name,
		enabled: enabled,
	}
}

func (t *Target) Name() string  { return t.name }
func (t *Target) Type() string  { return stream.TargetTypeNop }
func (t *Target) Enabled() bool { return t.enabled }

func (t *Target) Send(_ context.Context, _ []corestream.StreamItem) error {
	return nil
}

func (t *Target) Close() error { return nil }

var _ corestream.Target = (*Target)(nil)

package cli

import (
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func TestHooksAboveVersion(t *testing.T) {
	hooks := []events.HookSpec{
		{Type: "BeforeAgent", Prompt: true, MinVersion: "0.26.0"},
		{Type: "BeforeTool"},
	}
	cases := []struct {
		version string
		want    []string
	}{
		{"0.25.3", []string{"BeforeAgent (needs 0.26.0)"}},
		{"v0.25.0", []string{"BeforeAgent (needs 0.26.0)"}},
		{"0.26.0", nil},
		{"0.35.1", nil},
		{"1.0.0-nightly.1", nil},
		{"0.26.0-preview.1", nil},
		{"0.26.0-nightly.20260110", nil},
		{"0.25.9-preview.1", []string{"BeforeAgent (needs 0.26.0)"}},
		{"unknown", nil},
		{"", nil},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			assert.Equal(t, tc.want, hooksAboveVersion(hooks, tc.version))
		})
	}
}

func TestAdapterHooks_MinVersionIsSemver(t *testing.T) {
	for _, a := range coverageAdapters() {
		for _, h := range a.Hooks() {
			if h.MinVersion != "" {
				assert.NotEmpty(t, canonicalVersion(h.MinVersion), "%s %s MinVersion %q", a.Name(), h.Type, h.MinVersion)
			}
		}
	}
}

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecision_EveryConstantRoundTrips(t *testing.T) {
	seen := map[string]Decision{}
	for d := Decision(0); d < decisionCount; d++ {
		name := d.String()
		require.NotEqual(t, "unknown", name, "decision %d has no name", int(d))
		require.NotContains(t, seen, name, "decision %d reuses a name", int(d))
		seen[name] = d

		text, err := d.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, name, string(text))

		var got Decision
		require.NoError(t, got.UnmarshalText(text))
		assert.Equal(t, d, got)

		parsed, ok := ParseDecision(name)
		assert.True(t, ok)
		assert.Equal(t, d, parsed)
	}
}

func TestDecision_UnknownValues(t *testing.T) {
	for _, d := range []Decision{-1, decisionCount, 99} {
		assert.Equal(t, "unknown", d.String())
		_, err := d.MarshalText()
		assert.Error(t, err)
	}

	for _, name := range []string{"", "unknown", "defer", "Allow"} {
		_, ok := ParseDecision(name)
		assert.False(t, ok, name)

		var got Decision
		assert.Error(t, got.UnmarshalText([]byte(name)), name)
	}
}

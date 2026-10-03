package transcript

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/core/cost"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollector_Collect(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		usage  *cost.SessionUsage
		models int
	}{
		{
			name:   "multi model",
			path:   filepath.Join("testdata", "transcript_multi_model.jsonl"),
			usage:  &cost.SessionUsage{InputTokens: 8000, OutputTokens: 4300, CacheReadTokens: 1500, CacheWriteTokens: 750},
			models: 2,
		},
		{
			name:   "single model",
			path:   filepath.Join("testdata", "transcript_single_model.jsonl"),
			usage:  &cost.SessionUsage{InputTokens: 1500},
			models: 1,
		},
		{
			name:   "malformed lines are skipped",
			path:   filepath.Join("testdata", "transcript_malformed.jsonl"),
			usage:  &cost.SessionUsage{InputTokens: 300},
			models: 1,
		},
		{name: "empty file", path: filepath.Join("testdata", "transcript_empty.jsonl")},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.jsonl")},
		{name: "empty path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, err := NewCollector().Collect(context.Background(), tt.path)
			require.NoError(t, err)
			if tt.usage == nil {
				assert.Nil(t, usage)
				return
			}
			require.NotNil(t, usage)
			assert.Len(t, usage.Models, tt.models)
			assert.Equal(t, tt.usage.InputTokens, usage.InputTokens)
			if tt.usage.OutputTokens != 0 {
				assert.Equal(t, tt.usage.OutputTokens, usage.OutputTokens)
				assert.Equal(t, tt.usage.CacheReadTokens, usage.CacheReadTokens)
				assert.Equal(t, tt.usage.CacheWriteTokens, usage.CacheWriteTokens)
			}
		})
	}
}

func TestCollector_SingleModelName(t *testing.T) {
	usage, err := NewCollector().Collect(context.Background(), filepath.Join("testdata", "transcript_single_model.jsonl"))
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, "claude-sonnet-4-20250514", usage.Models[0].Model)
}

func TestCollector_Source(t *testing.T) {
	assert.Equal(t, cost.CostSourceTranscript, NewCollector().Source())
}

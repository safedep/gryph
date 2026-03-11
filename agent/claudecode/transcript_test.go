package claudecode

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTranscriptCollector_MultiModel(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), filepath.Join("testdata", "transcript_multi_model.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage == nil {
		t.Fatal("expected usage, got nil")
	}

	if len(usage.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(usage.Models))
	}

	// Check aggregate totals
	// sonnet: input=3000, output=1300, cache_read=500, cache_write=250
	// opus: input=5000, output=3000, cache_read=1000, cache_write=500
	if usage.InputTokens != 8000 {
		t.Errorf("expected input_tokens=8000, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 4300 {
		t.Errorf("expected output_tokens=4300, got %d", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 1500 {
		t.Errorf("expected cache_read_tokens=1500, got %d", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 750 {
		t.Errorf("expected cache_write_tokens=750, got %d", usage.CacheWriteTokens)
	}
}

func TestTranscriptCollector_SingleModel(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), filepath.Join("testdata", "transcript_single_model.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage == nil {
		t.Fatal("expected usage, got nil")
	}
	if len(usage.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(usage.Models))
	}
	if usage.Models[0].Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", usage.Models[0].Model)
	}
	if usage.InputTokens != 1500 {
		t.Errorf("expected input_tokens=1500, got %d", usage.InputTokens)
	}
}

func TestTranscriptCollector_EmptyFile(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), filepath.Join("testdata", "transcript_empty.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != nil {
		t.Errorf("expected nil usage for empty file, got %+v", usage)
	}
}

func TestTranscriptCollector_MissingFile(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), "/nonexistent/path.jsonl")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if usage != nil {
		t.Errorf("expected nil usage for missing file, got %+v", usage)
	}
}

func TestTranscriptCollector_EmptyPath(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != nil {
		t.Errorf("expected nil usage for empty path, got %+v", usage)
	}
}

func TestTranscriptCollector_MalformedLines(t *testing.T) {
	c := NewTranscriptCollector()
	usage, err := c.Collect(context.Background(), filepath.Join("testdata", "transcript_malformed.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage == nil {
		t.Fatal("expected usage (valid lines should be parsed)")
	}
	// Should have parsed 2 valid assistant messages, skipping the malformed line
	if usage.InputTokens != 300 {
		t.Errorf("expected input_tokens=300, got %d", usage.InputTokens)
	}
}

func TestTranscriptCollector_Source(t *testing.T) {
	c := NewTranscriptCollector()
	if c.Source() != "transcript" {
		t.Errorf("expected source 'transcript', got '%s'", c.Source())
	}
}

package pricing

import (
	"testing"
)

func TestBundledProvider_ExactMatch(t *testing.T) {
	p, err := NewBundledProvider()
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	pricing, err := p.GetPricing("anthropic/claude-sonnet-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pricing == nil {
		t.Fatal("expected pricing for anthropic/claude-sonnet-4, got nil")
	}
	if pricing.InputPer1M <= 0 {
		t.Errorf("expected positive input pricing, got %f", pricing.InputPer1M)
	}
}

func TestBundledProvider_DateSuffixStripping(t *testing.T) {
	p, err := NewBundledProvider()
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	pricing, err := p.GetPricing("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pricing == nil {
		t.Fatal("expected pricing for claude-sonnet-4-20250514 (should resolve via date stripping + provider prefix), got nil")
	}
}

func TestBundledProvider_UnknownModel(t *testing.T) {
	p, err := NewBundledProvider()
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	pricing, err := p.GetPricing("unknown/model-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pricing != nil {
		t.Errorf("expected nil for unknown model, got %+v", pricing)
	}
}

func TestBundledProvider_ListModels(t *testing.T) {
	p, err := NewBundledProvider()
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	models := p.ListModels()
	if len(models) == 0 {
		t.Fatal("expected non-empty model list")
	}
}

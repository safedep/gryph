package cost

import (
	"time"

	"github.com/google/uuid"
)

// ModelPricing contains the per-token pricing for a model.
type ModelPricing struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	InputPer1M  float64 `json:"input"`
	OutputPer1M float64 `json:"output"`
	CacheRead   float64 `json:"cache_read"`
	CacheWrite  float64 `json:"cache_write"`
}

// PricingProvider looks up pricing for a model.
type PricingProvider interface {
	// GetPricing returns pricing for a model ID (as seen in transcripts).
	// Returns nil, nil if model is not found (not an error).
	GetPricing(modelID string) (*ModelPricing, error)

	// ListModels returns all known model IDs.
	ListModels() []string
}

// CostCalculator computes dollar cost from session usage.
type CostCalculator interface {
	Calculate(usage *SessionUsage) (*SessionCost, error)
}

// DefaultCalculator implements CostCalculator using a PricingProvider.
type DefaultCalculator struct {
	provider  PricingProvider
	sessionID uuid.UUID
	source    CostSource
}

// NewDefaultCalculator creates a CostCalculator backed by the given PricingProvider.
func NewDefaultCalculator(provider PricingProvider, sessionID uuid.UUID, source CostSource) *DefaultCalculator {
	return &DefaultCalculator{
		provider:  provider,
		sessionID: sessionID,
		source:    source,
	}
}

// Calculate computes the cost for each model in the usage and returns a SessionCost.
func (c *DefaultCalculator) Calculate(usage *SessionUsage) (*SessionCost, error) {
	if usage == nil {
		return nil, nil
	}

	sc := &SessionCost{
		SessionID:  c.sessionID,
		Usage:      *usage,
		Currency:   "USD",
		Source:     c.source,
		ComputedAt: time.Now().UTC(),
	}

	for _, mu := range usage.Models {
		mc := ModelCost{Model: mu.Model}

		pricing, err := c.provider.GetPricing(mu.Model)
		if err != nil {
			return nil, err
		}

		if pricing != nil {
			mc.InputCost = float64(mu.InputTokens) * pricing.InputPer1M / 1_000_000
			mc.OutputCost = float64(mu.OutputTokens) * pricing.OutputPer1M / 1_000_000
			mc.CacheCost = float64(mu.CacheReadTokens)*pricing.CacheRead/1_000_000 +
				float64(mu.CacheWriteTokens)*pricing.CacheWrite/1_000_000
		}

		mc.TotalCost = mc.InputCost + mc.OutputCost + mc.CacheCost
		sc.Models = append(sc.Models, mc)
		sc.TotalCost += mc.TotalCost
	}

	return sc, nil
}

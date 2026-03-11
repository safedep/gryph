package pricing

import (
	"github.com/safedep/gryph/core/cost"
)

type BundledProvider struct {
	models  map[string]cost.ModelPricing
	aliases map[string]string
}

func NewBundledProvider() (*BundledProvider, error) {
	entries, err := parseModels()
	if err != nil {
		return nil, err
	}

	models := make(map[string]cost.ModelPricing, len(entries))
	for _, e := range entries {
		models[e.ID] = cost.ModelPricing{
			ID:          e.ID,
			Name:        e.Name,
			InputPer1M:  e.Cost.Input,
			OutputPer1M: e.Cost.Output,
			CacheRead:   e.Cost.CacheRead,
			CacheWrite:  e.Cost.CacheWrite,
		}
	}

	return &BundledProvider{
		models:  models,
		aliases: buildAliases(),
	}, nil
}

func (p *BundledProvider) GetPricing(modelID string) (*cost.ModelPricing, error) {
	if canonical, ok := p.aliases[modelID]; ok {
		modelID = canonical
	}

	canonical := normalizeModelID(modelID, p.models)
	if canonical == "" {
		return nil, nil
	}

	pricing := p.models[canonical]
	return &pricing, nil
}

func (p *BundledProvider) ListModels() []string {
	models := make([]string, 0, len(p.models))
	for id := range p.models {
		models = append(models, id)
	}
	return models
}

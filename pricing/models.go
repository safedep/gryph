package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/safedep/gryph/core/cost"
)

//go:embed models.json
var modelsJSON []byte

type modelEntry struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Cost costEntry `json:"cost"`
}

type costEntry struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

var dateVersionSuffix = regexp.MustCompile(`-\d{8}$`)

func parseModels() ([]modelEntry, error) {
	var models []modelEntry
	if err := json.Unmarshal(modelsJSON, &models); err != nil {
		return nil, fmt.Errorf("failed to parse bundled models: %w", err)
	}
	return models, nil
}

func buildAliases() map[string]string {
	return map[string]string{}
}

func stripDateSuffix(modelID string) string {
	return dateVersionSuffix.ReplaceAllString(modelID, "")
}

func normalizeModelID(modelID string, models map[string]cost.ModelPricing) string {
	if _, ok := models[modelID]; ok {
		return modelID
	}

	stripped := stripDateSuffix(modelID)
	if stripped != modelID {
		if _, ok := models[stripped]; ok {
			return stripped
		}
	}

	providers := []string{"anthropic/", "openai/", "google/", "meta/", "mistral/"}
	for _, prefix := range providers {
		candidate := prefix + stripped
		if _, ok := models[candidate]; ok {
			return candidate
		}
		candidate = prefix + modelID
		if _, ok := models[candidate]; ok {
			return candidate
		}
	}

	for canonical := range models {
		parts := strings.SplitN(canonical, "/", 2)
		if len(parts) == 2 && parts[1] == stripped {
			return canonical
		}
	}

	return ""
}

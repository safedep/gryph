package pricing

import (
	"encoding/json"
	"testing"
)

func TestModelsJSON_Parseable(t *testing.T) {
	models, err := parseModels()
	if err != nil {
		t.Fatalf("failed to parse models.json: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("models.json is empty")
	}
}

func TestModelsJSON_ValidSchema(t *testing.T) {
	models, err := parseModels()
	if err != nil {
		t.Fatalf("failed to parse models.json: %v", err)
	}

	for _, m := range models {
		if m.ID == "" {
			t.Error("model has empty id")
		}
		if m.Name == "" {
			t.Errorf("model %s has empty name", m.ID)
		}
		if m.Cost.Input <= 0 && m.Cost.Output <= 0 {
			t.Errorf("model %s has no pricing data", m.ID)
		}
	}
}

func TestModelsJSON_NoDuplicateIDs(t *testing.T) {
	models, err := parseModels()
	if err != nil {
		t.Fatalf("failed to parse models.json: %v", err)
	}

	seen := make(map[string]bool)
	for _, m := range models {
		if seen[m.ID] {
			t.Errorf("duplicate model id: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestModelsJSON_KnownModelsPresent(t *testing.T) {
	models, err := parseModels()
	if err != nil {
		t.Fatalf("failed to parse models.json: %v", err)
	}

	ids := make(map[string]bool, len(models))
	for _, m := range models {
		ids[m.ID] = true
	}

	required := []string{
		"anthropic/claude-sonnet-4",
	}

	for _, id := range required {
		if !ids[id] {
			t.Errorf("required model %q not found in models.json", id)
		}
	}
}

func TestModelsJSON_PositivePricing(t *testing.T) {
	models, err := parseModels()
	if err != nil {
		t.Fatalf("failed to parse models.json: %v", err)
	}

	for _, m := range models {
		if m.Cost.Input < 0 {
			t.Errorf("model %s has negative input cost", m.ID)
		}
		if m.Cost.Output < 0 {
			t.Errorf("model %s has negative output cost", m.ID)
		}
		if m.Cost.CacheRead < 0 {
			t.Errorf("model %s has negative cache_read cost", m.ID)
		}
		if m.Cost.CacheWrite < 0 {
			t.Errorf("model %s has negative cache_write cost", m.ID)
		}
	}
}

func TestModelsJSON_ValidJSON(t *testing.T) {
	var raw json.RawMessage
	if err := json.Unmarshal(modelsJSON, &raw); err != nil {
		t.Fatalf("models.json is not valid JSON: %v", err)
	}
}

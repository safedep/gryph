//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

type rawProvider struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Models map[string]rawModel `json:"models"`
}

type rawModel struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Cost *rawCost `json:"cost"`
}

type rawCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type outputModel struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Cost outputCost `json:"cost"`
}

type outputCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

func main() {
	resp, err := http.Get("https://models.dev/api.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch models.dev: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read response: %v\n", err)
		os.Exit(1)
	}

	var providers map[string]rawProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse JSON: %v\n", err)
		os.Exit(1)
	}

	seen := make(map[string]bool)
	var output []outputModel
	for _, p := range providers {
		for _, m := range p.Models {
			if m.Cost == nil || (m.Cost.Input <= 0 && m.Cost.Output <= 0) {
				continue
			}
			id := m.ID
			if id == "" {
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			output = append(output, outputModel{
				ID:   id,
				Name: m.Name,
				Cost: outputCost{
					Input:      m.Cost.Input,
					Output:     m.Cost.Output,
					CacheRead:  m.Cost.CacheRead,
					CacheWrite: m.Cost.CacheWrite,
				},
			})
		}
	}

	sort.Slice(output, func(i, j int) bool {
		return output[i].ID < output[j].ID
	})

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal output: %v\n", err)
		os.Exit(1)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	outPath := filepath.Join(filepath.Dir(thisFile), "..", "models.json")
	if err := os.WriteFile(outPath, append(data, '\n'), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outPath, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %d models to %s\n", len(output), outPath)
}

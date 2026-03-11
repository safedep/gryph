package cost

import (
	"time"

	"github.com/google/uuid"
)

// CostSource identifies where token/cost data was collected from.
type CostSource string

const (
	CostSourceTranscript CostSource = "transcript"
	CostSourceHook       CostSource = "hook"
)

// ModelUsage represents token usage for a single model within a session.
type ModelUsage struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

// TotalTokens returns the total token count for this model.
func (m *ModelUsage) TotalTokens() int64 {
	return m.InputTokens + m.OutputTokens + m.CacheReadTokens + m.CacheWriteTokens
}

// SessionUsage represents aggregated token usage for an entire session.
type SessionUsage struct {
	Models           []ModelUsage `json:"models"`
	InputTokens      int64        `json:"input_tokens"`
	OutputTokens     int64        `json:"output_tokens"`
	CacheReadTokens  int64        `json:"cache_read_tokens"`
	CacheWriteTokens int64        `json:"cache_write_tokens"`
}

// TotalTokens returns the total token count across all models.
func (s *SessionUsage) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheWriteTokens
}

// Aggregate recomputes the top-level sums from the Models slice.
func (s *SessionUsage) Aggregate() {
	s.InputTokens = 0
	s.OutputTokens = 0
	s.CacheReadTokens = 0
	s.CacheWriteTokens = 0
	for _, m := range s.Models {
		s.InputTokens += m.InputTokens
		s.OutputTokens += m.OutputTokens
		s.CacheReadTokens += m.CacheReadTokens
		s.CacheWriteTokens += m.CacheWriteTokens
	}
}

// ModelCost represents the computed cost for a single model.
type ModelCost struct {
	Model      string  `json:"model"`
	InputCost  float64 `json:"input_cost"`
	OutputCost float64 `json:"output_cost"`
	CacheCost  float64 `json:"cache_cost"`
	TotalCost  float64 `json:"total_cost"`
}

// SessionCost represents the computed cost for an entire session.
type SessionCost struct {
	SessionID  uuid.UUID    `json:"session_id"`
	Usage      SessionUsage `json:"usage"`
	Models     []ModelCost  `json:"models"`
	TotalCost  float64      `json:"total_cost"`
	Currency   string       `json:"currency"`
	Source     CostSource   `json:"source"`
	ComputedAt time.Time    `json:"computed_at"`
}

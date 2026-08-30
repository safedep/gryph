package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func visibleRuneCount(line string) int {
	return utf8.RuneCountInString(ansiRegex.ReplaceAllString(line, ""))
}

func renderCostSummary(t *testing.T, summary *CostSummaryView) []string {
	t.Helper()
	var buf bytes.Buffer
	p := NewTablePresenter(PresenterOptions{Writer: &buf, UseColors: true, TerminalWidth: 200})
	require.NoError(t, p.RenderCostSummary(summary))
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func TestRenderCostSummaryByAgentAlignsColoredNames(t *testing.T) {
	lines := renderCostSummary(t, &CostSummaryView{
		GroupBy: "agent",
		Groups: []CostGroupView{
			{Label: "claude-code", SessionCount: 68, TotalTokens: 1578400000, TotalCost: 19.18},
			{Label: "codex", SessionCount: 9},
		},
		TotalSessions: 77,
		TotalTokens:   1578400000,
		TotalCost:     19.18,
	})

	require.Len(t, lines, 6)
	for i, line := range lines {
		assert.Equal(t, costGroupTableWidth, visibleRuneCount(line), "line %d: %q", i, line)
	}
}

func TestRenderCostSummaryBySessionAlignsColoredNames(t *testing.T) {
	lines := renderCostSummary(t, &CostSummaryView{
		GroupBy: "session",
		Sessions: []*CostSessionView{
			{ShortID: "a91f", AgentName: "claude-code", ProjectName: "gryph",
				StartedAt: time.Now(), ModelCount: 1, TotalTokens: 1200, TotalCost: 0.02},
			{ShortID: "b23c", AgentName: "codex", ProjectName: "gryph",
				StartedAt: time.Now(), ModelCount: 1, TotalTokens: 900, TotalCost: 0.01},
		},
		TotalSessions: 2,
		TotalTokens:   2100,
		TotalCost:     0.03,
	})

	require.GreaterOrEqual(t, len(lines), 5)
	for i, line := range lines[:5] {
		assert.Equal(t, costSessionTableWidth, visibleRuneCount(line), "line %d: %q", i, line)
	}
}

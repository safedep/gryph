package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/tui"
)

func (m Model) costTabLayout(height int) string {
	if m.width >= 80 {
		return m.costTwoColumnLayout(height)
	}
	return m.costSingleColumnLayout(height)
}

func (m Model) costTwoColumnLayout(height int) string {
	half := m.width / 2
	topH := (height - 2) / 3
	if topH < 4 {
		topH = 4
	}
	bottomH := height - 2 - topH*2

	row1 := twoColumnGrid(
		renderCostSummary(m.data, half, topH),
		renderCostByAgent(m.data, m.width-half, topH),
		m.width,
	)
	row2 := twoColumnGrid(
		renderCostByModel(m.data, half, topH),
		renderCostTokens(m.data, m.width-half, topH),
		m.width,
	)
	row3 := renderCostTrend(m.data, m.width, bottomH)

	return singleColumnStack(row1, row2, row3)
}

func (m Model) costSingleColumnLayout(height int) string {
	w := m.width
	panelH := 6

	return singleColumnStack(
		renderCostSummary(m.data, w, panelH),
		renderCostByAgent(m.data, w, panelH),
		renderCostByModel(m.data, w, panelH),
		renderCostTokens(m.data, w, panelH),
		renderCostTrend(m.data, w, panelH),
	)
}

func renderCostSummary(data *StatsData, width, height int) string {
	if data.SessionsWithCost == 0 {
		return renderPanel("SUMMARY", "  No cost data available", width, height)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Total Cost"),
		greenValueStyle.Render(tui.FormatCost(data.TotalCost)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Avg/Session"),
		valueStyle.Render(tui.FormatCost(data.AvgCostPerSession)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Sessions"),
		valueStyle.Render(fmt.Sprintf("%d of %d", data.SessionsWithCost, data.TotalSessions)),
	))

	totalTokens := int64(0)
	for _, ms := range data.ModelStats {
		totalTokens += ms.TotalTokens
	}
	if totalTokens > 0 {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(12).Render("Tokens"),
			valueStyle.Render(tui.FormatTokens(totalTokens)),
		))
	}

	if !data.TimeSpanStart.IsZero() {
		span := fmt.Sprintf("%s – %s",
			tui.FormatTimeShort(data.TimeSpanStart),
			tui.FormatTimeShort(data.TimeSpanEnd))
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			labelStyle.Width(12).Render("Span"),
			labelStyle.Render(span),
		))
	}

	return renderPanel("SUMMARY", b.String(), width, height)
}

func renderCostByAgent(data *StatsData, width, height int) string {
	hasCost := false
	for _, a := range data.Agents {
		if a.Cost > 0 {
			hasCost = true
			break
		}
	}
	if !hasCost {
		return renderPanel("BY AGENT", "  No cost data", width, height)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s %s %s\n",
		labelStyle.Width(14).Render("Agent"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Sess"),
		labelStyle.Width(9).Align(lipgloss.Right).Render("Cost"),
	))

	maxAgents := height - 3
	if maxAgents < 1 {
		maxAgents = 1
	}
	if maxAgents > len(data.Agents) {
		maxAgents = len(data.Agents)
	}

	for _, a := range data.Agents[:maxAgents] {
		if a.Cost == 0 {
			continue
		}
		name := lipgloss.NewStyle().Foreground(agentColor(a.Name)).Render(
			tui.TruncateString(a.Name, 14),
		)
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			tui.PadRightVisible(name, 14),
			valueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", a.Sessions)),
			greenValueStyle.Width(9).Align(lipgloss.Right).Render(tui.FormatCost(a.Cost)),
		))
	}

	return renderPanel("BY AGENT", b.String(), width, height)
}

func renderCostByModel(data *StatsData, width, height int) string {
	if len(data.ModelStats) == 0 {
		return renderPanel("BY MODEL", "  No model data", width, height)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s %s %s\n",
		labelStyle.Width(18).Render("Model"),
		labelStyle.Width(8).Align(lipgloss.Right).Render("Tokens"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Sess"),
	))

	maxModels := height - 3
	if maxModels < 1 {
		maxModels = 1
	}
	if maxModels > len(data.ModelStats) {
		maxModels = len(data.ModelStats)
	}

	for _, ms := range data.ModelStats[:maxModels] {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			valueStyle.Width(18).Render(tui.TruncateString(ms.Name, 18)),
			valueStyle.Width(8).Align(lipgloss.Right).Render(tui.FormatTokens(ms.TotalTokens)),
			labelStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", ms.Sessions)),
		))
	}

	return renderPanel("BY MODEL", b.String(), width, height)
}

func renderCostTokens(data *StatsData, width, height int) string {
	totalInput := data.TotalInputTokens
	totalOutput := data.TotalOutputTokens
	totalCacheR := data.TotalCacheRead
	totalCacheW := data.TotalCacheWrite

	if totalInput == 0 && totalOutput == 0 {
		return renderPanel("TOKENS", "  No token data", width, height)
	}

	total := totalInput + totalOutput + totalCacheR + totalCacheW

	var b strings.Builder
	rows := []struct {
		label string
		value int64
		style lipgloss.Style
	}{
		{"Input", totalInput, valueStyle},
		{"Output", totalOutput, valueStyle},
		{"Cache Read", totalCacheR, labelStyle},
		{"Cache Write", totalCacheW, labelStyle},
	}

	for _, r := range rows {
		pct := ""
		if total > 0 {
			pct = fmt.Sprintf(" %d%%", r.value*100/total)
		}
		b.WriteString(fmt.Sprintf("  %s  %s%s\n",
			labelStyle.Width(12).Render(r.label),
			r.style.Render(tui.FormatTokens(r.value)),
			labelStyle.Render(pct),
		))
	}

	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Total"),
		valueStyle.Bold(true).Render(tui.FormatTokens(total)),
	))

	return renderPanel("TOKENS", b.String(), width, height)
}

func renderCostTrend(data *StatsData, width, height int) string {
	buckets := data.CostBuckets
	if len(buckets) == 0 {
		return renderPanel("COST TREND", "  No data", width, height)
	}

	var b strings.Builder

	chartWidth := width - 6
	if chartWidth < 10 {
		chartWidth = 10
	}
	chartHeight := height - 4
	if chartHeight < 1 {
		chartHeight = 1
	}

	values := costBucketsToScaled(buckets)
	chart := lipgloss.NewStyle().Foreground(colorGreen).Render(
		renderVerticalChart(values, chartWidth, chartHeight),
	)
	b.WriteString("  " + chart + "\n")

	labelBuf := make([]byte, chartWidth)
	for i := range labelBuf {
		labelBuf[i] = ' '
	}
	n := len(buckets)
	step := n / 8
	if step < 1 {
		step = 1
	}
	for i := 0; i < n; i += step {
		pos := (i * chartWidth) / n
		tag := buckets[i].Label
		if pos+len(tag) <= chartWidth {
			copy(labelBuf[pos:], tag)
		}
	}
	b.WriteString("  " + labelStyle.Render(string(labelBuf)) + "\n")

	peak := 0.0
	peakLabel := ""
	for _, bucket := range buckets {
		if bucket.Cost > peak {
			peak = bucket.Cost
			peakLabel = bucket.Label
		}
	}
	if peak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s",
			labelStyle.Render("Peak:"),
			valueStyle.Render(fmt.Sprintf("%s at %s", tui.FormatCost(peak), peakLabel)),
		))
	}

	return renderPanel("COST TREND", b.String(), width, height)
}

func costBucketsToScaled(buckets []CostBucket) []int {
	values := make([]int, len(buckets))
	max := 0.0
	for _, b := range buckets {
		if b.Cost > max {
			max = b.Cost
		}
	}
	if max == 0 {
		return values
	}
	for i, b := range buckets {
		values[i] = int((b.Cost / max) * 1000)
	}
	return values
}

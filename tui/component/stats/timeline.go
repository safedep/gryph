package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderTimeline(data *StatsData, width, height int) string {
	var b strings.Builder

	chartWidth := width - 6
	if chartWidth < 10 {
		chartWidth = 10
	}
	chartHeight := height - 3
	if chartHeight < 1 {
		chartHeight = 1
	}

	buckets := data.TimelineBuckets
	if len(buckets) == 0 {
		return renderPanel("TIMELINE", "  No data", width, height)
	}

	values := make([]int, len(buckets))
	for i, bucket := range buckets {
		values[i] = bucket.Count
	}

	chart := lipgloss.NewStyle().Foreground(colorTeal).Render(
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

	peak := 0
	peakLabel := ""
	for _, bucket := range buckets {
		if bucket.Count > peak {
			peak = bucket.Count
			peakLabel = bucket.Label
		}
	}
	if peak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s",
			labelStyle.Render("Peak:"),
			valueStyle.Render(fmt.Sprintf("%d events at %s", peak, peakLabel)),
		))
	}

	return renderPanel("TIMELINE", b.String(), width, height)
}

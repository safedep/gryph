package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderTimeline(data *StatsData, width, height int) string {
	var b strings.Builder

	// Panel chrome: title line (1) + hour labels (1) + peak line (1) = 3 lines reserved
	chartWidth := width - 6
	if chartWidth < 10 {
		chartWidth = 10
	}
	chartHeight := height - 3
	if chartHeight < 1 {
		chartHeight = 1
	}

	values := data.HourlyBuckets[:]
	chart := lipgloss.NewStyle().Foreground(colorTeal).Render(
		renderVerticalChart(values, chartWidth, chartHeight),
	)
	b.WriteString("  " + chart + "\n")

	// Build hour labels spaced evenly across the chart width
	labelBuf := make([]byte, chartWidth)
	for i := range labelBuf {
		labelBuf[i] = ' '
	}
	for h := 0; h < 24; h += 3 {
		pos := (h * chartWidth) / 24
		tag := fmt.Sprintf("%02d", h)
		if pos+len(tag) <= chartWidth {
			copy(labelBuf[pos:], tag)
		}
	}
	b.WriteString("  " + labelStyle.Render(string(labelBuf)) + "\n")

	peak := 0
	peakHour := 0
	for h, v := range data.HourlyBuckets {
		if v > peak {
			peak = v
			peakHour = h
		}
	}
	if peak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s",
			labelStyle.Render("Peak:"),
			valueStyle.Render(fmt.Sprintf("%d events at %02d:00", peak, peakHour)),
		))
	}

	return renderPanel("TIMELINE", b.String(), width, height)
}

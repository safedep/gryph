package stats

import (
	"fmt"
	"strings"

	"github.com/safedep/gryph/tui"
)

func renderSessions(data *StatsData, width, height int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Avg duration"),
		valueStyle.Render(tui.FormatDuration(data.AvgDuration)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Avg actions"),
		valueStyle.Render(fmt.Sprintf("%.0f", data.AvgActionsPerSess)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Longest"),
		valueStyle.Render(tui.FormatDuration(data.LongestSession)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Shortest"),
		valueStyle.Render(tui.FormatDuration(data.ShortestSession)),
	))
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		labelStyle.Width(12).Render("Peak parallel"),
		valueStyle.Render(fmt.Sprintf("%d", data.PeakConcurrent)),
	))

	return renderPanel("SESSIONS", b.String(), width, height)
}

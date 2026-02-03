package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/tui"
)

func renderActivity(data *StatsData, width, height int) string {
	type row struct {
		label string
		count int
		color lipgloss.Color
	}

	rows := []row{
		{"Reads", data.FileReads, colorBlue},
		{"Writes", data.FileWrites, colorGreen},
		{"Deletes", data.FileDeletes, colorRed},
		{"Execs", data.CommandExecs, colorAmber},
		{"Tools", data.ToolUses, colorIndigo},
		{"Net", data.NetworkRequests, colorViolet},
	}

	total := data.TotalEvents
	barWidth := width - 30
	if barWidth < 5 {
		barWidth = 5
	}

	var b strings.Builder
	for _, r := range rows {
		if r.count == 0 {
			continue
		}
		bar := lipgloss.NewStyle().Foreground(r.color).Render(
			renderBar(r.count, total, barWidth),
		)
		b.WriteString(fmt.Sprintf("  %s %s %s (%s)\n",
			labelStyle.Width(8).Render(r.label),
			bar,
			valueStyle.Render(tui.FormatNumber(r.count)),
			percentage(r.count, total),
		))
	}

	return renderPanel("ACTIVITY", b.String(), width, height)
}

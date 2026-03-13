package stats

import (
	"fmt"
	"strings"

	"github.com/safedep/gryph/tui"
)

func renderOverview(data *StatsData, width, height int) string {
	var b strings.Builder

	pairs := []struct {
		label string
		value string
	}{
		{"Events", tui.FormatNumber(data.TotalEvents)},
		{"Sessions", tui.FormatNumber(data.TotalSessions)},
		{"Active", tui.FormatNumber(data.ActiveSessions)},
		{"Agents", tui.FormatNumber(data.UniqueAgents)},
		{"Projects", tui.FormatNumber(len(data.WorkingDirs))},
	}

	for _, p := range pairs {
		fmt.Fprintf(&b, "  %s  %s\n",
			labelStyle.Width(10).Render(p.label),
			valueStyle.Render(p.value),
		)
	}

	if data.TotalCost > 0 {
		fmt.Fprintf(&b, "  %s  %s\n",
			labelStyle.Width(10).Render("Cost"),
			greenValueStyle.Render(tui.FormatCost(data.TotalCost)),
		)
	}

	if !data.TimeSpanStart.IsZero() {
		span := fmt.Sprintf("%s – %s",
			tui.FormatTimeShort(data.TimeSpanStart),
			tui.FormatTimeShort(data.TimeSpanEnd))
		fmt.Fprintf(&b, "  %s  %s\n",
			labelStyle.Width(10).Render("Span"),
			labelStyle.Render(span),
		)
	}

	return renderPanel("OVERVIEW", b.String(), width, height)
}

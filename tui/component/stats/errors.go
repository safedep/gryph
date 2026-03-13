package stats

import (
	"fmt"
	"strings"
)

func renderErrors(data *StatsData, width, height int) string {
	var b strings.Builder

	errorRate := percentage(data.TotalErrors, data.TotalEvents)
	fmt.Fprintf(&b, "  %s %s\n",
		labelStyle.Render("Errors"),
		redValueStyle.Render(fmt.Sprintf("%d (%s)", data.TotalErrors, errorRate)),
	)
	fmt.Fprintf(&b, "  %s %s  %s %s\n",
		labelStyle.Render("Blocked"),
		amberValueStyle.Render(fmt.Sprintf("%d", data.TotalBlocked)),
		labelStyle.Render("Rejected"),
		amberValueStyle.Render(fmt.Sprintf("%d", data.TotalRejected)),
	)

	return renderPanel("ERRORS", b.String(), width, height)
}

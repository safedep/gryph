package stats

import (
	"fmt"
	"strings"

	"github.com/safedep/gryph/tui"
)

func renderCommands(data *StatsData, width, height int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %s %s  %s %s  %s %s\n",
		labelStyle.Render("Total"),
		valueStyle.Render(tui.FormatNumber(data.TotalCommands)),
		labelStyle.Render("Pass"),
		greenValueStyle.Render(fmt.Sprintf("%d (%s)", data.PassedCommands, percentage(data.PassedCommands, data.TotalCommands))),
		labelStyle.Render("Fail"),
		redValueStyle.Render(fmt.Sprintf("%d (%s)", data.FailedCommands, percentage(data.FailedCommands, data.TotalCommands))),
	))

	if len(data.TopCommands) > 0 {
		b.WriteString("\n")
		maxCmds := 5
		if height > 6 {
			maxCmds = height - 3
		}
		if maxCmds > len(data.TopCommands) {
			maxCmds = len(data.TopCommands)
		}
		cmdWidth := width - 16
		if cmdWidth < 10 {
			cmdWidth = 10
		}
		for i := 0; i < maxCmds; i++ {
			c := data.TopCommands[i]
			cmd := tui.TruncateString(c.Command, cmdWidth)
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				labelStyle.Render(cmd),
				valueStyle.Render(fmt.Sprintf("%d", c.Count)),
			))
		}
	}

	return renderPanel("COMMANDS", b.String(), width, height)
}

package stats

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/tui"
)

func renderAgents(data *StatsData, width, height int) string {
	if len(data.Agents) == 0 {
		return renderPanel("AGENTS", "  No agent data", width, height)
	}

	hasCost := false
	for _, a := range data.Agents {
		if a.Cost > 0 {
			hasCost = true
			break
		}
	}

	var b strings.Builder
	header := fmt.Sprintf("  %s %s %s %s %s %s",
		labelStyle.Width(12).Render("Agent"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Sess"),
		labelStyle.Width(6).Align(lipgloss.Right).Render("Evts"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Wrts"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Cmds"),
		labelStyle.Width(5).Align(lipgloss.Right).Render("Errs"),
	)
	if hasCost {
		header += " " + labelStyle.Width(7).Align(lipgloss.Right).Render("Cost")
	}
	b.WriteString(header + "\n")

	maxAgents := height - 3
	if maxAgents < 1 {
		maxAgents = 1
	}
	if maxAgents > len(data.Agents) {
		maxAgents = len(data.Agents)
	}

	for _, a := range data.Agents[:maxAgents] {
		name := lipgloss.NewStyle().Foreground(agentColor(a.Name)).Render(
			tui.TruncateString(a.Name, 12),
		)
		row := fmt.Sprintf("  %s %s %s %s %s %s",
			tui.PadRightVisible(name, 12),
			valueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", a.Sessions)),
			valueStyle.Width(6).Align(lipgloss.Right).Render(tui.FormatNumber(a.Events)),
			valueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", a.FilesWritten)),
			valueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", a.Commands)),
			redValueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", a.Errors)),
		)
		if hasCost {
			row += " " + greenValueStyle.Width(7).Align(lipgloss.Right).Render(tui.FormatCost(a.Cost))
		}
		b.WriteString(row + "\n")
	}

	return renderPanel("AGENTS", b.String(), width, height)
}

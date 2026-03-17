package query

import "github.com/charmbracelet/lipgloss"

func (m Model) renderDetail(width, height int) string {
	return lipgloss.NewStyle().Width(width).Height(height).Render("Detail pane (not yet implemented)")
}

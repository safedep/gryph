package query

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// highlightSnippet replaces FTS snippet markers >>> and <<< with amber styling.
func highlightSnippet(snippet string) string {
	parts := strings.Split(snippet, ">>>")
	if len(parts) == 1 {
		return snippet
	}
	var sb strings.Builder
	sb.WriteString(parts[0])
	for _, part := range parts[1:] {
		end := strings.Index(part, "<<<")
		if end == -1 {
			sb.WriteString(searchHighlightStyle.Render(part))
			continue
		}
		sb.WriteString(searchHighlightStyle.Render(part[:end]))
		sb.WriteString(part[end+3:])
	}
	return sb.String()
}

// renderSearch is used in narrow mode when search is the only pane.
func (m Model) renderSearch(width, height int) string {
	prompt := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render("/")
	cursor := lipgloss.NewStyle().Background(colorWhite).Foreground(colorDim).Render(" ")
	input := " " + prompt + " " + m.searchInput + cursor

	hint := dimStyle.Render("  type to search, enter to apply, esc to cancel")

	var lines []string
	lines = append(lines, input)
	lines = append(lines, hint)
	lines = append(lines, "")

	if m.activeSearchQuery != "" {
		lines = append(lines, dimStyle.Render("  active: "+m.activeSearchQuery))
	}

	return padLines(lines, width, height)
}

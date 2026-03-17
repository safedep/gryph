package query

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var helpBindings = []struct {
	key  string
	desc string
}{
	{"q / Ctrl+C", "Quit"},
	{"?", "Toggle help"},
	{"tab", "Switch pane focus"},
	{"/", "Search (FTS)"},
	{"f", "Open filter bar"},
	{"", ""},
	{"j / ↓", "Next / scroll down"},
	{"k / ↑", "Previous / scroll up"},
	{"g / G", "Jump to first / last"},
	{"pgup/pgdn", "Page up / down"},
	{"enter", "Select / expand"},
	{"esc", "Back / collapse"},
	{"", ""},
	{"o", "Toggle sort order"},
	{"1-5", "Toggle action filter"},
	{"0", "Clear action filters"},
	{"x", "Export event to file"},
}

func (m Model) renderHelp() string {
	var sb strings.Builder

	keyStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Width(14)

	for _, b := range helpBindings {
		if b.key == "" {
			sb.WriteString("\n")
			continue
		}
		fmt.Fprintf(&sb, "  %s  %s\n", keyStyle.Render(b.key), b.desc)
	}

	overlay := overlayStyle.Render(sb.String())

	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	return lipgloss.Place(m.width, contentHeight, lipgloss.Center, lipgloss.Center, overlay)
}

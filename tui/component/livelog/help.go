package livelog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpModel struct {
	visible bool
}

func newHelpModel() helpModel {
	return helpModel{}
}

func (h *helpModel) toggle() {
	h.visible = !h.visible
}

var helpBindings = []struct {
	key  string
	desc string
}{
	{"q / Ctrl+C", "Quit"},
	{"p / Space", "Toggle pause"},
	{"?", "Toggle help"},
	{"Up / k", "Scroll up"},
	{"Down / j", "Scroll down"},
	{"PgUp/PgDn", "Page scroll"},
	{"G / End", "Jump to bottom"},
	{"g / Home", "Jump to top"},
	{"1-5", "Toggle filter (read/write/exec/tool/net)"},
	{"0", "Clear all filters"},
	{"a", "Cycle agent filter"},
	{"c", "Clear events"},
	{"s", "Toggle sidebar"},
}

func (h helpModel) view(width, height int) string {
	if !h.visible {
		return ""
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	for _, bind := range helpBindings {
		b.WriteString(helpKeyStyle.Render(bind.key))
		b.WriteString(helpDescStyle.Render(bind.desc))
		b.WriteByte('\n')
	}

	overlay := helpOverlayStyle.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlay)
}

package stats

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
	{"?", "Toggle help"},
	{"t", "Time range: Today"},
	{"w", "Time range: 7 days"},
	{"m", "Time range: 30 days"},
	{"a", "Time range: All"},
	{"r", "Force refresh"},
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

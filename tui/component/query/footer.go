package query

import "github.com/charmbracelet/lipgloss"

func (m Model) renderFooter() string {
	bg := lipgloss.Color("#2C3E50")
	sep := lipgloss.NewStyle().Foreground(colorDim).Background(bg).Render(" │ ")
	key := func(k string) string {
		return lipgloss.NewStyle().Foreground(colorWhite).Background(bg).Render(k)
	}

	var hints string
	switch m.focus {
	case paneSessionList:
		hints = key(" j/k") + " nav" + sep + key("enter") + " select" + sep + key("/") + " search" + sep + key("f") + " filter" + sep + key("o") + " sort" + sep + key("?") + " help" + sep + key("q") + " quit"
		if m.activeSearchQuery != "" {
			hints = key(" j/k") + " nav" + sep + key("enter") + " select" + sep + key("/") + " search" + sep + key("esc") + " clear search" + sep + key("?") + " help" + sep + key("q") + " quit"
		}
	case paneDetail:
		hints = key(" j/k") + " nav" + sep + key("enter") + " expand" + sep + key("esc") + " back" + sep + key("tab") + " pane" + sep + key("1-5") + " filter" + sep + key("0") + " clear" + sep + key("q") + " quit"
	case paneSearch:
		hints = key(" type") + " to search" + sep + key("enter") + " apply" + sep + key("esc") + " cancel"
	case paneFilter:
		hints = key(" tab") + " fields" + sep + key("space") + " toggle" + sep + key("enter") + " apply" + sep + key("esc") + " cancel"
	default:
		hints = key(" ?") + " help" + sep + key("q") + " quit"
	}

	if m.loading {
		hints = lipgloss.NewStyle().Foreground(colorAmber).Background(bg).Render(" loading...") + sep + hints
	}

	return footerStyle.Width(m.width).Render(hints)
}

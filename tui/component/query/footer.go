package query

import "github.com/charmbracelet/lipgloss"

func (m Model) renderFooter() string {
	var hints string

	switch m.focus {
	case paneSessionList:
		hints = " j/k navigate  enter select  / search  f filter  o sort  ? help  q quit"
	case paneDetail:
		hints = " j/k navigate  enter expand  tab back  1-5 filter  0 clear  ? help  q quit"
	case paneSearch:
		hints = " type to search  esc cancel  enter select"
	case paneFilter:
		hints = " tab/esc close filter"
	default:
		hints = " ? help  q quit"
	}

	if m.loading {
		hints = " loading..." + hints
	}

	return footerStyle.Width(m.width).Render(
		lipgloss.NewStyle().Width(m.width - 2).Render(hints),
	)
}

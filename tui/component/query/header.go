package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHeader() string {
	left := " gryph query"

	if len(m.filters.agents) > 0 {
		left += " | agent:" + strings.Join(m.filters.agents, ",")
	} else {
		left += " | agent:all"
	}

	left += fmt.Sprintf(" | %d sessions", len(m.sessions))

	if m.filters.timeRange != "" {
		left += " | since:" + m.filters.timeRange
	}

	if m.activeSearchQuery != "" {
		left += " | " + searchHighlightStyle.Render("search:"+m.activeSearchQuery)
	}

	if m.err != nil {
		left += " | " + errorDotStyle.Render("err: "+m.err.Error())
	}

	right := time.Now().Format("15:04:05")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return headerStyle.Width(m.width).Render(line)
}

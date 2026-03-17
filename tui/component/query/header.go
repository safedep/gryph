package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHeader() string {
	left := " gryph query"

	// Agent filter indicator.
	if len(m.filters.agents) > 0 {
		left += " | agent:" + strings.Join(m.filters.agents, ",")
	}

	// Session count.
	left += fmt.Sprintf(" | %d sessions", len(m.sessions))

	// Since range.
	if !m.filters.since.IsZero() {
		since := time.Since(m.filters.since)
		left += fmt.Sprintf(" | since:%s", compactDuration(since))
	}

	// Error indicator.
	if m.err != nil {
		left += " | " + errorDotStyle.Render("error: "+m.err.Error())
	}

	right := time.Now().Format("15:04:05")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return headerStyle.Width(m.width).Render(line)
}

// compactDuration returns e.g. "7d", "2h", "30m" for a given duration.
func compactDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

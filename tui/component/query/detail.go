package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
)

func (m Model) renderDetail(width, height int) string {
	if len(m.sessions) == 0 || m.sessionIdx >= len(m.sessions) {
		return lipgloss.NewStyle().Width(width).Height(height).
			Foreground(colorDim).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No session selected")
	}

	sess := m.sessions[m.sessionIdx]

	header := fmt.Sprintf(" %s · %s · %s",
		sess.AgentName, sess.ProjectName, tui.FormatDuration(sess.Duration()))
	lines := []string{header, ""}

	if m.expanded && m.eventIdx < len(m.filteredEvents()) {
		expanded := formatExpandedEvent(m.filteredEvents()[m.eventIdx], width)
		expandedLines := strings.Split(expanded, "\n")

		start := m.eventScroll
		if start >= len(expandedLines) {
			start = len(expandedLines) - 1
		}
		if start < 0 {
			start = 0
		}
		end := start + height - 3
		if end > len(expandedLines) {
			end = len(expandedLines)
		}

		lines = append(lines, expandedLines[start:end]...)
	} else {
		summary := m.renderSummary(width)
		lines = append(lines, strings.Split(summary, "\n")...)

		sortLabel := "oldest first"
		if m.sortOrder == sortNewestFirst {
			sortLabel = "newest first"
		}
		evtHeader := fmt.Sprintf("\n %s  %s",
			dimStyle.Render(fmt.Sprintf("Events (%d)", len(m.filteredEvents()))),
			dimStyle.Render(sortLabel))
		lines = append(lines, evtHeader)

		sortedEvents := make([]*events.Event, len(m.filteredEvents()))
		copy(sortedEvents, m.filteredEvents())
		if m.sortOrder == sortNewestFirst {
			slices.Reverse(sortedEvents)
		}

		for i, e := range sortedEvents {
			highlighted := i == m.eventIdx
			lines = append(lines, formatEventRow(e, width, highlighted))
		}
	}

	content := strings.Join(lines, "\n")

	contentLines := strings.Count(content, "\n") + 1
	for contentLines < height {
		content += "\n"
		contentLines++
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

func (m Model) filteredEvents() []*events.Event {
	if len(m.detailActionFilters) == 0 {
		return m.events
	}
	var filtered []*events.Event
	for _, e := range m.events {
		if m.detailActionFilters[e.ActionType] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

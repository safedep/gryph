package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
)

func (m Model) renderDetail(width, height int) string {
	if len(m.sessions) == 0 || m.sessionIdx >= len(m.sessions) {
		return padLines([]string{
			"",
			dimStyle.Render("  No session selected"),
		}, width, height)
	}

	if m.events == nil && m.loading {
		return padLines([]string{
			"",
			dimStyle.Render("  Loading events..."),
		}, width, height)
	}

	sess := m.sessions[m.sessionIdx]

	titleStyle := paneTitleStyle
	if m.focus != paneDetail {
		titleStyle = paneTitleDimStyle
	}
	project := sess.ProjectName
	if project == "" {
		project = "-"
	}
	title := titleStyle.Render(fmt.Sprintf("%s · %s · %s",
		sess.AgentName, project, tui.FormatDuration(sess.Duration())))

	if m.expanded && m.eventIdx < len(m.sortedFilteredEvents()) {
		return m.renderExpandedDetail(title, width, height)
	}

	return m.renderSummaryAndEvents(title, width, height)
}

func (m Model) renderExpandedDetail(title string, width, height int) string {
	var out []string
	out = append(out, title)
	out = append(out, dimStyle.Render(strings.Repeat("─", max(0, width))))

	expanded := formatExpandedEvent(m.sortedFilteredEvents()[m.eventIdx], width)
	expandedLines := strings.Split(expanded, "\n")

	available := height - 2
	start := m.eventScroll
	if start > len(expandedLines)-available {
		start = len(expandedLines) - available
	}
	if start < 0 {
		start = 0
	}
	end := start + available
	if end > len(expandedLines) {
		end = len(expandedLines)
	}

	out = append(out, expandedLines[start:end]...)
	return padLines(out, width, height)
}

func (m Model) renderSummaryAndEvents(title string, width, height int) string {
	var out []string
	out = append(out, title)
	out = append(out, "")

	// Summary — capped to avoid pushing events off screen
	s := m.summary
	out = append(out, summaryLabelStyle.Render("  Summary"))
	out = append(out, dimStyle.Render("  "+strings.Repeat("─", max(0, width-4))))
	out = append(out, fmt.Sprintf("   %s  %s written  %s read  %s deleted",
		summaryLabelStyle.Render("Files"),
		summaryValueStyle.Render(fmt.Sprintf("%d", len(s.filesWritten))),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.filesRead)),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.filesDeleted))))

	// Show at most 5 written files in summary
	shown := len(s.filesWritten)
	if shown > 5 {
		shown = 5
	}
	for i := 0; i < shown; i++ {
		f := s.filesWritten[i]
		changes := tui.FormatLineChanges(f.linesAdded, f.linesRemoved)
		path := f.path
		maxPath := width - len(changes) - 12
		if maxPath > 0 && len(path) > maxPath {
			path = "..." + path[len(path)-maxPath+3:]
		}
		out = append(out, fmt.Sprintf("     %s %s  %s",
			addedStyle.Render("+"), path, dimStyle.Render(changes)))
	}
	if len(s.filesWritten) > 5 {
		out = append(out, dimStyle.Render(fmt.Sprintf("     ... and %d more", len(s.filesWritten)-5)))
	}

	out = append(out, "")
	out = append(out, fmt.Sprintf("   %s  %s executed  %s failed",
		summaryLabelStyle.Render("Cmds"),
		summaryValueStyle.Render(fmt.Sprintf("%d", len(s.commands))),
		summaryValueStyle.Render(fmt.Sprintf("%d", s.commandsFailed))))

	// Show at most 5 commands
	shownCmds := len(s.commands)
	if shownCmds > 5 {
		shownCmds = 5
	}
	for i := 0; i < shownCmds; i++ {
		c := s.commands[i]
		exitStyle := dimStyle
		if c.exitCode != 0 {
			exitStyle = redTextStyle
		}
		cmd := c.command
		maxCmd := width - 22
		if maxCmd > 0 && len(cmd) > maxCmd {
			cmd = cmd[:maxCmd-3] + "..."
		}
		out = append(out, fmt.Sprintf("     %s %s  %s",
			amberTextStyle.Render("$"),
			cmd, exitStyle.Render(tui.FormatExitCode(c.exitCode))))
	}
	if len(s.commands) > 5 {
		out = append(out, dimStyle.Render(fmt.Sprintf("     ... and %d more", len(s.commands)-5)))
	}

	out = append(out, "")
	flagLine := fmt.Sprintf("   %s  %d sensitive  %d blocked  %d errors",
		summaryLabelStyle.Render("Flags"), s.sensitive, s.blocked, s.errors)
	if s.errors > 0 {
		flagLine = redTextStyle.Render(flagLine)
	} else if s.sensitive > 0 || s.blocked > 0 {
		flagLine = amberTextStyle.Render(flagLine)
	} else {
		flagLine = dimStyle.Render(flagLine)
	}
	out = append(out, flagLine)

	// Event list separator
	out = append(out, "")
	out = append(out, dimStyle.Render("  "+strings.Repeat("─", max(0, width-4))))

	sortLabel := "oldest first"
	if m.sortOrder == sortNewestFirst {
		sortLabel = "newest first"
	}
	filtered := m.filteredEvents()
	out = append(out, fmt.Sprintf("  %s  %s",
		eventsTitleStyle.Render(fmt.Sprintf("Events (%d)", len(filtered))),
		dimStyle.Render(sortLabel)))

	// How many lines the summary took
	summaryUsed := len(out)
	eventAreaHeight := height - summaryUsed
	if eventAreaHeight < 3 {
		eventAreaHeight = 3
	}

	// Sort and window events
	sortedEvents := make([]*events.Event, len(filtered))
	copy(sortedEvents, filtered)
	if m.sortOrder == sortNewestFirst {
		slices.Reverse(sortedEvents)
	}

	scrollOffset := 0
	if m.eventIdx >= eventAreaHeight {
		scrollOffset = m.eventIdx - eventAreaHeight + 1
	}

	for i := scrollOffset; i < len(sortedEvents) && i < scrollOffset+eventAreaHeight; i++ {
		highlighted := i == m.eventIdx && m.focus == paneDetail
		out = append(out, formatEventRow(sortedEvents[i], width, highlighted))
	}

	return padLines(out, width, height)
}

// padLines joins lines and pads/truncates to exactly `height` lines.
func padLines(lines []string, _, height int) string {
	// Truncate if too many
	if len(lines) > height {
		lines = lines[:height]
	}
	// Pad if too few
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) sortedFilteredEvents() []*events.Event {
	filtered := m.filteredEvents()
	sorted := make([]*events.Event, len(filtered))
	copy(sorted, filtered)
	if m.sortOrder == sortNewestFirst {
		slices.Reverse(sorted)
	}
	return sorted
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

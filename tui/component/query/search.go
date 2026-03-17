package query

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/tui"
)

type debounceTickMsg struct {
	query string
}

func (m Model) renderSearch(width, height int) string {
	var sb strings.Builder

	// Input line: prompt + query text + cursor
	prompt := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render("/")
	cursor := lipgloss.NewStyle().Background(colorWhite).Foreground(colorDim).Render(" ")
	inputLine := prompt + " " + m.searchInput + cursor
	sb.WriteString(lipgloss.NewStyle().Width(width).Render(inputLine))
	sb.WriteString("\n")

	// Status line: match count or hint
	var statusLine string
	switch {
	case len(m.searchInput) == 0:
		statusLine = dimStyle.Render("type to search (FTS)…")
	case len(m.searchInput) == 1:
		statusLine = dimStyle.Render("type at least 2 characters…")
	case len(m.searchResults) == 0:
		statusLine = dimStyle.Render("no results")
	default:
		total := 0
		for _, g := range m.searchResults {
			total += len(g.matches)
		}
		statusLine = dimStyle.Render(fmt.Sprintf("%d match(es) across %d session(s)", total, len(m.searchResults)))
	}
	sb.WriteString(lipgloss.NewStyle().Width(width).Render(statusLine))
	sb.WriteString("\n")

	// Separator
	sb.WriteString(dimStyle.Render(strings.Repeat("─", width)))
	sb.WriteString("\n")

	// Results area: remaining lines
	usedLines := 3 // input + status + separator
	resultsHeight := height - usedLines
	if resultsHeight < 1 {
		resultsHeight = 1
	}

	resultLines := m.buildSearchResultLines(width)

	// Scroll so selected group is always visible.
	// Each group occupies at least 1 line (header) + up to 2 snippet lines + 1 blank = 4 lines max.
	// We use a simple approach: render all and clip.
	visible := resultsHeight
	start := 0
	if len(resultLines) > visible && m.searchIdx >= 0 {
		// Estimate offset: each group ~3 lines on average.
		approxOffset := m.searchIdx * 3
		if approxOffset+visible > len(resultLines) {
			approxOffset = len(resultLines) - visible
		}
		if approxOffset < 0 {
			approxOffset = 0
		}
		start = approxOffset
	}

	end := start + visible
	if end > len(resultLines) {
		end = len(resultLines)
	}

	rendered := 0
	for i := start; i < end; i++ {
		sb.WriteString(lipgloss.NewStyle().Width(width).Render(resultLines[i]))
		sb.WriteString("\n")
		rendered++
	}

	// Pad remaining space
	for rendered < visible {
		sb.WriteString(strings.Repeat(" ", width))
		sb.WriteString("\n")
		rendered++
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(sb.String())
}

func (m Model) buildSearchResultLines(width int) []string {
	if len(m.searchResults) == 0 {
		return nil
	}

	var lines []string
	for i, g := range m.searchResults {
		selected := i == m.searchIdx
		lines = append(lines, m.renderSearchGroupHeader(g, width, selected))

		// Show up to 2 snippet lines per group
		shown := 0
		for _, r := range g.matches {
			if shown >= 2 {
				break
			}
			if r.Snippet == "" {
				continue
			}
			snippet := highlightSnippet(tui.TruncateString(r.Snippet, width-4))
			line := "  " + dimStyle.Render("│") + " " + snippet
			lines = append(lines, line)
			shown++
		}

		// Blank separator between groups
		lines = append(lines, "")
	}
	return lines
}

func (m Model) renderSearchGroupHeader(g sessionSearchGroup, width int, selected bool) string {
	sess := g.session
	dot := attentionDot(sess)

	agent := tui.TruncateString(sess.AgentName, 12)
	project := sess.ProjectName
	if project == "" {
		project = tui.TruncateString(sess.WorkingDirectory, 20)
	}
	project = tui.TruncateString(project, 20)

	matchCount := fmt.Sprintf("%d hit(s)", len(g.matches))
	left := fmt.Sprintf("%s %-12s %-20s", dot, agent, project)
	right := dimStyle.Render(matchCount)

	leftVis := tui.VisibleLen(left)
	rightVis := tui.VisibleLen(matchCount)
	gap := width - leftVis - rightVis - 2
	if gap < 1 {
		gap = 1
	}
	row := left + strings.Repeat(" ", gap) + right

	if selected {
		return selectedStyle.Width(width).Render(row)
	}
	return lipgloss.NewStyle().Width(width).Render(row)
}

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

// findSessionByID looks up a session in m.sessions by ID, returns its index or -1.
func (m Model) findSessionBySearchGroup(g sessionSearchGroup) int {
	id := g.session.ID
	for i, s := range m.sessions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// openSearchResult navigates to the session for the currently selected search result.
func (m Model) openSearchResult() (Model, tea.Cmd) {
	if m.searchIdx < 0 || m.searchIdx >= len(m.searchResults) {
		return m, nil
	}
	g := m.searchResults[m.searchIdx]
	idx := m.findSessionBySearchGroup(g)
	if idx == -1 {
		return m, nil
	}
	m.sessionIdx = idx
	m.focus = paneDetail
	return m, loadEvents(m.store, m.sessions[idx])
}

package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
)

func (m Model) renderSessionList(width, height int) string {
	if len(m.sessions) == 0 {
		placeholder := dimStyle.Width(width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No sessions found")
		return placeholder
	}

	visible := height
	if visible <= 0 {
		visible = 1
	}

	// Adjust scroll so selected item is always visible.
	if m.sessionIdx < m.sessionScroll {
		m.sessionScroll = m.sessionIdx
	}
	if m.sessionIdx >= m.sessionScroll+visible {
		m.sessionScroll = m.sessionIdx - visible + 1
	}

	var rows []string
	end := m.sessionScroll + visible
	if end > len(m.sessions) {
		end = len(m.sessions)
	}
	for i := m.sessionScroll; i < end; i++ {
		selected := i == m.sessionIdx && m.focus == paneSessionList
		rows = append(rows, formatSessionRow(m.sessions[i], width, selected))
	}

	// Pad to fill height.
	for len(rows) < visible {
		rows = append(rows, strings.Repeat(" ", width))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

func formatSessionRow(sess *session.Session, width int, selected bool) string {
	dot := attentionDot(sess)

	agent := tui.TruncateString(sess.AgentName, 12)
	project := sess.ProjectName
	if project == "" {
		project = tui.TruncateString(sess.WorkingDirectory, 20)
	}
	project = tui.TruncateString(project, 20)

	dur := tui.FormatDuration(sess.Duration())
	tokens := tui.FormatTokens(sess.InputTokens + sess.OutputTokens)
	cost := tui.FormatCost(sess.EstimatedCostUSD)
	age := formatAge(sess.StartedAt)

	// Compose the left part: dot + agent + project
	left := fmt.Sprintf("%s %-12s %-20s", dot, agent, project)
	// Right part: age + dur + tokens + cost
	right := fmt.Sprintf("%6s %6s %6s %7s", age, dur, tokens, cost)

	// Pad middle to fill width
	leftVis := tui.VisibleLen(left)
	rightVis := tui.VisibleLen(right)
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

// attentionDot returns a coloured indicator based on session health.
func attentionDot(sess *session.Session) string {
	if sess.BlockedActions > 0 {
		return errorDotStyle.Render("●")
	}
	if sess.Errors > 0 || sess.SensitiveActions > 0 {
		return amberDotStyle.Render("●")
	}
	if sess.IsActive() {
		return lipgloss.NewStyle().Foreground(colorGreen).Render("●")
	}
	return dimStyle.Render("○")
}

// formatAge returns a compact relative age string (e.g. "2h", "3d").
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

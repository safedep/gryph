package query

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
)

func (m Model) renderSessionList(width, height int) string {
	if len(m.sessions) == 0 {
		var lines []string
		msg := "No sessions found"
		if m.activeSearchQuery != "" {
			msg = "No sessions match: " + m.activeSearchQuery
		}
		mid := height / 2
		for i := 0; i < mid; i++ {
			lines = append(lines, "")
		}
		lines = append(lines, dimStyle.Render("  "+msg))
		return padLines(lines, width, height)
	}

	var title string
	if m.focus == paneSearch {
		// Show search input as the title line
		prompt := lipgloss.NewStyle().Foreground(colorAmber).Bold(true).Render("/")
		cursor := lipgloss.NewStyle().Background(colorWhite).Foreground(colorDim).Render(" ")
		title = " " + prompt + " " + m.searchInput + cursor
	} else {
		titleStyle := paneTitleStyle
		if m.focus != paneSessionList {
			titleStyle = paneTitleDimStyle
		}
		label := fmt.Sprintf("Sessions (%d)", len(m.sessions))
		if m.activeSearchQuery != "" {
			label += "  " + dimStyle.Render("search:"+m.activeSearchQuery)
		}
		title = titleStyle.Render(label)
	}

	var out []string
	out = append(out, title)

	// Each session = 2 visible lines + 1 blank
	rowHeight := 3
	visibleRows := (height - 1) / rowHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	scroll := m.sessionScroll
	end := scroll + visibleRows
	if end > len(m.sessions) {
		end = len(m.sessions)
	}

	for i := scroll; i < end; i++ {
		selected := i == m.sessionIdx && m.focus == paneSessionList
		out = append(out, formatSessionRow(m.sessions[i], width, selected))
	}

	return padLines(out, width, height)
}

func formatSessionRow(sess *session.Session, width int, selected bool) string {
	dot := attentionDot(sess)
	date := sess.StartedAt.Local().Format("01/02")
	agent := tui.TruncateString(sess.AgentName, 14)
	project := sess.ProjectName
	if project == "" {
		if sess.WorkingDirectory != "" {
			parts := strings.Split(sess.WorkingDirectory, "/")
			project = parts[len(parts)-1]
		} else {
			project = "-"
		}
	}
	project = tui.TruncateString(project, 18)

	// Line 1: cursor + date + agent + project + dot
	cursor := "  "
	if selected {
		cursor = "> "
	}
	line1 := fmt.Sprintf("%s%s %s %-18s %s", cursor, date, agent, project, dot)

	// Line 2: indented stats
	tokens := tui.FormatTokens(sess.InputTokens + sess.OutputTokens)
	cost := tui.FormatCost(sess.EstimatedCostUSD)
	line2 := fmt.Sprintf("     %d actions · %s tok · %s",
		sess.TotalActions, tokens, cost)

	if selected {
		line1 = selectedStyle.Width(width).Render(line1)
		line2 = selectedStyle.Width(width).Render(line2)
	} else {
		line1 = lipgloss.NewStyle().Width(width).Render(line1)
		line2 = dimStyle.Width(width).Render(line2)
	}

	return line1 + "\n" + line2 + "\n"
}

func attentionDot(sess *session.Session) string {
	if sess.Errors > 0 {
		return errorDotStyle.Render("●")
	}
	if sess.BlockedActions > 0 || sess.SensitiveActions > 0 {
		return amberDotStyle.Render("●")
	}
	if sess.IsActive() {
		return greenDotStyle.Render("●")
	}
	return dimStyle.Render("·")
}

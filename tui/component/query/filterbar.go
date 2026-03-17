package query

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterField int

const (
	fieldAgent filterField = iota
	fieldAction
	fieldStatus
	fieldSince
	fieldFile
	fieldCommand
	fieldCount
)

var allActionTypes = []string{
	"file_read",
	"file_write",
	"file_delete",
	"command_exec",
	"network_request",
	"tool_use",
}

var allStatusTypes = []string{
	"success",
	"error",
	"blocked",
	"rejected",
}

var sincePresets = []string{"today", "yesterday", "7d", "30d", "all"}

type filterBarModel struct {
	activeField filterField
	subIdx      int
	agents      []string
	actions     []string
	statuses    []string
	since       string
	file        string
	command     string
	allAgents   []string
}

func newFilterBar(f FilterState) filterBarModel {
	since := f.timeRange
	if since == "" && !f.since.IsZero() {
		since = "all"
	}
	return filterBarModel{
		agents:    append([]string(nil), f.agents...),
		actions:   append([]string(nil), f.actions...),
		statuses:  append([]string(nil), f.statuses...),
		since:     since,
		file:      f.filePattern,
		command:   f.cmdPattern,
		allAgents: append([]string(nil), f.allAgents...),
	}
}

func (fb filterBarModel) view(width, height int) string {
	var sb strings.Builder

	fieldLabel := func(name string, active bool) string {
		if active {
			return lipgloss.NewStyle().Foreground(colorViolet).Bold(true).Render(name + ":")
		}
		return dimStyle.Render(name + ":")
	}

	renderMulti := func(label string, all []string, selected []string, active bool, subIdx int) string {
		var parts []string
		for i, item := range all {
			cursor := "[ ]"
			if contains(selected, item) {
				cursor = "[x]"
			}
			text := cursor + " " + item
			if active && i == subIdx {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorViolet).Render(text))
			} else if contains(selected, item) {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorGreen).Render(text))
			} else {
				parts = append(parts, dimStyle.Render(text))
			}
		}
		if len(parts) == 0 {
			parts = []string{dimStyle.Render("(none)")}
		}
		return fmt.Sprintf("  %s %s", fieldLabel(label, active), strings.Join(parts, "  "))
	}

	renderText := func(label string, value string, active bool) string {
		display := value
		if display == "" {
			display = dimStyle.Render("(empty)")
		}
		cursor := ""
		if active {
			cursor = lipgloss.NewStyle().Foreground(colorViolet).Render("_")
		}
		return fmt.Sprintf("  %s %s%s", fieldLabel(label, active), display, cursor)
	}

	renderSince := func(active bool) string {
		var parts []string
		for _, p := range sincePresets {
			if fb.since == p {
				parts = append(parts, lipgloss.NewStyle().Foreground(colorGreen).Render("["+p+"]"))
			} else {
				parts = append(parts, dimStyle.Render(p))
			}
		}
		return fmt.Sprintf("  %s %s", fieldLabel("Since", active), strings.Join(parts, "  "))
	}

	title := lipgloss.NewStyle().Foreground(colorViolet).Bold(true).Render("Filters")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	agents := fb.allAgents
	if len(agents) == 0 {
		agents = fb.agents
	}
	sb.WriteString(renderMulti("Agent", agents, fb.agents, fb.activeField == fieldAgent, fb.subIdx))
	sb.WriteString("\n")
	sb.WriteString(renderMulti("Action", allActionTypes, fb.actions, fb.activeField == fieldAction, fb.subIdx))
	sb.WriteString("\n")
	sb.WriteString(renderMulti("Status", allStatusTypes, fb.statuses, fb.activeField == fieldStatus, fb.subIdx))
	sb.WriteString("\n")
	sb.WriteString(renderSince(fb.activeField == fieldSince))
	sb.WriteString("\n")
	sb.WriteString(renderText("File glob", fb.file, fb.activeField == fieldFile))
	sb.WriteString("\n")
	sb.WriteString(renderText("Cmd glob", fb.command, fb.activeField == fieldCommand))
	sb.WriteString("\n\n")

	hints := dimStyle.Render("tab/shift+tab switch field  j/k navigate  space toggle  enter apply  esc cancel")
	if fb.activeField == fieldSince {
		hints = dimStyle.Render("j/k or t/w/m/a select range  enter apply  esc cancel")
	}
	sb.WriteString("  " + hints)

	body := overlayStyle.Render(sb.String())

	bw := lipgloss.Width(body)
	bh := lipgloss.Height(body)

	padLeft := (width - bw) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	padTop := (height - bh) / 2
	if padTop < 0 {
		padTop = 0
	}

	lines := strings.Split(body, "\n")
	prefix := strings.Repeat(" ", padLeft)
	for i, l := range lines {
		lines[i] = prefix + l
	}
	centered := strings.Join(lines, "\n")

	topPad := strings.Repeat("\n", padTop)
	return topPad + centered
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.handleFilterKeyFull(msg)
}

func (m Model) handleFilterKeyFull(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fb := m.filterBar

	switch msg.String() {
	case "esc":
		m.focus = paneSessionList
		m.filterBar = newFilterBar(m.filters)
		return m, nil

	case "enter":
		m.filters.agents = fb.agents
		m.filters.actions = fb.actions
		m.filters.statuses = fb.statuses
		m.filters.timeRange = fb.since
		m.filters.filePattern = fb.file
		m.filters.cmdPattern = fb.command
		m.applyTimeRange()
		m.focus = paneSessionList
		m.loading = true
		return m, loadSessions(m.store, m.filters)

	case "tab":
		fb.activeField = (fb.activeField + 1) % fieldCount
		fb.subIdx = 0
		m.filterBar = fb
		return m, nil

	case "shift+tab":
		fb.activeField = (fb.activeField - 1 + fieldCount) % fieldCount
		fb.subIdx = 0
		m.filterBar = fb
		return m, nil

	case "j", "down":
		switch fb.activeField {
		case fieldAgent:
			if len(fb.allAgents) > 0 {
				fb.subIdx = (fb.subIdx + 1) % len(fb.allAgents)
			}
		case fieldAction:
			fb.subIdx = (fb.subIdx + 1) % len(allActionTypes)
		case fieldStatus:
			fb.subIdx = (fb.subIdx + 1) % len(allStatusTypes)
		case fieldSince:
			fb.subIdx = (fb.subIdx + 1) % len(sincePresets)
			fb.since = sincePresets[fb.subIdx]
		}
		m.filterBar = fb
		return m, nil

	case "k", "up":
		switch fb.activeField {
		case fieldAgent:
			if len(fb.allAgents) > 0 {
				fb.subIdx = (fb.subIdx - 1 + len(fb.allAgents)) % len(fb.allAgents)
			}
		case fieldAction:
			fb.subIdx = (fb.subIdx - 1 + len(allActionTypes)) % len(allActionTypes)
		case fieldStatus:
			fb.subIdx = (fb.subIdx - 1 + len(allStatusTypes)) % len(allStatusTypes)
		case fieldSince:
			fb.subIdx = (fb.subIdx - 1 + len(sincePresets)) % len(sincePresets)
			fb.since = sincePresets[fb.subIdx]
		}
		m.filterBar = fb
		return m, nil

	case " ":
		switch fb.activeField {
		case fieldAgent:
			if len(fb.allAgents) > 0 && fb.subIdx < len(fb.allAgents) {
				fb.agents = toggle(fb.agents, fb.allAgents[fb.subIdx])
			}
		case fieldAction:
			if fb.subIdx < len(allActionTypes) {
				fb.actions = toggle(fb.actions, allActionTypes[fb.subIdx])
			}
		case fieldStatus:
			if fb.subIdx < len(allStatusTypes) {
				fb.statuses = toggle(fb.statuses, allStatusTypes[fb.subIdx])
			}
		}
		m.filterBar = fb
		return m, nil

	case "t":
		if fb.activeField == fieldSince {
			fb.since = "today"
			m.filterBar = fb
			return m, nil
		}

	case "w":
		if fb.activeField == fieldSince {
			fb.since = "7d"
			m.filterBar = fb
			return m, nil
		}

	case "m":
		if fb.activeField == fieldSince {
			fb.since = "30d"
			m.filterBar = fb
			return m, nil
		}

	case "a":
		if fb.activeField == fieldSince {
			fb.since = "all"
			m.filterBar = fb
			return m, nil
		}

	case "backspace":
		switch fb.activeField {
		case fieldFile:
			if len(fb.file) > 0 {
				fb.file = fb.file[:len(fb.file)-1]
			}
		case fieldCommand:
			if len(fb.command) > 0 {
				fb.command = fb.command[:len(fb.command)-1]
			}
		}
		m.filterBar = fb
		return m, nil
	}

	if len(msg.Runes) > 0 {
		switch fb.activeField {
		case fieldFile:
			fb.file += string(msg.Runes)
			m.filterBar = fb
			return m, nil
		case fieldCommand:
			fb.command += string(msg.Runes)
			m.filterBar = fb
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) applyTimeRange() {
	now := time.Now().UTC()
	switch m.filters.timeRange {
	case "today":
		m.filters.since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		m.filters.until = time.Time{}
	case "yesterday":
		y := now.AddDate(0, 0, -1)
		m.filters.since = time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, time.UTC)
		m.filters.until = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case "7d":
		m.filters.since = now.AddDate(0, 0, -7)
		m.filters.until = time.Time{}
	case "30d":
		m.filters.since = now.AddDate(0, 0, -30)
		m.filters.until = time.Time{}
	case "all", "":
		m.filters.since = time.Time{}
		m.filters.until = time.Time{}
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func toggle(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return append(slice, s)
}

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
	"tool_use",
	"network_request",
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

const maxVisibleItems = 4

func (fb filterBarModel) view(width, height int) string {
	activeStyle := lipgloss.NewStyle().Foreground(colorViolet).Bold(true)
	greenStyle := lipgloss.NewStyle().Foreground(colorGreen)

	sectionLabel := func(name string, isActive bool) string {
		s := dimStyle
		if isActive {
			s = activeStyle
		}
		return s.Render(fmt.Sprintf("  %-10s", name))
	}

	renderItem := func(selected bool, item string, isCursor bool) string {
		marker := "○"
		if selected {
			marker = "●"
		}
		text := marker + " " + item
		if isCursor {
			return activeStyle.Render("▸ " + text)
		}
		if selected {
			return greenStyle.Render("  " + text)
		}
		return dimStyle.Render("  " + text)
	}

	renderList := func(items []string, selectedItems []string, isActive bool, cursorIdx int) string {
		if len(items) == 0 {
			return dimStyle.Render("    (none)") + "\n"
		}

		// Compute visible window around cursor
		start := 0
		if isActive && cursorIdx >= maxVisibleItems {
			start = cursorIdx - maxVisibleItems + 1
		}
		end := start + maxVisibleItems
		if end > len(items) {
			end = len(items)
			start = end - maxVisibleItems
			if start < 0 {
				start = 0
			}
		}

		var out string
		if start > 0 {
			out += dimStyle.Render(fmt.Sprintf("    ↑ %d more", start)) + "\n"
		}
		for i := start; i < end; i++ {
			isCursor := isActive && i == cursorIdx
			out += "  " + renderItem(contains(selectedItems, items[i]), items[i], isCursor) + "\n"
		}
		if end < len(items) {
			out += dimStyle.Render(fmt.Sprintf("    ↓ %d more", len(items)-end)) + "\n"
		}
		return out
	}

	var sb strings.Builder

	sb.WriteString("  " + activeStyle.Render("━━ Filters ━━") + "\n\n")

	// Agent
	agents := fb.allAgents
	if len(agents) == 0 {
		agents = fb.agents
	}
	sb.WriteString(sectionLabel("Agent", fb.activeField == fieldAgent) + "\n")
	sb.WriteString(renderList(agents, fb.agents, fb.activeField == fieldAgent, fb.subIdx))

	// Action
	sb.WriteString(sectionLabel("Action", fb.activeField == fieldAction) + "\n")
	sb.WriteString(renderList(allActionTypes, fb.actions, fb.activeField == fieldAction, fb.subIdx))

	// Status
	sb.WriteString(sectionLabel("Status", fb.activeField == fieldStatus) + "\n")
	sb.WriteString(renderList(allStatusTypes, fb.statuses, fb.activeField == fieldStatus, fb.subIdx))

	// Since
	sb.WriteString(sectionLabel("Since", fb.activeField == fieldSince) + "\n")
	for i, p := range sincePresets {
		isCursor := fb.activeField == fieldSince && fb.subIdx == i
		selected := fb.since == p
		marker := "○"
		if selected {
			marker = "●"
		}
		text := marker + " " + p
		if isCursor {
			sb.WriteString("  " + activeStyle.Render("▸ "+text) + "\n")
		} else if selected {
			sb.WriteString("  " + greenStyle.Render("  "+text) + "\n")
		} else {
			sb.WriteString("  " + dimStyle.Render("  "+text) + "\n")
		}
	}

	// File glob
	fileActive := fb.activeField == fieldFile
	sb.WriteString(sectionLabel("File glob", fileActive) + "\n")
	fileVal := fb.file
	if fileVal == "" {
		fileVal = dimStyle.Render("(any)")
	}
	cursor := ""
	if fileActive {
		cursor = activeStyle.Render("█")
	}
	sb.WriteString("    " + fileVal + cursor + "\n")

	// Command glob
	cmdActive := fb.activeField == fieldCommand
	sb.WriteString(sectionLabel("Cmd glob", cmdActive) + "\n")
	cmdVal := fb.command
	if cmdVal == "" {
		cmdVal = dimStyle.Render("(any)")
	}
	cursor = ""
	if cmdActive {
		cursor = activeStyle.Render("█")
	}
	sb.WriteString("    " + cmdVal + cursor + "\n\n")

	// Hints
	sep := dimStyle.Render(" · ")
	sb.WriteString("  " + dimStyle.Render("tab") + " switch" + sep +
		dimStyle.Render("j/k") + " move" + sep +
		dimStyle.Render("space") + " toggle" + sep +
		dimStyle.Render("enter") + " apply" + sep +
		dimStyle.Render("esc") + " cancel")

	overlay := overlayStyle.Render(sb.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlay)
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

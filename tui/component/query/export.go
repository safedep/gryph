package query

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
)

type exportModel struct {
	active   bool
	filename string
	event    *events.Event
	err      string
	success  string
}

func (m Model) renderExportOverlay() string {
	ex := m.export
	activeStyle := lipgloss.NewStyle().Foreground(colorViolet).Bold(true)
	var sb strings.Builder

	sb.WriteString("  " + activeStyle.Render("━━ Export Event ━━") + "\n\n")

	if ex.event != nil {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  Event #%d · %s · %s",
			ex.event.Sequence, ex.event.ActionType, ex.event.ToolName)) + "\n\n")
	}

	sb.WriteString("  " + dimStyle.Render("File:") + " " + ex.filename)
	sb.WriteString(activeStyle.Render("█") + "\n\n")

	if ex.err != "" {
		sb.WriteString("  " + redTextStyle.Render(ex.err) + "\n\n")
	}
	if ex.success != "" {
		sb.WriteString("  " + greenDotStyle.Render(ex.success) + "\n\n")
	}

	sep := dimStyle.Render(" · ")
	sb.WriteString("  " + dimStyle.Render("enter") + " export" + sep + dimStyle.Render("esc") + " cancel")

	overlay := overlayStyle.Render(sb.String())
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	return lipgloss.Place(m.width, contentHeight, lipgloss.Center, lipgloss.Center, overlay)
}

func (m Model) handleExportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.export.active = false
		return m, nil

	case "enter":
		filename := strings.TrimSpace(m.export.filename)
		if filename == "" {
			m.export.err = "filename cannot be empty"
			return m, nil
		}
		if err := exportEventToFile(m.export.event, filename); err != nil {
			m.export.err = err.Error()
			return m, nil
		}
		m.export.success = "exported to " + filename
		m.export.err = ""
		return m, scheduleExportClose()

	case "backspace":
		if len(m.export.filename) > 0 {
			m.export.filename = m.export.filename[:len(m.export.filename)-1]
		}
		m.export.err = ""
		m.export.success = ""
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.export.filename += string(msg.Runes)
			m.export.err = ""
			m.export.success = ""
		}
		return m, nil
	}
}

func exportEventToFile(event *events.Event, filename string) error {
	if event == nil {
		return fmt.Errorf("no event to export")
	}

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := os.WriteFile(filename, data, 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func defaultExportFilename(event *events.Event) string {
	ts := event.Timestamp.Format("20060102-150405")
	return fmt.Sprintf("event-%s-%s.json", ts, event.ActionType)
}

func openExportModal(event *events.Event) exportModel {
	return exportModel{
		active:   true,
		filename: defaultExportFilename(event),
		event:    event,
	}
}

type exportDoneMsg struct{}

func scheduleExportClose() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return exportDoneMsg{}
	})
}

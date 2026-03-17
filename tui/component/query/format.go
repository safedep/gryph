package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
)

var actionSymbols = map[events.ActionType]struct {
	symbol string
	color  lipgloss.Color
}{
	events.ActionFileRead:       {">", colorBlue},
	events.ActionFileWrite:      {"+", colorGreen},
	events.ActionFileDelete:     {"x", colorRed},
	events.ActionCommandExec:    {"$", colorAmber},
	events.ActionNetworkRequest: {"~", colorViolet},
	events.ActionToolUse:        {"*", lipgloss.Color("#6C5CE7")},
	events.ActionSessionStart:   {"[", colorTeal},
	events.ActionSessionEnd:     {"]", colorTeal},
	events.ActionNotification:   {"!", lipgloss.Color("#E67E22")},
	events.ActionUnknown:        {"?", colorDim},
}

func formatEventRow(e *events.Event, width int, highlighted bool) string {
	ts := e.Timestamp.Local().Format("15:04:05")

	as, ok := actionSymbols[e.ActionType]
	if !ok {
		as = actionSymbols[events.ActionUnknown]
	}

	target := eventTarget(e)
	detail := eventDetail(e)

	maxTarget := width - 20
	if maxTarget > 0 && len(target) > maxTarget {
		target = "..." + target[len(target)-maxTarget+3:]
	}

	if highlighted {
		// Build as plain text, apply single background style across entire row
		// to avoid ANSI reset breaks in the highlight bar
		line := fmt.Sprintf("  %s %s %s", ts, as.symbol, target)
		if detail != "" {
			line += "  " + detail
		}
		if e.ResultStatus != events.ResultSuccess {
			line += "  " + string(e.ResultStatus)
		}
		return selectedStyle.Width(width).Render(line)
	}

	symbol := lipgloss.NewStyle().Foreground(as.color).Render(as.symbol)
	line := fmt.Sprintf("  %s %s %s", ts, symbol, target)
	if detail != "" {
		line += "  " + dimStyle.Render(detail)
	}
	if e.ResultStatus != events.ResultSuccess {
		statusStyle := lipgloss.NewStyle().Foreground(colorRed)
		if e.ResultStatus == events.ResultBlocked {
			statusStyle = lipgloss.NewStyle().Foreground(colorAmber)
		}
		line += "  " + statusStyle.Render(string(e.ResultStatus))
	}

	return line
}

func eventTarget(e *events.Event) string {
	switch e.ActionType {
	case events.ActionFileRead:
		if p, err := e.GetFileReadPayload(); err == nil && p != nil {
			return p.DisplayTarget()
		}
	case events.ActionFileWrite:
		if p, err := e.GetFileWritePayload(); err == nil && p != nil {
			return p.Path
		}
	case events.ActionFileDelete:
		if p, err := e.GetFileDeletePayload(); err == nil && p != nil {
			return p.Path
		}
	case events.ActionCommandExec:
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			return p.Command
		}
	case events.ActionToolUse:
		if p, err := e.GetToolUsePayload(); err == nil && p != nil {
			return p.ToolName
		}
	case events.ActionNotification:
		if p, err := e.GetNotificationPayload(); err == nil && p != nil {
			return p.Message
		}
	case events.ActionSessionStart:
		return "started"
	case events.ActionSessionEnd:
		return "ended"
	}
	if e.ToolName != "" {
		return e.ToolName
	}
	return ""
}

func eventDetail(e *events.Event) string {
	switch e.ActionType {
	case events.ActionFileWrite:
		if p, err := e.GetFileWritePayload(); err == nil && p != nil {
			return tui.FormatLineChanges(p.LinesAdded, p.LinesRemoved)
		}
	case events.ActionCommandExec:
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			return tui.FormatExitCode(p.ExitCode)
		}
	}
	return ""
}

func formatExpandedEvent(e *events.Event, width int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(" Event #%d · %s · %s\n",
		e.Sequence, e.ActionType, e.ResultStatus))
	sb.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")

	sb.WriteString(fmt.Sprintf(" Time:   %s\n", e.Timestamp.Local().Format("01/02 15:04:05")))
	if e.DurationMs > 0 {
		sb.WriteString(fmt.Sprintf(" Dur:    %s\n", tui.FormatDuration(time.Duration(e.DurationMs)*time.Millisecond)))
	}
	if e.ToolName != "" {
		sb.WriteString(fmt.Sprintf(" Tool:   %s\n", e.ToolName))
	}

	target := eventTarget(e)
	if target != "" {
		sb.WriteString(fmt.Sprintf(" Target: %s\n", target))
	}

	detail := eventDetail(e)
	if detail != "" {
		sb.WriteString(fmt.Sprintf(" Detail: %s\n", detail))
	}

	if e.ErrorMessage != "" {
		sb.WriteString(fmt.Sprintf(" Error:  %s\n",
			lipgloss.NewStyle().Foreground(colorRed).Render(e.ErrorMessage)))
	}

	if e.DiffContent != "" {
		sb.WriteString("\n" + dimStyle.Render(" Diff:") + "\n")
		sb.WriteString(dimStyle.Render(strings.Repeat("─", width)) + "\n")
		for _, line := range strings.Split(e.DiffContent, "\n") {
			if strings.HasPrefix(line, "+") {
				sb.WriteString(addedStyle.Render(" "+line) + "\n")
			} else if strings.HasPrefix(line, "-") {
				sb.WriteString(removedStyle.Render(" "+line) + "\n")
			} else {
				sb.WriteString(" " + line + "\n")
			}
		}
	}

	if e.ActionType == events.ActionCommandExec {
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			if p.StdoutPreview != "" {
				sb.WriteString("\n" + dimStyle.Render(" stdout:") + "\n")
				sb.WriteString(" " + p.StdoutPreview + "\n")
			}
			if p.StderrPreview != "" {
				sb.WriteString("\n" + dimStyle.Render(" stderr:") + "\n")
				sb.WriteString(" " + p.StderrPreview + "\n")
			}
		}
	}

	return sb.String()
}

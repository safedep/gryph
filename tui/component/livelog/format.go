package livelog

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
)

const (
	colTimeWidth    = 8  // "15:04:05"
	colIconWidth    = 1  // single symbol
	colAgentWidth   = 12 // "claude-code " padded
	colSessWidth    = 4  // short session ID
	colActionWidth  = 6  // "write " padded
	colSpacing      = 6  // spaces between fixed columns
	colMinTarget    = 15
	colMaxTarget    = 120
)

func fixedColumnsWidth() int {
	return colTimeWidth + colIconWidth + colAgentWidth + colSessWidth + colActionWidth + colSpacing
}

func targetWidth(streamWidth int) int {
	avail := streamWidth - fixedColumnsWidth()
	if avail < colMinTarget {
		return colMinTarget
	}
	if avail > colMaxTarget {
		return colMaxTarget
	}
	return avail
}

func formatEvent(e *events.Event, width int) string {
	as := actionStyleFor(e.ActionType)
	iconStyle := lipgloss.NewStyle().Foreground(as.color)

	tw := targetWidth(width)

	ts := eventTimeStyle.Render(tui.FormatTimeShort(e.Timestamp))
	icon := iconStyle.Render(as.symbol)
	agent := agentBadge(e.AgentName)
	sess := lipgloss.NewStyle().Foreground(colorDim).Render(tui.FormatShortID(e.SessionID.String())[:4])
	action := iconStyle.Render(fmt.Sprintf("%-6s", actionShort(e.ActionType)))

	target, detail := extractTargetDetail(e, tw)

	if e.IsSensitive {
		target = lipgloss.NewStyle().Foreground(colorAmber).Render("[sensitive]")
		detail = ""
	}

	var statusSuffix string
	if e.ResultStatus != events.ResultSuccess {
		statusSuffix = " " + statusStyleFor(e.ResultStatus).Render(string(e.ResultStatus))
	}

	return fmt.Sprintf("%s %s %-14s %s %s %s %s%s",
		ts, icon, agent, sess, action, target, detail, statusSuffix)
}

func extractTargetDetail(e *events.Event, maxTarget int) (string, string) {
	switch e.ActionType {
	case events.ActionFileRead:
		if p, err := e.GetFileReadPayload(); err == nil && p != nil {
			return truncatePath(p.Path, maxTarget), ""
		}
	case events.ActionFileWrite:
		if p, err := e.GetFileWritePayload(); err == nil && p != nil {
			changes := tui.FormatLineChanges(p.LinesAdded, p.LinesRemoved)
			detail := ""
			if changes != "" {
				detail = lipgloss.NewStyle().Foreground(colorDim).Render(changes)
			}
			return truncatePath(p.Path, maxTarget), detail
		}
	case events.ActionFileDelete:
		var payload events.FileDeletePayload
		if e.Payload != nil {
			if err := decodePayload(e.Payload, &payload); err == nil {
				return truncatePath(payload.Path, maxTarget), ""
			}
		}
	case events.ActionCommandExec:
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			detail := lipgloss.NewStyle().Foreground(colorDim).Render(tui.FormatExitCode(p.ExitCode))
			return tui.TruncateString(p.Command, maxTarget), detail
		}
	case events.ActionToolUse:
		return tui.TruncateString(e.ToolName, maxTarget), ""
	case events.ActionNotification:
		var payload events.NotificationPayload
		if e.Payload != nil {
			if err := decodePayload(e.Payload, &payload); err == nil {
				return tui.TruncateString(payload.Message, maxTarget), ""
			}
		}
	case events.ActionSessionStart:
		return "started", ""
	case events.ActionSessionEnd:
		return "ended", ""
	}
	return tui.TruncateString(e.ToolName, maxTarget), ""
}

func decodePayload(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func truncatePath(p string, maxWidth int) string {
	if len(p) <= maxWidth {
		return p
	}
	base := filepath.Base(p)
	if len(base) <= maxWidth {
		return base
	}
	return tui.TruncateString(base, maxWidth)
}

func actionShort(a events.ActionType) string {
	switch a {
	case events.ActionFileRead:
		return "read"
	case events.ActionFileWrite:
		return "write"
	case events.ActionFileDelete:
		return "delete"
	case events.ActionCommandExec:
		return "exec"
	case events.ActionNetworkRequest:
		return "http"
	case events.ActionToolUse:
		return "tool"
	case events.ActionSessionStart:
		return "start"
	case events.ActionSessionEnd:
		return "end"
	case events.ActionNotification:
		return "notice"
	default:
		return "?"
	}
}

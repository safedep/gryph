package livelog

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
)

var (
	colorBlue    = lipgloss.Color("#5B9BD5")
	colorGreen   = lipgloss.Color("#6BCB77")
	colorRed     = lipgloss.Color("#E74C3C")
	colorAmber   = lipgloss.Color("#F0AD4E")
	colorViolet  = lipgloss.Color("#9B59B6")
	colorIndigo  = lipgloss.Color("#6C5CE7")
	colorTeal    = lipgloss.Color("#1ABC9C")
	colorOrange  = lipgloss.Color("#E67E22")
	colorPink    = lipgloss.Color("#E91E63")
	colorWhite   = lipgloss.Color("#ECF0F1")
	colorDim     = lipgloss.Color("#7F8C8D")
	colorBg      = lipgloss.Color("#1E1E2E")
	colorBgLight = lipgloss.Color("#2E2E3E")

	headerStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorWhite).
			Bold(true).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorDim).
			Padding(0, 1)

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorDim).
			Padding(0, 1)

	sidebarLabelStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	sidebarValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true)

	sidebarHeaderStyle = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true).
				Underline(true)

	eventTimeStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	pauseIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorAmber).
				Bold(true)

	scrollLockStyle = lipgloss.NewStyle().
			Foreground(colorViolet).
			Bold(true)

	helpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorViolet).
				Padding(1, 2).
				Foreground(colorWhite)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true).
			Width(12)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorWhite)
)

type actionStyle struct {
	symbol string
	color  lipgloss.Color
}

var actionStyles = map[events.ActionType]actionStyle{
	events.ActionFileRead:        {symbol: ">", color: colorBlue},
	events.ActionFileWrite:       {symbol: "+", color: colorGreen},
	events.ActionFileDelete:      {symbol: "x", color: colorRed},
	events.ActionCommandExec:     {symbol: "$", color: colorAmber},
	events.ActionNetworkRequest:  {symbol: "~", color: colorViolet},
	events.ActionToolUse:         {symbol: "*", color: colorIndigo},
	events.ActionSessionStart:    {symbol: "[", color: colorTeal},
	events.ActionSessionEnd:      {symbol: "]", color: colorTeal},
	events.ActionNotification:    {symbol: "!", color: colorOrange},
	events.ActionUnknown:         {symbol: "?", color: colorDim},
}

func actionStyleFor(action events.ActionType) actionStyle {
	if s, ok := actionStyles[action]; ok {
		return s
	}
	return actionStyle{symbol: "?", color: colorDim}
}

func statusStyleFor(status events.ResultStatus) lipgloss.Style {
	switch status {
	case events.ResultSuccess:
		return lipgloss.NewStyle().Foreground(colorGreen)
	case events.ResultError:
		return lipgloss.NewStyle().Foreground(colorRed)
	case events.ResultBlocked:
		return lipgloss.NewStyle().Foreground(colorAmber)
	case events.ResultRejected:
		return lipgloss.NewStyle().Foreground(colorPink)
	default:
		return lipgloss.NewStyle().Foreground(colorDim)
	}
}

func agentBadge(agentName string) string {
	switch agentName {
	case "claude-code":
		return lipgloss.NewStyle().Foreground(colorOrange).Bold(true).Render("claude-code")
	case "cursor":
		return lipgloss.NewStyle().Foreground(colorViolet).Bold(true).Render("cursor")
	case "gemini":
		return lipgloss.NewStyle().Foreground(colorBlue).Bold(true).Render("gemini")
	default:
		return lipgloss.NewStyle().Foreground(colorDim).Render(agentName)
	}
}

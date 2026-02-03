package stats

import "github.com/charmbracelet/lipgloss"

var (
	colorBlue   = lipgloss.Color("#5B9BD5")
	colorGreen  = lipgloss.Color("#6BCB77")
	colorRed    = lipgloss.Color("#E74C3C")
	colorAmber  = lipgloss.Color("#F0AD4E")
	colorViolet = lipgloss.Color("#9B59B6")
	colorIndigo = lipgloss.Color("#6C5CE7")
	colorTeal   = lipgloss.Color("#1ABC9C")
	colorOrange = lipgloss.Color("#E67E22")
	colorPink   = lipgloss.Color("#E91E63")
	colorWhite  = lipgloss.Color("#ECF0F1")
	colorDim    = lipgloss.Color("#7F8C8D")
	colorBg     = lipgloss.Color("#1E1E2E")

	titleStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorWhite).
			Bold(true).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorDim).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true).
			Underline(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true)

	greenValueStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	redValueStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	amberValueStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true)

	helpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorViolet).
				Padding(1, 2).
				Foreground(colorWhite)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorAmber).
			Bold(true).
			Width(14)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorWhite)
)

func agentColor(name string) lipgloss.Color {
	switch name {
	case "claude-code":
		return colorOrange
	case "cursor":
		return colorViolet
	case "gemini":
		return colorBlue
	case "opencode":
		return colorTeal
	case "openclaw":
		return colorPink
	default:
		return colorDim
	}
}

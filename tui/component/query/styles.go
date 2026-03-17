package query

import "github.com/charmbracelet/lipgloss"

var (
	colorBlue   = lipgloss.Color("#5B9BD5")
	colorGreen  = lipgloss.Color("#6BCB77")
	colorRed    = lipgloss.Color("#E74C3C")
	colorAmber  = lipgloss.Color("#F0AD4E")
	colorViolet = lipgloss.Color("#9B59B6")
	colorTeal   = lipgloss.Color("#1ABC9C")
	colorWhite  = lipgloss.Color("#ECF0F1")
	colorDim    = lipgloss.Color("#7F8C8D")

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2C3E50")).
			Foreground(colorWhite).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2C3E50")).
			Foreground(colorDim).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#34495E")).
			Foreground(colorWhite)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorDotStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	amberDotStyle = lipgloss.NewStyle().
			Foreground(colorAmber)

	summaryLabelStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	summaryValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	addedStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	removedStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(colorDim)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorViolet).
			Padding(1, 2)

	searchHighlightStyle = lipgloss.NewStyle().
				Foreground(colorAmber).
				Bold(true)
)

package stats

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/storage"
)

const refreshInterval = 30 * time.Second

type Model struct {
	opts   Options
	width  int
	height int

	header headerModel
	footer footerModel
	help   helpModel

	data      *StatsData
	timeRange TimeRange
	ready     bool
}

func New(opts Options) Model {
	return Model{
		opts:      opts,
		header:    newHeaderModel(opts.TimeRange, opts.AgentFilter),
		footer:    newFooterModel(),
		help:      newHelpModel(),
		timeRange: opts.TimeRange,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadStats(m.opts.Store, m.timeRange, m.opts.AgentFilter),
		scheduleRefresh(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case statsLoadedMsg:
		m.data = msg.data
		m.header.lastRefresh = time.Now()
		m.footer.lastError = ""
		return m, nil

	case statsErrorMsg:
		m.footer.lastError = msg.err.Error()
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			loadStats(m.opts.Store, m.timeRange, m.opts.AgentFilter),
			scheduleRefresh(),
		)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.help.toggle()
		return m, nil

	case "t":
		return m.setTimeRange(RangeToday)
	case "w":
		return m.setTimeRange(Range7Days)
	case "m":
		return m.setTimeRange(Range30Days)
	case "a":
		return m.setTimeRange(RangeAll)

	case "r":
		return m, loadStats(m.opts.Store, m.timeRange, m.opts.AgentFilter)
	}

	return m, nil
}

func (m *Model) setTimeRange(r TimeRange) (tea.Model, tea.Cmd) {
	m.timeRange = r
	m.header.timeRange = r
	return m, loadStats(m.opts.Store, m.timeRange, m.opts.AgentFilter)
}

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}

	header := m.header.view(m.width)
	footer := m.footer.view(m.width)
	contentHeight := m.height - 2

	if m.help.visible {
		helpOverlay := m.help.view(m.width, contentHeight)
		return lipgloss.JoinVertical(lipgloss.Left, header, helpOverlay, footer)
	}

	if m.data == nil {
		placeholder := lipgloss.Place(m.width, contentHeight, lipgloss.Center, lipgloss.Center, "Loading stats...")
		return lipgloss.JoinVertical(lipgloss.Left, header, placeholder, footer)
	}

	var content string
	if m.width >= 80 {
		content = m.twoColumnLayout(contentHeight)
	} else {
		content = m.singleColumnLayout(contentHeight)
	}

	content = lipgloss.NewStyle().Width(m.width).Height(contentHeight).MaxHeight(contentHeight).Render(content)

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m Model) twoColumnLayout(height int) string {
	half := m.width / 2
	panelH := (height - 2) / 4
	if panelH < 4 {
		panelH = 4
	}

	row1 := twoColumnGrid(
		renderOverview(m.data, half, panelH),
		renderActivity(m.data, m.width-half, panelH),
		m.width,
	)
	row2 := twoColumnGrid(
		renderAgents(m.data, half, panelH),
		renderTimeline(m.data, m.width-half, panelH),
		m.width,
	)
	row3 := twoColumnGrid(
		renderChanges(m.data, half, panelH),
		renderCommands(m.data, m.width-half, panelH),
		m.width,
	)
	row4 := twoColumnGrid(
		renderErrors(m.data, half, panelH),
		renderSessions(m.data, m.width-half, panelH),
		m.width,
	)

	return singleColumnStack(row1, row2, row3, row4)
}

func (m Model) singleColumnLayout(height int) string {
	w := m.width
	panelH := 6

	return singleColumnStack(
		renderOverview(m.data, w, panelH),
		renderActivity(m.data, w, panelH),
		renderAgents(m.data, w, panelH),
		renderTimeline(m.data, w, panelH),
		renderChanges(m.data, w, panelH),
		renderCommands(m.data, w, panelH),
		renderErrors(m.data, w, panelH),
		renderSessions(m.data, w, panelH),
	)
}

func loadStats(store storage.Store, timeRange TimeRange, agentFilter string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		data, err := computeStats(ctx, store, timeRange, agentFilter)
		if err != nil {
			return statsErrorMsg{err: err}
		}
		return statsLoadedMsg{data: data}
	}
}

func scheduleRefresh() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

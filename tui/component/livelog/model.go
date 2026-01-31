package livelog

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/gryph/core/events"
)

var agentCycle = []string{"", "claude-code", "cursor", "gemini"}

type Model struct {
	opts   Options
	width  int
	height int

	header    headerModel
	footer    footerModel
	stats     statsModel
	eventList eventListModel
	help      helpModel

	showSidebar bool
	paused      bool
	agentFilter string
	lastPollAt  time.Time
	ready       bool
}

func New(opts Options) Model {
	return Model{
		opts:        opts,
		header:      newHeaderModel(opts.AgentFilter),
		footer:      newFooterModel(),
		stats:       newStatsModel(),
		eventList:   newEventListModel(),
		help:        newHelpModel(),
		showSidebar: true,
		agentFilter: opts.AgentFilter,
	}
}

func (m Model) Init() tea.Cmd {
	since := m.opts.Since
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	return tea.Batch(
		loadInitialEvents(m.opts.Store, since, m.agentFilter, m.opts.initialLimit()),
		schedulePoll(m.opts.pollInterval()),
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

	case newEventsMsg:
		if len(msg.events) > 0 {
			for _, e := range msg.events {
				m.stats.record(e)
			}
			m.eventList.append(msg.events, m.streamWidth())
			m.updateSessionCount()
			newest := msg.events[len(msg.events)-1]
			m.lastPollAt = newest.Timestamp
		}
		if m.lastPollAt.IsZero() {
			m.lastPollAt = time.Now()
		}
		return m, nil

	case tickMsg:
		if m.paused {
			return m, schedulePoll(m.opts.pollInterval())
		}
		return m, tea.Batch(
			pollEvents(m.opts.Store, m.lastPollAt, m.agentFilter, 100),
			schedulePoll(m.opts.pollInterval()),
		)

	case pollErrorMsg:
		m.footer.lastError = msg.err.Error()
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "p", " ":
		m.paused = !m.paused
		m.footer.paused = m.paused
		return m, nil

	case "?":
		m.help.toggle()
		return m, nil

	case "up", "k":
		m.eventList.scrollUp(1)
		m.footer.scrollLock = !m.eventList.autoScroll
		return m, nil

	case "down", "j":
		m.eventList.scrollDown(1, m.streamHeight())
		m.footer.scrollLock = !m.eventList.autoScroll
		return m, nil

	case "pgup":
		m.eventList.scrollUp(m.streamHeight())
		m.footer.scrollLock = !m.eventList.autoScroll
		return m, nil

	case "pgdown":
		m.eventList.scrollDown(m.streamHeight(), m.streamHeight())
		m.footer.scrollLock = !m.eventList.autoScroll
		return m, nil

	case "G", "end":
		m.eventList.jumpToBottom(m.streamHeight())
		m.footer.scrollLock = false
		return m, nil

	case "g", "home":
		m.eventList.jumpToTop()
		m.footer.scrollLock = true
		return m, nil

	case "1":
		m.eventList.toggleFilter(events.ActionFileRead)
		return m, nil
	case "2":
		m.eventList.toggleFilter(events.ActionFileWrite)
		return m, nil
	case "3":
		m.eventList.toggleFilter(events.ActionCommandExec)
		return m, nil
	case "4":
		m.eventList.toggleFilter(events.ActionToolUse)
		return m, nil
	case "5":
		m.eventList.toggleFilter(events.ActionNetworkRequest)
		return m, nil
	case "0":
		m.eventList.clearFilters()
		return m, nil

	case "a":
		m.cycleAgentFilter()
		m.header.agentFilter = m.agentFilter
		return m, nil

	case "c":
		m.eventList.clear()
		return m, nil

	case "s":
		m.showSidebar = !m.showSidebar
		return m, nil
	}

	return m, nil
}

func (m *Model) cycleAgentFilter() {
	current := m.agentFilter
	for i, a := range agentCycle {
		if a == current {
			m.agentFilter = agentCycle[(i+1)%len(agentCycle)]
			return
		}
	}
	m.agentFilter = ""
}

func (m *Model) updateSessionCount() {
	m.header.sessionCount = len(m.stats.agents)
}

func (m Model) sidebarVisible() bool {
	return m.showSidebar && m.width >= 90
}

func (m Model) streamWidth() int {
	if m.sidebarVisible() {
		return m.width - sidebarWidth
	}
	return m.width
}

func (m Model) streamHeight() int {
	return m.height - 2 // header + footer
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

	streamW := m.streamWidth()
	stream := lipgloss.NewStyle().Width(streamW).Render(
		m.eventList.view(streamW, contentHeight),
	)

	var content string
	if m.sidebarVisible() {
		sidebar := m.stats.view(contentHeight)
		content = lipgloss.JoinHorizontal(lipgloss.Top, stream, sidebar)
	} else {
		content = stream
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

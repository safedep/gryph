package query

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
)

type pane int

const (
	paneSessionList pane = iota
	paneDetail
	paneSearch
	paneFilter
)

type sortOrder int

const (
	sortOldestFirst sortOrder = iota
	sortNewestFirst
)

type Model struct {
	store    storage.Store
	searcher storage.Searcher
	width    int
	height   int
	ready    bool
	focus    pane
	showHelp bool

	// All sessions from DB (before search filter)
	allSessions []*session.Session
	// Visible sessions (filtered by active search)
	sessions      []*session.Session
	sessionIdx    int
	sessionScroll int

	// All events for selected session
	allEvents []*events.Event
	// Visible events (filtered by active search)
	events  []*events.Event
	summary sessionSummary

	eventIdx    int
	eventScroll int
	expanded    bool

	sortOrder sortOrder
	filters   FilterState

	// Search-as-filter state
	searchInput          string
	activeSearchQuery    string
	activeSearchSessionIDs map[uuid.UUID]bool
	activeSearchEventIDs   map[uuid.UUID]bool

	filterBar filterBarModel

	initialSession      string
	detailActionFilters map[events.ActionType]bool

	export exportModel

	loading bool
	err     error
}

type FilterState struct {
	agents      []string
	allAgents   []string
	timeRange   string
	since       time.Time
	until       time.Time
	actions     []string
	statuses    []string
	filePattern string
	cmdPattern  string
	sensitive   bool
}

func New(opts Options) Model {
	since := opts.Since
	timeRange := "7d"
	if since.IsZero() {
		since = time.Now().UTC().Add(-7 * 24 * time.Hour)
	} else {
		d := time.Since(since)
		switch {
		case d < 25*time.Hour:
			timeRange = "today"
		case d < 49*time.Hour:
			timeRange = "yesterday"
		case d < 8*24*time.Hour:
			timeRange = "7d"
		case d < 31*24*time.Hour:
			timeRange = "30d"
		default:
			timeRange = "all"
		}
	}

	f := FilterState{
		agents:      opts.Agents,
		actions:     opts.Actions,
		statuses:    opts.Statuses,
		since:       since,
		until:       opts.Until,
		timeRange:   timeRange,
		filePattern: opts.FilePattern,
		cmdPattern:  opts.CmdPattern,
		sensitive:   opts.Sensitive,
	}
	return Model{
		store:               opts.Store,
		searcher:            opts.Searcher,
		filters:             f,
		filterBar:           newFilterBar(f),
		initialSession:      opts.Session,
		loading:             true,
		detailActionFilters: make(map[events.ActionType]bool),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		loadSessions(m.store, m.filters),
		loadAgents(m.searcher),
	}
	if m.searcher != nil && m.searcher.HasSearch() {
		cmds = append(cmds, backfillFTS(m.store, m.searcher))
	}
	return tea.Batch(cmds...)
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

	case sessionsLoadedMsg:
		m.allSessions = msg.sessions
		m.applySearchFilter()
		m.loading = false
		m.err = nil
		m.sessionIdx = 0
		m.sessionScroll = 0
		if m.initialSession != "" {
			for i, s := range m.sessions {
				if strings.HasPrefix(s.ID.String(), m.initialSession) ||
					strings.HasPrefix(s.AgentSessionID, m.initialSession) {
					m.sessionIdx = i
					break
				}
			}
			m.initialSession = "" // only use once
		}
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
		return m, nil

	case eventsLoadedMsg:
		m.allEvents = msg.events
		m.applyEventSearchFilter()
		m.eventIdx = 0
		m.eventScroll = 0
		return m, nil

	case agentsLoadedMsg:
		m.filters.allAgents = msg.agents
		return m, nil

	case searchAppliedMsg:
		m.activeSearchQuery = msg.query
		m.activeSearchSessionIDs = msg.sessionIDs
		m.activeSearchEventIDs = msg.eventIDs
		m.applySearchFilter()
		m.sessionIdx = 0
		m.sessionScroll = 0
		m.focus = paneSessionList
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[0])
		}
		m.allEvents = nil
		m.events = nil
		m.summary = sessionSummary{}
		return m, nil

	case backfillDoneMsg, backfillErrorMsg:
		return m, nil

	case exportDoneMsg:
		m.export.active = false
		return m, nil

	case loadErrorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case searchErrorMsg:
		m.err = msg.err
		m.focus = paneSessionList
		return m, nil
	}

	return m, nil
}

// applySearchFilter updates m.sessions from m.allSessions based on active search.
func (m *Model) applySearchFilter() {
	if m.activeSearchQuery == "" {
		m.sessions = m.allSessions
		return
	}
	var filtered []*session.Session
	for _, s := range m.allSessions {
		if m.activeSearchSessionIDs[s.ID] {
			filtered = append(filtered, s)
		}
	}
	m.sessions = filtered
}

// applyEventSearchFilter updates m.events from m.allEvents based on active search.
func (m *Model) applyEventSearchFilter() {
	if m.activeSearchQuery == "" {
		m.events = m.allEvents
	} else {
		var filtered []*events.Event
		for _, e := range m.allEvents {
			if m.activeSearchEventIDs[e.ID] {
				filtered = append(filtered, e)
			}
		}
		m.events = filtered
	}
	m.summary = computeSummary(m.events)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
			m.showHelp = false
		}
		return m, nil
	}

	// Export modal
	if m.export.active {
		return m.handleExportKey(msg)
	}

	// Search input mode
	if m.focus == paneSearch {
		return m.handleSearchKey(msg)
	}

	// Filter bar mode
	if m.focus == paneFilter {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.showHelp = true
		return m, nil

	case "tab":
		if m.focus == paneSessionList {
			m.focus = paneDetail
		} else {
			m.focus = paneSessionList
		}
		return m, nil

	case "/":
		if m.searcher != nil && m.searcher.HasSearch() {
			m.focus = paneSearch
			m.searchInput = m.activeSearchQuery // pre-fill with current search
		}
		return m, nil

	case "f":
		m.focus = paneFilter
		m.filterBar = newFilterBar(m.filters)
		return m, nil

	case "esc":
		if m.expanded {
			m.expanded = false
			m.eventScroll = 0
		} else if m.activeSearchQuery != "" && m.focus == paneSessionList {
			// Clear active search
			m.activeSearchQuery = ""
			m.activeSearchSessionIDs = nil
			m.activeSearchEventIDs = nil
			m.applySearchFilter()
			m.sessionIdx = 0
			m.sessionScroll = 0
			if len(m.sessions) > 0 {
				return m, loadEvents(m.store, m.sessions[0])
			}
		} else if m.focus == paneDetail {
			m.focus = paneSessionList
		}
		return m, nil

	case "o":
		if m.sortOrder == sortNewestFirst {
			m.sortOrder = sortOldestFirst
		} else {
			m.sortOrder = sortNewestFirst
		}
		return m, nil

	case "j", "down":
		return m.handleNavDown()
	case "k", "up":
		return m.handleNavUp()
	case "g", "home":
		return m.handleNavTop()
	case "G", "end":
		return m.handleNavBottom()
	case "pgdown":
		return m.handleNavPage(1)
	case "pgup":
		return m.handleNavPage(-1)

	case "enter":
		if m.focus == paneSessionList && len(m.sessions) > 0 {
			m.focus = paneDetail
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
		if m.focus == paneDetail && len(m.filteredEvents()) > 0 {
			m.expanded = !m.expanded
			m.eventScroll = 0
		}
		return m, nil

	case "1":
		m.toggleActionFilter(events.ActionFileRead)
		return m, nil
	case "2":
		m.toggleActionFilter(events.ActionFileWrite)
		return m, nil
	case "3":
		m.toggleActionFilter(events.ActionCommandExec)
		return m, nil
	case "4":
		m.toggleActionFilter(events.ActionToolUse)
		return m, nil
	case "5":
		m.toggleActionFilter(events.ActionNetworkRequest)
		return m, nil
	case "0":
		m.detailActionFilters = make(map[events.ActionType]bool)
		return m, nil

	case "x":
		if evts := m.sortedFilteredEvents(); m.focus == paneDetail && m.eventIdx < len(evts) {
			m.export = openExportModal(evts[m.eventIdx])
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel search input, go back without changing filter
		m.focus = paneSessionList
		return m, nil

	case "enter":
		// Apply search
		query := strings.TrimSpace(m.searchInput)
		if query == "" {
			// Empty search = clear filter
			m.activeSearchQuery = ""
			m.activeSearchSessionIDs = nil
			m.activeSearchEventIDs = nil
			m.applySearchFilter()
			m.sessionIdx = 0
			m.sessionScroll = 0
			m.focus = paneSessionList
			if len(m.sessions) > 0 {
				return m, loadEvents(m.store, m.sessions[0])
			}
			return m, nil
		}
		// Execute FTS and apply as filter
		return m, executeSearchFilter(m.searcher, query)

	case "backspace":
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.searchInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) handleNavDown() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneSessionList:
		if m.sessionIdx < len(m.sessions)-1 {
			m.sessionIdx++
			m.adjustSessionScroll()
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	case paneDetail:
		if m.expanded {
			m.eventScroll++ // clamped during render
		} else if m.eventIdx < len(m.filteredEvents())-1 {
			m.eventIdx++
		}
	}
	return m, nil
}

func (m Model) handleNavUp() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneSessionList:
		if m.sessionIdx > 0 {
			m.sessionIdx--
			m.adjustSessionScroll()
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	case paneDetail:
		if m.expanded {
			if m.eventScroll > 0 {
				m.eventScroll--
			}
		} else if m.eventIdx > 0 {
			m.eventIdx--
		}
	}
	return m, nil
}

func (m Model) handleNavTop() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneSessionList:
		m.sessionIdx = 0
		m.sessionScroll = 0
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[0])
		}
	case paneDetail:
		m.eventIdx = 0
		m.eventScroll = 0
	}
	return m, nil
}

func (m Model) handleNavBottom() (tea.Model, tea.Cmd) {
	if m.focus == paneSessionList && len(m.sessions) > 0 {
		m.sessionIdx = len(m.sessions) - 1
		m.adjustSessionScroll()
		return m, loadEvents(m.store, m.sessions[m.sessionIdx])
	} else if m.focus == paneDetail {
		evts := m.filteredEvents()
		if len(evts) > 0 {
			m.eventIdx = len(evts) - 1
		}
	}
	return m, nil
}

func (m Model) handleNavPage(dir int) (tea.Model, tea.Cmd) {
	pageSize := m.visibleSessionRows()
	switch m.focus {
	case paneSessionList:
		m.sessionIdx += dir * pageSize
		if m.sessionIdx < 0 {
			m.sessionIdx = 0
		}
		if m.sessionIdx >= len(m.sessions) {
			m.sessionIdx = len(m.sessions) - 1
		}
		if m.sessionIdx < 0 {
			m.sessionIdx = 0
		}
		m.adjustSessionScroll()
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	case paneDetail:
		m.eventIdx += dir * pageSize
		evts := m.filteredEvents()
		if m.eventIdx < 0 {
			m.eventIdx = 0
		}
		if m.eventIdx >= len(evts) {
			m.eventIdx = len(evts) - 1
		}
		if m.eventIdx < 0 {
			m.eventIdx = 0
		}
	}
	return m, nil
}

func (m *Model) adjustSessionScroll() {
	visible := m.visibleSessionRows()
	if visible < 1 {
		visible = 1
	}
	if m.sessionIdx < m.sessionScroll {
		m.sessionScroll = m.sessionIdx
	}
	if m.sessionIdx >= m.sessionScroll+visible {
		m.sessionScroll = m.sessionIdx - visible + 1
	}
}

func (m Model) visibleSessionRows() int {
	h := m.height - 2
	rows := (h - 1) / 3
	if rows < 1 {
		return 1
	}
	return rows
}

func (m Model) toggleActionFilter(at events.ActionType) {
	if m.detailActionFilters[at] {
		delete(m.detailActionFilters, at)
	} else {
		m.detailActionFilters[at] = true
	}
}

// --- View ---

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	if m.loading {
		loading := lipgloss.Place(m.width, contentHeight, lipgloss.Center, lipgloss.Center,
			dimStyle.Render("Loading sessions..."))
		return lipgloss.JoinVertical(lipgloss.Left, header, loading, footer)
	}

	if m.showHelp {
		helpOverlay := m.renderHelp()
		return lipgloss.JoinVertical(lipgloss.Left, header, helpOverlay, footer)
	}

	if m.focus == paneFilter {
		overlay := m.filterBar.view(m.width, contentHeight)
		return lipgloss.JoinVertical(lipgloss.Left, header, overlay, footer)
	}

	if m.export.active {
		overlay := m.renderExportOverlay()
		return lipgloss.JoinVertical(lipgloss.Left, header, overlay, footer)
	}

	var content string
	if m.width >= 80 {
		content = m.splitPaneLayout(contentHeight)
	} else {
		if m.focus == paneDetail {
			content = m.renderDetail(m.width, contentHeight)
		} else {
			content = m.renderSessionList(m.width, contentHeight)
		}
	}

	content = lipgloss.NewStyle().Width(m.width).Height(contentHeight).MaxHeight(contentHeight).Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m Model) splitPaneLayout(height int) string {
	listW := m.listWidth()
	detailW := m.width - listW - 1

	left := m.renderSessionList(listW, height)
	detail := m.renderDetail(detailW, height)

	listStyled := lipgloss.NewStyle().Width(listW).Height(height).Render(left)
	border := renderVerticalBorder(height)
	detailStyled := lipgloss.NewStyle().Width(detailW).Height(height).Render(detail)

	return lipgloss.JoinHorizontal(lipgloss.Top, listStyled, border, detailStyled)
}

func renderVerticalBorder(height int) string {
	var sb strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(verticalBorderStyle.Render("│"))
	}
	return sb.String()
}

func (m Model) listWidth() int {
	if m.width >= 120 {
		return 45
	}
	return 36
}

// --- Data loading commands ---

func loadSessions(store storage.Store, f FilterState) tea.Cmd {
	return func() tea.Msg {
		filter := session.NewSessionFilter().WithLimit(500)
		if len(f.agents) > 0 {
			filter.WithAgents(f.agents)
		}
		if !f.since.IsZero() {
			filter.WithEventSince(f.since)
		}
		if !f.until.IsZero() {
			filter.WithEventUntil(f.until)
		}
		if len(f.actions) > 0 {
			filter.WithEventActions(f.actions)
		}
		if len(f.statuses) > 0 {
			filter.WithEventStatuses(f.statuses)
		}
		if f.filePattern != "" {
			filter.WithFilePattern(f.filePattern)
		}
		if f.cmdPattern != "" {
			filter.WithCommandPattern(f.cmdPattern)
		}
		if f.sensitive {
			filter.WithHasSensitive(true)
		}
		sessions, err := store.QuerySessions(context.Background(), filter)
		if err != nil {
			return loadErrorMsg{err: err}
		}
		return sessionsLoadedMsg{sessions: sessions}
	}
}

func loadEvents(store storage.Store, sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		evts, err := store.GetEventsBySession(context.Background(), sess.ID)
		if err != nil {
			return loadErrorMsg{err: err}
		}
		return eventsLoadedMsg{events: evts}
	}
}

func loadAgents(searcher storage.Searcher) tea.Cmd {
	return func() tea.Msg {
		if searcher == nil {
			return agentsLoadedMsg{}
		}
		agents, err := searcher.DistinctAgents(context.Background())
		if err != nil {
			return agentsLoadedMsg{}
		}
		return agentsLoadedMsg{agents: agents}
	}
}

func backfillFTS(store storage.Store, searcher storage.Searcher) tea.Cmd {
	return func() tea.Msg {
		n, err := searcher.BackfillFTS(context.Background(), store)
		if err != nil {
			return backfillErrorMsg{err: err}
		}
		return backfillDoneMsg{indexed: n}
	}
}

func executeSearchFilter(searcher storage.Searcher, query string) tea.Cmd {
	return func() tea.Msg {
		if searcher == nil || !searcher.HasSearch() {
			return searchErrorMsg{err: nil}
		}
		results, err := searcher.SearchEvents(context.Background(), query, 500)
		if err != nil {
			return searchErrorMsg{err: err}
		}

		sessionIDs := make(map[uuid.UUID]bool)
		eventIDs := make(map[uuid.UUID]bool)
		for _, r := range results {
			sessionIDs[r.SessionID] = true
			eventIDs[r.EventID] = true
		}

		return searchAppliedMsg{
			query:      query,
			sessionIDs: sessionIDs,
			eventIDs:   eventIDs,
		}
	}
}

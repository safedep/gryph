package query

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// Model is the root Bubble Tea model for the query TUI.
type Model struct {
	store    storage.Store
	searcher storage.Searcher
	width    int
	height   int
	ready    bool
	focus    pane
	showHelp bool

	sessions      []*session.Session
	sessionIdx    int
	sessionScroll int

	events      []*events.Event
	summary     sessionSummary
	eventIdx    int
	eventScroll int
	expanded    bool

	sortOrder sortOrder
	filters   FilterState

	searchInput   string
	searchResults []sessionSearchGroup
	searchIdx     int

	filterBar filterBarModel

	initialSession      string
	detailActionFilters map[events.ActionType]bool

	loading bool
	err     error
}

// FilterState holds the current filter configuration.
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
}

// New creates a new query Model from the provided Options.
func New(opts Options) Model {
	f := FilterState{
		agents:      opts.Agents,
		actions:     opts.Actions,
		statuses:    opts.Statuses,
		since:       opts.Since,
		until:       opts.Until,
		filePattern: opts.FilePattern,
		cmdPattern:  opts.CmdPattern,
	}
	return Model{
		store:               opts.Store,
		searcher:            opts.Searcher,
		filters:             f,
		filterBar:           newFilterBar(f),
		initialSession:      opts.Session,
		detailActionFilters: make(map[events.ActionType]bool),
	}
}

// Init starts the initial data load commands.
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

// Update handles all incoming messages.
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
		m.sessions = msg.sessions
		m.loading = false
		m.err = nil
		if m.initialSession != "" {
			for i, s := range m.sessions {
				if strings.HasPrefix(s.ID.String(), m.initialSession) ||
					strings.HasPrefix(s.AgentSessionID, m.initialSession) {
					m.sessionIdx = i
					break
				}
			}
		}
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
		return m, nil

	case eventsLoadedMsg:
		m.events = msg.events
		m.summary = computeSummary(m.events)
		m.eventIdx = 0
		m.eventScroll = 0
		return m, nil

	case agentsLoadedMsg:
		m.filters.allAgents = msg.agents
		return m, nil

	case searchResultsMsg:
		m.searchResults = msg.groups
		m.searchIdx = 0
		return m, nil

	case backfillDoneMsg:
		return m, nil

	case loadErrorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case searchErrorMsg:
		return m, nil

	case backfillErrorMsg:
		return m, nil

	case debounceTickMsg:
		if msg.query == m.searchInput && m.searchInput != "" {
			return m, runSearch(m.searcher, m.store, msg.query)
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.showHelp = !m.showHelp
		return m, nil

	case "tab":
		if m.focus == paneSessionList {
			m.focus = paneDetail
		} else {
			m.focus = paneSessionList
		}
		return m, nil

	case "/":
		m.focus = paneSearch
		m.searchInput = ""
		m.searchResults = nil
		return m, nil

	case "f":
		if m.focus == paneFilter {
			m.focus = paneSessionList
		} else {
			m.focus = paneFilter
		}
		return m, nil

	case "esc":
		if m.focus == paneSearch || m.focus == paneFilter {
			m.focus = paneSessionList
		} else if m.showHelp {
			m.showHelp = false
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
		return m.handleNavPageDown()

	case "pgup":
		return m.handleNavPageUp()

	case "enter":
		if m.focus == paneSearch {
			return m.openSearchResult()
		}
		if m.focus == paneSessionList && len(m.sessions) > 0 {
			m.focus = paneDetail
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
		if m.focus == paneDetail {
			m.expanded = !m.expanded
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
	}

	if m.focus == paneSearch {
		return m.handleSearchInput(msg)
	}

	return m, nil
}

func (m Model) handleNavDown() (tea.Model, tea.Cmd) {
	if m.focus == paneSessionList {
		if m.sessionIdx < len(m.sessions)-1 {
			m.sessionIdx++
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	} else if m.focus == paneDetail {
		if m.eventIdx < len(m.events)-1 {
			m.eventIdx++
		}
	} else if m.focus == paneSearch {
		if m.searchIdx < len(m.searchResults)-1 {
			m.searchIdx++
		}
	}
	return m, nil
}

func (m Model) handleNavUp() (tea.Model, tea.Cmd) {
	if m.focus == paneSessionList {
		if m.sessionIdx > 0 {
			m.sessionIdx--
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	} else if m.focus == paneDetail {
		if m.eventIdx > 0 {
			m.eventIdx--
		}
	} else if m.focus == paneSearch {
		if m.searchIdx > 0 {
			m.searchIdx--
		}
	}
	return m, nil
}

func (m Model) handleNavTop() (tea.Model, tea.Cmd) {
	if m.focus == paneSessionList {
		m.sessionIdx = 0
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	} else if m.focus == paneDetail {
		m.eventIdx = 0
		m.eventScroll = 0
	}
	return m, nil
}

func (m Model) handleNavBottom() (tea.Model, tea.Cmd) {
	if m.focus == paneSessionList && len(m.sessions) > 0 {
		m.sessionIdx = len(m.sessions) - 1
		return m, loadEvents(m.store, m.sessions[m.sessionIdx])
	} else if m.focus == paneDetail && len(m.events) > 0 {
		m.eventIdx = len(m.events) - 1
	}
	return m, nil
}

func (m Model) handleNavPageDown() (tea.Model, tea.Cmd) {
	pageSize := m.listHeight()
	if m.focus == paneSessionList {
		m.sessionIdx += pageSize
		if m.sessionIdx >= len(m.sessions) {
			m.sessionIdx = len(m.sessions) - 1
		}
		if m.sessionIdx < 0 {
			m.sessionIdx = 0
		}
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	} else if m.focus == paneDetail {
		m.eventIdx += pageSize
		if m.eventIdx >= len(m.events) {
			m.eventIdx = len(m.events) - 1
		}
		if m.eventIdx < 0 {
			m.eventIdx = 0
		}
	}
	return m, nil
}

func (m Model) handleNavPageUp() (tea.Model, tea.Cmd) {
	pageSize := m.listHeight()
	if m.focus == paneSessionList {
		m.sessionIdx -= pageSize
		if m.sessionIdx < 0 {
			m.sessionIdx = 0
		}
		if len(m.sessions) > 0 {
			return m, loadEvents(m.store, m.sessions[m.sessionIdx])
		}
	} else if m.focus == paneDetail {
		m.eventIdx -= pageSize
		if m.eventIdx < 0 {
			m.eventIdx = 0
		}
	}
	return m, nil
}

func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "backspace":
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
	case "enter", "esc":
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.searchInput += string(msg.Runes)
		}
	}
	query := m.searchInput
	return m, tea.Tick(300*time.Millisecond, func(_ time.Time) tea.Msg {
		return debounceTickMsg{query: query}
	})
}

func (m Model) toggleActionFilter(at events.ActionType) {
	if m.detailActionFilters[at] {
		delete(m.detailActionFilters, at)
	} else {
		m.detailActionFilters[at] = true
	}
}

// View renders the full TUI.
func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	contentH := m.height - 2

	if m.showHelp {
		help := m.renderHelp()
		return lipgloss.JoinVertical(lipgloss.Left, header, help, footer)
	}

	if m.focus == paneSearch {
		search := m.renderSearch(m.width, contentH)
		return lipgloss.JoinVertical(lipgloss.Left, header, search, footer)
	}

	var content string
	if m.width >= 80 {
		listW := m.listWidth()
		detailW := m.width - listW
		list := m.renderSessionList(listW, contentH)
		detail := m.renderDetail(detailW, contentH)
		content = lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
	} else {
		if m.focus == paneDetail {
			content = m.renderDetail(m.width, contentH)
		} else {
			content = m.renderSessionList(m.width, contentH)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m Model) listWidth() int {
	if m.width >= 120 {
		return 45
	}
	return 36
}

func (m Model) listHeight() int {
	return m.height - 2
}

// loadSessions returns a Cmd that queries sessions from the store.
func loadSessions(store storage.Store, f FilterState) tea.Cmd {
	return func() tea.Msg {
		filter := session.NewSessionFilter().WithLimit(500)
		if len(f.agents) > 0 {
			filter.WithAgents(f.agents)
		}
		if !f.since.IsZero() {
			filter.WithSince(f.since)
		}
		if !f.until.IsZero() {
			filter.WithUntil(f.until)
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
		sessions, err := store.QuerySessions(context.Background(), filter)
		if err != nil {
			return loadErrorMsg{err: err}
		}
		return sessionsLoadedMsg{sessions: sessions}
	}
}

// loadEvents returns a Cmd that loads events for the given session.
func loadEvents(store storage.Store, sess *session.Session) tea.Cmd {
	return func() tea.Msg {
		evts, err := store.GetEventsBySession(context.Background(), sess.ID)
		if err != nil {
			return loadErrorMsg{err: err}
		}
		return eventsLoadedMsg{events: evts}
	}
}

// loadAgents returns a Cmd that fetches distinct agent names.
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

// backfillFTS returns a Cmd that populates the FTS index.
func backfillFTS(store storage.Store, searcher storage.Searcher) tea.Cmd {
	return func() tea.Msg {
		n, err := searcher.BackfillFTS(context.Background(), store)
		if err != nil {
			return backfillErrorMsg{err: err}
		}
		return backfillDoneMsg{indexed: n}
	}
}

// runSearch executes a full-text search and groups results by session.
func runSearch(searcher storage.Searcher, store storage.Store, query string) tea.Cmd {
	return func() tea.Msg {
		if searcher == nil || !searcher.HasSearch() {
			return searchResultsMsg{query: query}
		}
		results, err := searcher.SearchEvents(context.Background(), query, 200)
		if err != nil {
			return searchErrorMsg{err: err}
		}

		seen := make(map[string]int)
		var groups []sessionSearchGroup
		for _, r := range results {
			key := r.SessionID.String()
			if idx, ok := seen[key]; ok {
				groups[idx].matches = append(groups[idx].matches, r)
			} else {
				sess, _ := store.GetSession(context.Background(), r.SessionID)
				if sess == nil {
					continue
				}
				seen[key] = len(groups)
				groups = append(groups, sessionSearchGroup{
					session: sess,
					matches: []storage.SearchResult{r},
				})
			}
		}
		return searchResultsMsg{query: query, groups: groups}
	}
}

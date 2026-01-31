package livelog

import (
	"strings"

	coreevents "github.com/safedep/gryph/core/events"
)

const maxEvents = 1000

type eventListModel struct {
	items      []*coreevents.Event
	lines      []string
	offset     int
	autoScroll bool
	filters    map[coreevents.ActionType]bool
}

func newEventListModel() eventListModel {
	return eventListModel{
		autoScroll: true,
		filters:    make(map[coreevents.ActionType]bool),
	}
}

func (m *eventListModel) append(evts []*coreevents.Event, width int) {
	for _, e := range evts {
		m.items = append(m.items, e)
		m.lines = append(m.lines, formatEvent(e, width))
	}
	if len(m.items) > maxEvents {
		drop := len(m.items) - maxEvents
		m.items = m.items[drop:]
		m.lines = m.lines[drop:]
		m.offset -= drop
		if m.offset < 0 {
			m.offset = 0
		}
	}
}

func (m *eventListModel) clear() {
	m.items = nil
	m.lines = nil
	m.offset = 0
	m.autoScroll = true
}

func (m *eventListModel) toggleFilter(action coreevents.ActionType) {
	if m.filters[action] {
		delete(m.filters, action)
	} else {
		m.filters[action] = true
	}
}

func (m *eventListModel) clearFilters() {
	m.filters = make(map[coreevents.ActionType]bool)
}

func (m *eventListModel) hasFilters() bool {
	return len(m.filters) > 0
}

func (m eventListModel) filteredLines() []string {
	if !m.hasFilters() {
		return m.lines
	}
	var result []string
	for i, e := range m.items {
		if m.filters[e.ActionType] {
			result = append(result, m.lines[i])
		}
	}
	return result
}

func (m *eventListModel) scrollUp(n int) {
	m.autoScroll = false
	m.offset -= n
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *eventListModel) scrollDown(n int, viewHeight int) {
	lines := m.filteredLines()
	m.offset += n
	maxOffset := len(lines) - viewHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset >= maxOffset {
		m.offset = maxOffset
		m.autoScroll = true
	}
}

func (m *eventListModel) jumpToBottom(viewHeight int) {
	lines := m.filteredLines()
	m.offset = len(lines) - viewHeight
	if m.offset < 0 {
		m.offset = 0
	}
	m.autoScroll = true
}

func (m *eventListModel) jumpToTop() {
	m.offset = 0
	m.autoScroll = false
}

func (m eventListModel) view(width, height int) string {
	lines := m.filteredLines()

	if m.autoScroll {
		start := len(lines) - height
		if start < 0 {
			start = 0
		}
		visible := lines[start:]
		if len(visible) < height {
			padding := make([]string, height-len(visible))
			for i := range padding {
				padding[i] = ""
			}
			visible = append(visible, padding...)
		}
		return strings.Join(visible, "\n")
	}

	end := m.offset + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[m.offset:end]
	if len(visible) < height {
		padding := make([]string, height-len(visible))
		for i := range padding {
			padding[i] = ""
		}
		visible = append(visible, padding...)
	}
	return strings.Join(visible, "\n")
}

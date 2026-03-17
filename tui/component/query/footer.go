package query

import "github.com/safedep/gryph/core/events"

func (m Model) renderFooter() string {
	sep := " · "
	var hints string
	switch m.focus {
	case paneSessionList:
		hints = " j/k nav" + sep + "enter select" + sep + "/ search" + sep + "f filter" + sep + "o sort" + sep + "? help" + sep + "q quit"
		if m.activeSearchQuery != "" {
			hints = " j/k nav" + sep + "enter select" + sep + "/ search" + sep + "esc clear search" + sep + "? help" + sep + "q quit"
		}
	case paneDetail:
		hints = " j/k nav" + sep + "enter expand" + sep + "esc back" + sep + "tab pane" + sep + "/ search" + sep + "f filter" + sep + m.actionFilterHints() + sep + "q quit"
	case paneSearch:
		hints = " type to search" + sep + "enter apply" + sep + "esc cancel"
	case paneFilter:
		hints = " tab fields" + sep + "space toggle" + sep + "enter apply" + sep + "esc cancel"
	default:
		hints = " ? help" + sep + "q quit"
	}

	if m.loading {
		hints = " loading..." + sep + hints
	}

	return footerStyle.Width(m.width).Render(hints)
}

func (m Model) actionFilterHints() string {
	type entry struct {
		key    string
		label  string
		action events.ActionType
	}
	filters := []entry{
		{"1", "read", events.ActionFileRead},
		{"2", "write", events.ActionFileWrite},
		{"3", "cmd", events.ActionCommandExec},
		{"4", "tool", events.ActionToolUse},
		{"5", "net", events.ActionNetworkRequest},
	}

	hasActive := len(m.detailActionFilters) > 0
	var parts string
	for _, f := range filters {
		if m.detailActionFilters[f.action] {
			parts += "[" + f.key + ":" + f.label + "] "
		} else {
			parts += f.key + ":" + f.label + " "
		}
	}

	if hasActive {
		parts += "0:clear"
	}

	return parts
}

package livelog

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
)

func pollEvents(store storage.Store, after time.Time, agentFilter string, limit int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		evts, err := store.QueryEventsAfter(ctx, after, limit)
		if err != nil {
			return pollErrorMsg{err: err}
		}

		if agentFilter != "" {
			evts = filterByAgent(evts, agentFilter)
		}

		return newEventsMsg{events: evts}
	}
}

func loadInitialEvents(store storage.Store, since time.Time, agentFilter string, limit int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		filter := events.NewEventFilter().
			WithSince(since).
			WithLimit(limit)

		if agentFilter != "" {
			filter = filter.WithAgents(agentFilter)
		}

		evts, err := store.QueryEvents(ctx, filter)
		if err != nil {
			return pollErrorMsg{err: err}
		}

		return newEventsMsg{events: evts}
	}
}

func schedulePoll(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func filterByAgent(evts []*events.Event, agent string) []*events.Event {
	filtered := make([]*events.Event, 0, len(evts))
	for _, e := range evts {
		if e.AgentName == agent {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

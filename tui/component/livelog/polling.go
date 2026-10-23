package livelog

import (
	"context"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
)

func pollEvents(store storage.ReadStore, cursor pollCursor, agentFilter string, limit int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		raw, err := store.QueryEventsAfter(ctx, cursor.at, cursor.id, limit)
		if err != nil {
			return pollErrorMsg{err: err}
		}

		// Advance the cursor from the last raw event, not from the last
		// displayed event. The agent filter can hide a whole batch.
		next := cursor
		if len(raw) > 0 {
			last := raw[len(raw)-1]
			next = pollCursor{at: last.Timestamp.UTC(), id: last.ID}
		}

		shown := raw
		if agentFilter != "" {
			shown = filterByAgent(raw, agentFilter)
		}

		return newEventsMsg{events: shown, cursor: next}
	}
}

func loadInitialEvents(store storage.ReadStore, since time.Time, agentFilter string, limit int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		// Fetch DESC to get the N most recent events, then reverse
		// to chronological order for the live TUI display.
		filter := events.NewEventFilter().
			WithSort(events.SortDesc).
			WithSince(since).
			WithLimit(limit)

		if agentFilter != "" {
			filter = filter.WithAgents(agentFilter)
		}

		evts, err := store.QueryEvents(ctx, filter)
		if err != nil {
			return pollErrorMsg{err: err}
		}

		slices.Reverse(evts)

		cursor := pollCursor{}
		if len(evts) > 0 {
			last := evts[len(evts)-1]
			cursor = pollCursor{at: last.Timestamp.UTC(), id: last.ID}
		}

		return newEventsMsg{events: evts, cursor: cursor}
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

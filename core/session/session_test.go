package session

import (
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
)

func TestEventCounts(t *testing.T) {
	cases := []struct {
		name  string
		event events.Event
		want  Counts
	}{
		{"action with no kind", events.Event{ActionType: events.ActionFileRead}, Counts{TotalActions: 1, FilesRead: 1}},
		{"write action", events.Event{Kind: events.KindAction, ActionType: events.ActionFileWrite}, Counts{TotalActions: 1, FilesWritten: 1}},
		{"network action", events.Event{Kind: events.KindAction, ActionType: events.ActionNetworkRequest}, Counts{TotalActions: 1, NetworkRequests: 1}},
		{"blocked sensitive command", events.Event{Kind: events.KindAction, ActionType: events.ActionCommandExec, ResultStatus: events.ResultBlocked, IsSensitive: true},
			Counts{TotalActions: 1, CommandsExecuted: 1, BlockedActions: 1, SensitiveActions: 1}},
		{"observation", events.Event{Kind: events.KindObservation, ActionType: events.ActionCommandExec}, Counts{}},
		{"blocked observation", events.Event{Kind: events.KindObservation, ActionType: events.ActionToolUse, ResultStatus: events.ResultBlocked}, Counts{BlockedActions: 1}},
		{"sensitive observation", events.Event{Kind: events.KindObservation, ActionType: events.ActionFileRead, IsSensitive: true}, Counts{SensitiveActions: 1}},
		{"failed observation", events.Event{Kind: events.KindObservation, ActionType: events.ActionCommandExec, ResultStatus: events.ResultError}, Counts{Errors: 1}},
		{"failed action", events.Event{Kind: events.KindAction, ActionType: events.ActionToolUse, ResultStatus: events.ResultError}, Counts{TotalActions: 1, Errors: 1}},
		{"failed intent", events.Event{Kind: events.KindIntent, ResultStatus: events.ResultError}, Counts{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EventCounts(&tc.event))
		})
	}
}

func TestSession_CountEvent(t *testing.T) {
	s := &Session{EventCount: 4, TotalActions: 3}
	s.CountEvent(&events.Event{Sequence: 5, Kind: events.KindAction, ActionType: events.ActionFileRead})
	assert.Equal(t, 5, s.EventCount)
	assert.Equal(t, 4, s.TotalActions)
	assert.Equal(t, 1, s.FilesRead)
}

func TestSession_RecordedEvents(t *testing.T) {
	cases := []struct {
		name    string
		session Session
		want    int
	}{
		{"new session", Session{}, 0},
		{"event count leads", Session{EventCount: 7, TotalActions: 3}, 7},
		{"legacy session with no event count", Session{TotalActions: 5}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.session.RecordedEvents())
		})
	}
}

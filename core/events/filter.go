package events

import (
	"time"

	"github.com/google/uuid"
)

// EventFilter provides filtering criteria for querying events.
type EventFilter struct {
	// Since filters events after this time.
	Since *time.Time
	// Until filters events before this time.
	Until *time.Time
	// AgentNames filters by agent name(s).
	AgentNames []string
	// SessionID filters by a specific session.
	SessionID *uuid.UUID
	// ActionTypes filters by action type(s).
	ActionTypes []ActionType
	// ResultStatuses filters by result status(es).
	ResultStatuses []ResultStatus
	// FilePattern is a glob pattern to filter by file path.
	FilePattern string
	// CommandPattern is a glob pattern to filter by command.
	CommandPattern string
	// Limit is the maximum number of results.
	Limit int
	// Offset is the number of results to skip.
	Offset int
}

// NewEventFilter creates a new EventFilter with default values.
func NewEventFilter() *EventFilter {
	return &EventFilter{
		Limit: 100,
	}
}

// WithSince sets the Since filter.
func (f *EventFilter) WithSince(t time.Time) *EventFilter {
	f.Since = &t
	return f
}

// WithUntil sets the Until filter.
func (f *EventFilter) WithUntil(t time.Time) *EventFilter {
	f.Until = &t
	return f
}

// WithAgents sets the AgentNames filter.
func (f *EventFilter) WithAgents(agents ...string) *EventFilter {
	f.AgentNames = agents
	return f
}

// WithSession sets the SessionID filter.
func (f *EventFilter) WithSession(sessionID uuid.UUID) *EventFilter {
	f.SessionID = &sessionID
	return f
}

// WithActions sets the ActionTypes filter.
func (f *EventFilter) WithActions(actions ...ActionType) *EventFilter {
	f.ActionTypes = actions
	return f
}

// WithStatuses sets the ResultStatuses filter.
func (f *EventFilter) WithStatuses(statuses ...ResultStatus) *EventFilter {
	f.ResultStatuses = statuses
	return f
}

// WithFilePattern sets the FilePattern filter.
func (f *EventFilter) WithFilePattern(pattern string) *EventFilter {
	f.FilePattern = pattern
	return f
}

// WithCommandPattern sets the CommandPattern filter.
func (f *EventFilter) WithCommandPattern(pattern string) *EventFilter {
	f.CommandPattern = pattern
	return f
}

// WithLimit sets the Limit.
func (f *EventFilter) WithLimit(limit int) *EventFilter {
	f.Limit = limit
	return f
}

// WithOffset sets the Offset.
func (f *EventFilter) WithOffset(offset int) *EventFilter {
	f.Offset = offset
	return f
}

// Today returns a filter for events from today (midnight to now).
func Today() *EventFilter {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return NewEventFilter().WithSince(midnight)
}

// Yesterday returns a filter for events from yesterday.
func Yesterday() *EventFilter {
	now := time.Now()
	yesterdayStart := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
	yesterdayEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return NewEventFilter().WithSince(yesterdayStart).WithUntil(yesterdayEnd)
}

// Last24Hours returns a filter for events in the last 24 hours.
func Last24Hours() *EventFilter {
	since := time.Now().Add(-24 * time.Hour)
	return NewEventFilter().WithSince(since)
}

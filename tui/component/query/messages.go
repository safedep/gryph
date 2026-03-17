package query

import (
	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

type sessionsLoadedMsg struct {
	sessions []*session.Session
}

type eventsLoadedMsg struct {
	events []*events.Event
}

type agentsLoadedMsg struct {
	agents []string
}

type searchAppliedMsg struct {
	query      string
	sessionIDs map[uuid.UUID]bool
	eventIDs   map[uuid.UUID]bool
}

type loadErrorMsg struct {
	err error
}

type searchErrorMsg struct {
	err error
}

type backfillDoneMsg struct {
	indexed int
}

type backfillErrorMsg struct {
	err error
}

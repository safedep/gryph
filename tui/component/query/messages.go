package query

import (
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
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

type searchResultsMsg struct {
	query  string
	groups []sessionSearchGroup
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

type sessionSearchGroup struct {
	session *session.Session
	matches []storage.SearchResult
}

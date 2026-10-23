package livelog

import (
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
)

// pollCursor is the compound position of the newest event read from storage.
// Polling resumes strictly after this position.
type pollCursor struct {
	at time.Time
	id uuid.UUID
}

// valid reports whether the cursor carries a timestamp. A zero cursor means
// that storage returned no event.
func (c pollCursor) valid() bool {
	return !c.at.IsZero()
}

type newEventsMsg struct {
	events []*events.Event
	cursor pollCursor
}

type pollErrorMsg struct {
	err error
}

type tickMsg time.Time

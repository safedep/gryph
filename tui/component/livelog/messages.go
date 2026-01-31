package livelog

import (
	"time"

	"github.com/safedep/gryph/core/events"
)

type newEventsMsg struct {
	events []*events.Event
}

type pollErrorMsg struct {
	err error
}

type tickMsg time.Time

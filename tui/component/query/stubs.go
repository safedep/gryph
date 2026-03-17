package query

import "github.com/safedep/gryph/core/events"

// sessionSummary holds aggregated stats for a session's events.
// Full implementation in summary.go (future task).
type sessionSummary struct{}

// filterBarModel is the filter overlay component.
// Full implementation in filterbar.go (future task).
type filterBarModel struct{}

func computeSummary(_ []*events.Event) sessionSummary {
	return sessionSummary{}
}

func newFilterBar(_ FilterState) filterBarModel {
	return filterBarModel{}
}

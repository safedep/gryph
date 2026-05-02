package agent

import (
	"encoding/json"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
)

// mutatePayload unmarshals the event's payload into T, applies fn, and marshals
// the result back. On unmarshal or marshal failure the payload is left in its
// previous state and a warning is logged so the silent skip does not hide
// pipeline bugs. Used by the redact and logging-level pipelines.
func mutatePayload[T any](event *events.Event, fn func(*T)) {
	var p T
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		log.Warnf("mutatePayload: unmarshal %s payload failed: %v", event.ActionType, err)
		return
	}

	fn(&p)

	data, err := json.Marshal(&p)
	if err != nil {
		log.Warnf("mutatePayload: marshal %s payload failed: %v", event.ActionType, err)
		return
	}

	event.Payload = data
}

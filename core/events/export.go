package events

import (
	"encoding/json"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/privacy"
)

// ForExport returns a copy of the event for a destination outside the
// machine. It decodes and encodes the payload again, so a row from before
// content labels has the same shape as a new row. Each content value goes
// through privacy.Text.ForExport.
func (e *Event) ForExport() *Event {
	out := *e
	out.DiffContent = e.DiffContent.ForExport()

	payload := NewPayload(e.ActionType)
	if payload == nil || len(e.Payload) == 0 {
		return &out
	}
	if err := json.Unmarshal(e.Payload, payload); err != nil {
		log.Warnf("events: export %s payload: %v", e.ActionType, err)
		return &out
	}
	privacy.Walk(payload, func(_ string, t *privacy.Text) {
		*t = t.ForExport()
	})
	data, err := json.Marshal(payload)
	if err != nil {
		log.Warnf("events: export %s payload: %v", e.ActionType, err)
		return &out
	}
	out.Payload = data
	return &out
}

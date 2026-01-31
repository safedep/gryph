package agent

import (
	"encoding/json"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

// ApplyLoggingLevel strips fields from the event based on the configured logging level.
// Must be called after parsing and before saving.
func ApplyLoggingLevel(event *events.Event, level config.LoggingLevel) {
	switch {
	case level.IsAtLeast(config.LoggingFull) && !event.IsSensitive:
		// Keep everything
		return

	case level.IsAtLeast(config.LoggingStandard):
		// Standard: keep payload content previews, strip raw/diff/context
		event.RawEvent = nil
		event.DiffContent = ""
		event.ConversationContext = ""

	default:
		// Minimal: strip raw/diff/context and content fields from payloads
		event.RawEvent = nil
		event.DiffContent = ""
		event.ConversationContext = ""
		stripPayloadContent(event)
	}
}

// stripPayloadContent removes content preview fields from payloads at minimal level.
func stripPayloadContent(event *events.Event) {
	if len(event.Payload) == 0 {
		return
	}

	switch event.ActionType {
	case events.ActionFileWrite:
		var payload events.FileWritePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return
		}
		payload.ContentPreview = ""
		payload.OldString = ""
		payload.NewString = ""
		if data, err := json.Marshal(payload); err == nil {
			event.Payload = data
		}

	case events.ActionCommandExec:
		var payload events.CommandExecPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return
		}
		payload.Output = ""
		payload.StdoutPreview = ""
		payload.StderrPreview = ""
		if data, err := json.Marshal(payload); err == nil {
			event.Payload = data
		}
	}
}

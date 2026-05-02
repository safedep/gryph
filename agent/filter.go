package agent

import (
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

// ApplyLoggingLevel strips fields from the event based on the configured
// logging level. Must be called after parsing and before saving. Sensitive
// events have their content stripped unconditionally regardless of level.
func ApplyLoggingLevel(event *events.Event, level config.LoggingLevel) {
	if event.IsSensitive {
		event.RawEvent = nil
		event.DiffContent = ""
		event.ConversationContext = ""

		stripPayloadContent(event)
		return
	}

	switch {
	case level.IsAtLeast(config.LoggingFull):
		return

	case level.IsAtLeast(config.LoggingStandard):
		event.RawEvent = nil
		event.DiffContent = ""
		event.ConversationContext = ""

	default:
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
		mutatePayload(event, func(p *events.FileWritePayload) {
			p.ContentPreview = ""
			p.OldString = ""
			p.NewString = ""
			p.LinesAdded = 0
			p.LinesRemoved = 0
		})

	case events.ActionCommandExec:
		mutatePayload(event, func(p *events.CommandExecPayload) {
			p.Output = ""
			p.StdoutPreview = ""
			p.StderrPreview = ""
		})

	case events.ActionToolUse:
		mutatePayload(event, func(p *events.ToolUsePayload) {
			p.Input = nil
			p.Output = nil
			p.OutputPreview = ""
		})
	}
}

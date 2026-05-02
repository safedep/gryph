package agent

import (
	"encoding/json"

	"github.com/safedep/gryph/core/events"
)

// RedactEvent applies the checker's redaction patterns to event fields that may
// contain user content. Must be called after parsing and before ApplyLoggingLevel,
// so configured patterns are scrubbed before any storage or logging-level filtering.
// No-ops on a nil checker or event.
func RedactEvent(event *events.Event, checker *events.PrivacyChecker) {
	if checker == nil || event == nil {
		return
	}

	if len(event.RawEvent) > 0 {
		event.RawEvent = json.RawMessage(checker.Redact(string(event.RawEvent)))
	}

	event.DiffContent = checker.Redact(event.DiffContent)
	event.ConversationContext = checker.Redact(event.ConversationContext)

	if len(event.Payload) == 0 {
		return
	}

	switch event.ActionType {
	case events.ActionFileWrite:
		mutatePayload(event, func(p *events.FileWritePayload) {
			p.ContentPreview = checker.Redact(p.ContentPreview)
			p.OldString = checker.Redact(p.OldString)
			p.NewString = checker.Redact(p.NewString)
		})

	case events.ActionCommandExec:
		mutatePayload(event, func(p *events.CommandExecPayload) {
			p.Command = checker.Redact(p.Command)
			p.Output = checker.Redact(p.Output)
			p.StdoutPreview = checker.Redact(p.StdoutPreview)
			p.StderrPreview = checker.Redact(p.StderrPreview)
		})

	case events.ActionToolUse:
		// Input/Output are arbitrary JSON; redacting their bytes as a flat
		// string can break structure. The level filter strips them at minimal;
		// a deeper walker can be added later if needed.
		mutatePayload(event, func(p *events.ToolUsePayload) {
			p.OutputPreview = checker.Redact(p.OutputPreview)
		})
	}
}

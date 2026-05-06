package agent

import (
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
		event.RawEvent = checker.RedactJSON(event.RawEvent)
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
		mutatePayload(event, func(p *events.ToolUsePayload) {
			p.Input = checker.RedactJSON(p.Input)
			p.Output = checker.RedactJSON(p.Output)
			p.OutputPreview = checker.Redact(p.OutputPreview)
		})
	}
}

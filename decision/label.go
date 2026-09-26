package decision

import (
	"encoding/json"
	"slices"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
)

// keptAtMinimal are the payload fields that the minimal level and a
// sensitive event keep. The command is the main audit fact of a
// command_exec event, and the redactor already removed its secrets.
var keptAtMinimal = map[string]bool{"command": true}

// keptAtFullOnly are the content values that only the full level keeps. A
// prompt holds what the user typed, which can be a pasted secret or private
// text, so the standard level keeps its label and digest only.
var keptAtFullOnly = map[string]bool{"diff_content": true, "prompt": true}

// outputFields are the content values that a tool returned. They take the
// origin of the event. Every other value, except a prompt, is what the agent
// wrote, so it takes the origin agent.
var outputFields = map[string]bool{"output": true, "stdout_preview": true, "stderr_preview": true, "output_preview": true}

// labelEvent runs the first label steps on every content value, in the order
// of docs/content-labels.md: digest and size of the raw value, classes, and
// redaction. The policy then evaluates the redacted event. applyLevel strips
// values after the evaluation, so a rule on a URL or on content still sees
// it at every logging level.
//
// Only the secret class makes an event sensitive. The other classes are
// facts on the label. The sensitive-path check of the adapter adds the
// secret class.
func labelEvent(event *events.Event, redactor *privacy.Redactor, classes []privacy.Class) {
	if event.IsSensitive && !slices.Contains(classes, privacy.ClassSecret) {
		classes = append(classes, privacy.ClassSecret)
	}
	if slices.Contains(classes, privacy.ClassSecret) {
		event.IsSensitive = true
	}

	if err := walkContent(event, func(path string, t *privacy.Text) {
		if t.IsZero() {
			return
		}
		setOrigin(event, path, t)
		if t.Label.Digest == "" && t.Value != "" {
			t.Label.Digest = privacy.Digest(t.Value)
			t.Label.Size = len(t.Value)
		}
		t.Label.AddClasses(classes...)
		if redactor != nil && t.Value != "" {
			redactText(redactor, t)
		}
	}, func(p any) {
		if end, ok := p.(*events.SessionEndPayload); ok && redactor != nil {
			end.Reason = redactor.Redact(end.Reason)
		}
	}); err != nil {
		log.Warnf("decision: label the %s payload that does not decode: %v", event.ActionType, err)
	}

	if redactor != nil {
		if len(event.RawEvent) > 0 {
			event.RawEvent = redactor.RedactJSON(event.RawEvent)
		}
		event.ErrorMessage = redactor.Redact(event.ErrorMessage)
	}
}

// StripsContent reports whether the logging level or the sensitivity of the
// event removes the content values from the stored event. The AARM receipt
// uses the same rule, so a receipt never keeps what the event loses.
func StripsContent(event *events.Event, level config.LoggingLevel) bool {
	return event.IsSensitive || !level.IsAtLeast(config.LoggingStandard)
}

// applyLevel records the logging level on every label and removes the
// values that the level or the sensitivity of the event does not keep. A
// sensitive event keeps labels only, except the command.
func applyLevel(event *events.Event, level config.LoggingLevel) {
	stripContent := StripsContent(event, level)
	stripFull := stripContent || !level.IsAtLeast(config.LoggingFull)

	strip := func(t *privacy.Text, remove bool) {
		if t.IsZero() {
			return
		}
		t.Label.Level = string(level)
		if remove && t.Value != "" {
			t.Value = ""
			t.Label.Stripped = true
		}
	}

	if err := walkContent(event, func(path string, t *privacy.Text) {
		if keptAtFullOnly[path] {
			strip(t, stripFull)
			return
		}
		strip(t, stripContent && !keptAtMinimal[path])
	}, func(p any) {
		if !stripContent {
			return
		}
		switch p := p.(type) {
		case *events.FileWritePayload:
			p.LinesAdded = 0
			p.LinesRemoved = 0
		case *events.SessionEndPayload:
			p.Reason = ""
		}
	}); err != nil {
		log.Warnf("decision: drop the %s payload that does not decode: %v", event.ActionType, err)
		event.Payload = nil
	}

	if stripFull {
		event.RawEvent = nil
	}
	if event.IsSensitive {
		event.FullContent = ""
	}
}

func setOrigin(event *events.Event, path string, t *privacy.Text) {
	if t.Label.Origin != "" || event.Origin == "" {
		return
	}
	if path == "prompt" {
		t.Label.Origin = event.Origin
		return
	}
	if !outputFields[path] {
		t.Label.Origin = privacy.OriginAgent
		return
	}
	t.Label.Origin = event.Origin
	if event.Origin == privacy.OriginMCP {
		t.Label.Source = event.OriginSource
	}
}

// walkContent calls fn for the diff and for every content value of the
// payload, then calls other with the decoded payload for its plain fields,
// and writes the payload back. It returns the error of a payload that does
// not decode. The label step keeps that payload, so the policy sees it and
// its fail mode decides. applyLevel drops it, because Gryph cannot label,
// redact or strip it in storage.
func walkContent(event *events.Event, fn func(path string, t *privacy.Text), other func(payload any)) error {
	fn("diff_content", &event.DiffContent)

	payload := events.NewPayload(event.ActionType)
	if payload == nil || len(event.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(event.Payload, payload); err != nil {
		return err
	}
	privacy.Walk(payload, fn)
	other(payload)

	data, err := json.Marshal(payload)
	if err != nil {
		log.Warnf("decision: marshal %s payload: %v", event.ActionType, err)
		return nil
	}
	event.Payload = data
	return nil
}

// redactText redacts the value. A value that holds a JSON object or array
// keeps its structure, because tool input and output hold JSON.
func redactText(redactor *privacy.Redactor, t *privacy.Text) {
	var redacted string
	if v := []byte(t.Value); json.Valid(v) && (v[0] == '{' || v[0] == '[') {
		redacted = string(redactor.RedactJSON(v))
	} else {
		redacted = redactor.Redact(t.Value)
	}
	if redacted != t.Value {
		t.Value = redacted
		t.Label.Redacted = true
	}
}

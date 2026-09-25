package decision

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRedactor(t *testing.T) *privacy.Redactor {
	t.Helper()
	r, err := privacy.NewRedactor(privacy.DefaultSensitivePatterns(), privacy.DefaultRedactPatterns())
	require.NoError(t, err)
	return r
}

func newLabelEvent(t *testing.T, actionType events.ActionType, payload any) *events.Event {
	t.Helper()
	event := events.NewEvent(uuid.New(), "test-agent", actionType)
	event.RawEvent = []byte(`{"test": "raw"}`)
	event.DiffContent = privacy.NewText("--- a/file\n+++ b/file\n")
	event.ConversationContext = "user said something"
	require.NoError(t, event.SetPayload(payload))
	return event
}

func decode[T any](t *testing.T, event *events.Event) T {
	t.Helper()
	var p T
	require.NoError(t, json.Unmarshal(event.Payload, &p))
	return p
}

// label runs both steps with no evaluation between them.
func label(event *events.Event, redactor *privacy.Redactor, classes []privacy.Class, level config.LoggingLevel) {
	labelEvent(event, redactor, classes)
	applyLevel(event, level)
}

func TestLabelEvent_Levels(t *testing.T) {
	write := events.FileWritePayload{
		Path:           "/work/main.go",
		ContentPreview: privacy.NewText("package main"),
		OldString:      privacy.NewText("old"),
		NewString:      privacy.NewText("new"),
		LinesAdded:     3,
	}
	cases := []struct {
		name         string
		level        config.LoggingLevel
		sensitive    bool
		wantDiff     bool
		wantRaw      bool
		wantPreviews bool
	}{
		{"full", config.LoggingFull, false, true, true, true},
		{"standard", config.LoggingStandard, false, false, false, true},
		{"minimal", config.LoggingMinimal, false, false, false, false},
		{"full but sensitive", config.LoggingFull, true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := newLabelEvent(t, events.ActionFileWrite, write)
			event.IsSensitive = tc.sensitive

			label(event, nil, nil, tc.level)

			assert.Equal(t, tc.wantDiff, event.DiffContent.Value != "")
			assert.Equal(t, !tc.wantDiff, event.DiffContent.Label.Stripped)
			assert.Equal(t, privacy.Digest("--- a/file\n+++ b/file\n"), event.DiffContent.Label.Digest)
			assert.Equal(t, tc.wantRaw, event.RawEvent != nil)
			assert.Equal(t, tc.wantRaw, event.ConversationContext != "")

			p := decode[events.FileWritePayload](t, event)
			assert.Equal(t, "/work/main.go", p.Path)
			for _, text := range []privacy.Text{p.ContentPreview, p.OldString, p.NewString} {
				assert.Equal(t, tc.wantPreviews, text.Value != "")
				assert.Equal(t, !tc.wantPreviews, text.Label.Stripped)
				assert.Equal(t, string(tc.level), text.Label.Level)
				assert.NotEmpty(t, text.Label.Digest)
			}
			assert.Equal(t, privacy.Digest("package main"), p.ContentPreview.Label.Digest)
			assert.Equal(t, len("package main"), p.ContentPreview.Label.Size)
			assert.Equal(t, tc.wantPreviews, p.LinesAdded == 3)
			if tc.sensitive {
				assert.True(t, p.ContentPreview.Label.HasClass(privacy.ClassSecret))
			}
		})
	}
}

func TestLabelEvent_MinimalKeepsCommand(t *testing.T) {
	event := newLabelEvent(t, events.ActionCommandExec, events.CommandExecPayload{
		Command:       privacy.NewText("go test ./..."),
		StdoutPreview: privacy.NewText("ok"),
		Output:        privacy.NewText("ok"),
	})

	label(event, nil, nil, config.LoggingMinimal)

	p := decode[events.CommandExecPayload](t, event)
	assert.Equal(t, "go test ./...", p.Command.Value)
	assert.Equal(t, "minimal", p.Command.Label.Level)
	assert.Empty(t, p.StdoutPreview.Value)
	assert.True(t, p.StdoutPreview.Label.Stripped)
	assert.Empty(t, p.Output.Value)
}

func TestLabelEvent_DigestBeforeRedact(t *testing.T) {
	event := newLabelEvent(t, events.ActionCommandExec, events.CommandExecPayload{
		Command: privacy.NewText("curl -H 'Authorization: Bearer abc123' https://x.example"),
	})

	label(event, testRedactor(t), nil, config.LoggingFull)

	p := decode[events.CommandExecPayload](t, event)
	assert.Contains(t, p.Command.Value, "[REDACTED]")
	assert.NotContains(t, p.Command.Value, "abc123")
	assert.True(t, p.Command.Label.Redacted)
	assert.Equal(t, privacy.Digest("curl -H 'Authorization: Bearer abc123' https://x.example"), p.Command.Label.Digest)
}

func TestLabelEvent_RedactsToolJSON(t *testing.T) {
	event := newLabelEvent(t, events.ActionToolUse, events.ToolUsePayload{
		ToolName: "WebFetch",
		Input:    privacy.NewText(`{"url":"https://x.example","header":"token=abc123"}`),
	})

	label(event, testRedactor(t), nil, config.LoggingFull)

	p := decode[events.ToolUsePayload](t, event)
	assert.True(t, json.Valid([]byte(p.Input.Value)))
	assert.Contains(t, p.Input.Value, "[REDACTED]")
	assert.True(t, p.Input.Label.Redacted)
	assert.False(t, p.Output.Label.Redacted)
	assert.True(t, p.Output.IsZero())
}

func TestLabelEvent_Classes(t *testing.T) {
	cases := []struct {
		name          string
		classes       []privacy.Class
		wantSensitive bool
	}{
		{"source code is not sensitive", []privacy.Class{privacy.ClassSourceCode}, false},
		{"pii is a fact, not sensitive", []privacy.Class{privacy.ClassPII}, false},
		{"unknown sensitive is a fact, not sensitive", []privacy.Class{privacy.ClassUnknownSensitive}, false},
		{"secret is sensitive", []privacy.Class{privacy.ClassSecret}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := newLabelEvent(t, events.ActionFileWrite, events.FileWritePayload{
				Path: "/work/x", ContentPreview: privacy.NewText("data"),
			})
			event.FullContent = "data"

			labelEvent(event, nil, tc.classes)
			assert.Equal(t, "data", event.FullContent, "the policy sees the content before the level step")
			applyLevel(event, config.LoggingFull)

			assert.Equal(t, tc.wantSensitive, event.IsSensitive)
			assert.Equal(t, tc.wantSensitive, event.FullContent == "")
			p := decode[events.FileWritePayload](t, event)
			assert.Equal(t, tc.classes, p.ContentPreview.Label.Classes)
			assert.Equal(t, tc.wantSensitive, p.ContentPreview.Value == "")
		})
	}
}

func TestLabelEvent_ToolUseAndSubagent(t *testing.T) {
	tool := events.ToolUsePayload{
		ToolName:      "WebFetch",
		Input:         privacy.NewText(`{"url":"https://x.example"}`),
		Output:        privacy.NewText(`{"body":"ok"}`),
		OutputPreview: privacy.NewText("ok"),
	}
	cases := []struct {
		name      string
		level     config.LoggingLevel
		sensitive bool
		wantKept  bool
	}{
		{"standard keeps tool content", config.LoggingStandard, false, true},
		{"minimal strips tool content", config.LoggingMinimal, false, false},
		{"sensitive strips tool content", config.LoggingFull, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := newLabelEvent(t, events.ActionToolUse, tool)
			event.IsSensitive = tc.sensitive
			label(event, nil, nil, tc.level)

			p := decode[events.ToolUsePayload](t, event)
			for _, text := range []privacy.Text{p.Input, p.Output, p.OutputPreview} {
				assert.Equal(t, tc.wantKept, text.Value != "")
				assert.Equal(t, !tc.wantKept, text.Label.Stripped)
				assert.NotEmpty(t, text.Label.Digest)
			}
		})
	}

	event := newLabelEvent(t, events.ActionSubagentStop, events.SubagentStopPayload{
		AgentID: "a1", LastAssistantMessage: privacy.NewText("done"),
	})
	label(event, nil, nil, config.LoggingMinimal)
	stop := decode[events.SubagentStopPayload](t, event)
	assert.Equal(t, "a1", stop.AgentID)
	assert.Empty(t, stop.LastAssistantMessage.Value)
	assert.True(t, stop.LastAssistantMessage.Label.Stripped)
}

func TestLabelEvent_RedactsPlainFields(t *testing.T) {
	event := newLabelEvent(t, events.ActionCommandExec, events.CommandExecPayload{Command: privacy.NewText("ls")})
	event.RawEvent = []byte(`{"tool_input":{"command":"export token=abc123"}}`)
	event.ConversationContext = "password=hunter2"
	event.ErrorMessage = "error: bad value token=abc123"

	label(event, testRedactor(t), nil, config.LoggingFull)

	assert.NotContains(t, string(event.RawEvent), "abc123")
	assert.True(t, json.Valid(event.RawEvent))
	assert.NotContains(t, event.ConversationContext, "hunter2")
	assert.NotContains(t, event.ErrorMessage, "abc123")
}

func TestLabelEvent_NilRedactor(t *testing.T) {
	event := newLabelEvent(t, events.ActionCommandExec, events.CommandExecPayload{Command: privacy.NewText("echo token=abc123")})
	assert.NotPanics(t, func() { label(event, nil, nil, config.LoggingFull) })
	p := decode[events.CommandExecPayload](t, event)
	assert.Equal(t, "echo token=abc123", p.Command.Value)
	assert.False(t, p.Command.Label.Redacted)
}

func TestLabelEvent_KeepsAdapterPreviewDigest(t *testing.T) {
	full := strings.Repeat("x", 500)
	event := newLabelEvent(t, events.ActionCommandExec, events.CommandExecPayload{
		Command:       privacy.NewText("cat big"),
		StdoutPreview: privacy.Preview(full, 100),
	})

	label(event, nil, nil, config.LoggingFull)

	p := decode[events.CommandExecPayload](t, event)
	assert.Len(t, p.StdoutPreview.Value, 100)
	assert.True(t, p.StdoutPreview.Label.Truncated)
	assert.Equal(t, privacy.Digest(full), p.StdoutPreview.Label.Digest)
	assert.Equal(t, 500, p.StdoutPreview.Label.Size)
}

func TestLabelEvent_SessionEndReason(t *testing.T) {
	event := newLabelEvent(t, events.ActionSessionEnd, events.SessionEndPayload{Reason: "token=abc123"})
	label(event, testRedactor(t), nil, config.LoggingStandard)
	assert.Equal(t, "[REDACTED]", decode[events.SessionEndPayload](t, event).Reason)

	event = newLabelEvent(t, events.ActionSessionEnd, events.SessionEndPayload{Reason: "logout"})
	label(event, nil, nil, config.LoggingMinimal)
	assert.Empty(t, decode[events.SessionEndPayload](t, event).Reason)
}

func TestLabelEvent_LegacyPayload(t *testing.T) {
	event := events.NewEvent(uuid.New(), "test-agent", events.ActionCommandExec)
	event.Payload = []byte(`{"command":"ls -la","exit_code":0}`)

	label(event, nil, nil, config.LoggingFull)

	p := decode[events.CommandExecPayload](t, event)
	assert.Equal(t, "ls -la", p.Command.Value)
	assert.Equal(t, privacy.Digest("ls -la"), p.Command.Label.Digest)
}

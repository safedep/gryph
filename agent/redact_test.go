package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactEvent_FileWrite(t *testing.T) {
	checker, err := events.NewPrivacyChecker(nil, events.DefaultRedactPatterns())
	require.NoError(t, err)

	event := events.NewEvent(uuid.New(), "test-agent", events.ActionFileWrite)
	event.RawEvent = json.RawMessage(`{"body":"password=hunter2"}`)
	event.DiffContent = "+ api_key=abc123\n"
	event.ConversationContext = "user said token=xyz789"

	require.NoError(t, event.SetPayload(events.FileWritePayload{
		Path:           "/tmp/app.go",
		ContentPreview: "password=hunter2",
		OldString:      "api_key=old",
		NewString:      "api_key=new",
	}))

	RedactEvent(event, checker)

	assert.Contains(t, string(event.RawEvent), "[REDACTED]")
	assert.NotContains(t, string(event.RawEvent), "hunter2")
	assert.Contains(t, event.DiffContent, "[REDACTED]")
	assert.NotContains(t, event.DiffContent, "abc123")
	assert.Contains(t, event.ConversationContext, "[REDACTED]")
	assert.NotContains(t, event.ConversationContext, "xyz789")

	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "/tmp/app.go", result.Path)
	assert.Equal(t, "[REDACTED]", result.ContentPreview)
	assert.Equal(t, "[REDACTED]", result.OldString)
	assert.Equal(t, "[REDACTED]", result.NewString)
}

func TestRedactEvent_CommandExec(t *testing.T) {
	checker, err := events.NewPrivacyChecker(nil, events.DefaultRedactPatterns())
	require.NoError(t, err)

	event := events.NewEvent(uuid.New(), "test-agent", events.ActionCommandExec)
	require.NoError(t, event.SetPayload(events.CommandExecPayload{
		Command:       "echo password=hunter2",
		Output:        "secret=abc",
		StdoutPreview: "token=xyz",
		StderrPreview: "no leak here",
	}))

	RedactEvent(event, checker)

	var result events.CommandExecPayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "echo [REDACTED]", result.Command)
	assert.Equal(t, "[REDACTED]", result.Output)
	assert.Equal(t, "[REDACTED]", result.StdoutPreview)
	assert.Equal(t, "no leak here", result.StderrPreview)
}

func TestRedactEvent_ToolUse(t *testing.T) {
	checker, err := events.NewPrivacyChecker(nil, events.DefaultRedactPatterns())
	require.NoError(t, err)

	event := events.NewEvent(uuid.New(), "test-agent", events.ActionToolUse)
	require.NoError(t, event.SetPayload(events.ToolUsePayload{
		ToolName:      "web_fetch",
		Input:         json.RawMessage(`{"q":"password=hunter2"}`),
		Output:        json.RawMessage(`{"got":"token=xyz"}`),
		OutputPreview: "secret=abc",
	}))

	RedactEvent(event, checker)

	var result events.ToolUsePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.JSONEq(t, `{"q":"password=hunter2"}`, string(result.Input))
	assert.JSONEq(t, `{"got":"token=xyz"}`, string(result.Output))
	assert.Equal(t, "[REDACTED]", result.OutputPreview)
}

func TestRedactEvent_RedactsSensitive(t *testing.T) {
	// Sensitive events still get redacted as defense in depth: the level
	// filter strips them later, but redaction must not depend on a sibling
	// running afterwards.
	checker, err := events.NewPrivacyChecker(nil, events.DefaultRedactPatterns())
	require.NoError(t, err)

	event := events.NewEvent(uuid.New(), "test-agent", events.ActionFileWrite)
	event.IsSensitive = true
	event.DiffContent = "password=hunter2"
	require.NoError(t, event.SetPayload(events.FileWritePayload{
		Path:           "/tmp/.env",
		ContentPreview: "password=hunter2",
	}))

	RedactEvent(event, checker)

	assert.Equal(t, "[REDACTED]", event.DiffContent)
	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "[REDACTED]", result.ContentPreview)
}

func TestRedactEvent_NilChecker(t *testing.T) {
	event := events.NewEvent(uuid.New(), "test-agent", events.ActionFileWrite)
	event.DiffContent = "password=hunter2"
	require.NoError(t, event.SetPayload(events.FileWritePayload{
		Path:           "/tmp/app.go",
		ContentPreview: "password=hunter2",
	}))

	RedactEvent(event, nil)

	assert.Equal(t, "password=hunter2", event.DiffContent)
}

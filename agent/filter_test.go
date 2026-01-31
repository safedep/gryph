package agent

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEvent(actionType events.ActionType) *events.Event {
	event := events.NewEvent(uuid.New(), "test-agent", actionType)
	event.RawEvent = []byte(`{"test": "raw"}`)
	event.DiffContent = "--- a/file\n+++ b/file\n"
	event.ConversationContext = "user said something"
	return event
}

func TestApplyLoggingLevel_Full_NotSensitive(t *testing.T) {
	event := newTestEvent(events.ActionFileWrite)
	event.IsSensitive = false

	payload := events.FileWritePayload{
		Path:           "/tmp/test.go",
		ContentPreview: "package main",
		OldString:      "old code",
		NewString:      "new code",
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingFull)

	assert.NotNil(t, event.RawEvent)
	assert.NotEmpty(t, event.DiffContent)
	assert.NotEmpty(t, event.ConversationContext)

	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "package main", result.ContentPreview)
	assert.Equal(t, "old code", result.OldString)
	assert.Equal(t, "new code", result.NewString)
}

func TestApplyLoggingLevel_Full_Sensitive(t *testing.T) {
	event := newTestEvent(events.ActionFileWrite)
	event.IsSensitive = true

	payload := events.FileWritePayload{
		Path:           "/tmp/.env",
		ContentPreview: "SECRET=xxx",
		OldString:      "old",
		NewString:      "new",
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingFull)

	assert.Nil(t, event.RawEvent)
	assert.Empty(t, event.DiffContent)
	assert.Empty(t, event.ConversationContext)

	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "SECRET=xxx", result.ContentPreview)
}

func TestApplyLoggingLevel_Standard(t *testing.T) {
	event := newTestEvent(events.ActionFileWrite)

	payload := events.FileWritePayload{
		Path:           "/tmp/test.go",
		ContentPreview: "package main",
		OldString:      "old code",
		NewString:      "new code",
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingStandard)

	assert.Nil(t, event.RawEvent)
	assert.Empty(t, event.DiffContent)
	assert.Empty(t, event.ConversationContext)

	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "package main", result.ContentPreview)
	assert.Equal(t, "old code", result.OldString)
	assert.Equal(t, "new code", result.NewString)
}

func TestApplyLoggingLevel_Minimal_FileWrite(t *testing.T) {
	event := newTestEvent(events.ActionFileWrite)

	payload := events.FileWritePayload{
		Path:           "/tmp/test.go",
		ContentPreview: "package main",
		OldString:      "old code",
		NewString:      "new code",
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingMinimal)

	assert.Nil(t, event.RawEvent)
	assert.Empty(t, event.DiffContent)
	assert.Empty(t, event.ConversationContext)

	var result events.FileWritePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "/tmp/test.go", result.Path)
	assert.Empty(t, result.ContentPreview)
	assert.Empty(t, result.OldString)
	assert.Empty(t, result.NewString)
}

func TestApplyLoggingLevel_Minimal_CommandExec(t *testing.T) {
	event := newTestEvent(events.ActionCommandExec)

	payload := events.CommandExecPayload{
		Command:  "npm install",
		Output:   "lots of output here",
		ExitCode: 0,
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingMinimal)

	assert.Nil(t, event.RawEvent)

	var result events.CommandExecPayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "npm install", result.Command)
	assert.Equal(t, 0, result.ExitCode)
	assert.Empty(t, result.Output)
}

func TestApplyLoggingLevel_Minimal_ToolUse(t *testing.T) {
	event := newTestEvent(events.ActionToolUse)

	payload := events.ToolUsePayload{
		ToolName:      "web_search",
		Input:         json.RawMessage(`{"query": "sensitive search"}`),
		Output:        json.RawMessage(`{"results": ["secret data"]}`),
		OutputPreview: "secret data preview",
	}
	require.NoError(t, event.SetPayload(payload))

	ApplyLoggingLevel(event, config.LoggingMinimal)

	assert.Nil(t, event.RawEvent)
	assert.Empty(t, event.DiffContent)
	assert.Empty(t, event.ConversationContext)

	var result events.ToolUsePayload
	require.NoError(t, json.Unmarshal(event.Payload, &result))
	assert.Equal(t, "web_search", result.ToolName)
	assert.Nil(t, result.Input)
	assert.Nil(t, result.Output)
	assert.Empty(t, result.OutputPreview)
}

func TestApplyLoggingLevel_Minimal_FileRead(t *testing.T) {
	event := newTestEvent(events.ActionFileRead)

	payload := events.FileReadPayload{
		Path: "/tmp/test.go",
	}
	require.NoError(t, event.SetPayload(payload))

	originalPayload := make(json.RawMessage, len(event.Payload))
	copy(originalPayload, event.Payload)

	ApplyLoggingLevel(event, config.LoggingMinimal)

	assert.Nil(t, event.RawEvent)
	assert.Equal(t, json.RawMessage(originalPayload), event.Payload)
}

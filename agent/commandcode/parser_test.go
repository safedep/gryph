package commandcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read fixture: %s", name)
	return data
}

func testPrivacyChecker(t *testing.T) *events.PrivacyChecker {
	t.Helper()
	pc, err := events.NewPrivacyChecker(events.DefaultSensitivePatterns(), nil)
	require.NoError(t, err)
	return pc
}

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return New(testPrivacyChecker(t), config.LoggingStandard, true)
}

func testAdapterWithLevel(t *testing.T, level config.LoggingLevel) *Adapter {
	t.Helper()
	return New(testPrivacyChecker(t), level, true)
}

func TestParseHookEvent_PreToolUseShell(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_shell.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "shell_command", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/dev/project", event.WorkingDirectory)
	assert.Equal(t, "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", event.AgentSessionID)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "go test ./... -run TestParse", payload.Command,
		"args array must be folded back into the audited command line")
}

func TestParseHookEvent_PreToolUseRead(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_read.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "read_file", event.ToolName)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/dev/project/main.go", payload.Path,
		"read_file carries its target in absolute_path")
}

func TestParseHookEvent_PreToolUseWrite(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "write_file", event.ToolName)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/dev/project/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview, "package main")
	assert.Contains(t, event.FullContent, "package main")
}

func TestParseHookEvent_HookTypeCaptured(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, "PreToolUse", event.HookType, "raw hook type must be captured for phase mapping")
}

func TestParseHookEvent_PostToolUseEdit(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_edit.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PostToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "edit_file", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus,
		"a plain-string tool_response must not be treated as a parse failure")

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/dev/project/main.go", payload.Path)
	assert.Equal(t, "func main() {}", payload.OldString)
	assert.Contains(t, payload.NewString, "println")
	assert.Contains(t, event.FullContent, "println",
		"edit new_value beyond the preview boundary must reach the content matcher")
}

func TestParseHookEvent_Stop(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "stop.json")

	event, err := testAdapter(t).ParseEvent(ctx, "Stop", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "Stop", event.HookType)
	assert.Equal(t, AgentName, event.AgentName)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "SessionStart", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, "SessionStart", event.HookType)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)
}

func TestParseHookEvent_UnknownHookType(t *testing.T) {
	ctx := context.Background()
	data := []byte(`{"session_id":"s1","cwd":"/p","hook_event_name":"SomethingElse"}`)

	event, err := testAdapter(t).ParseEvent(ctx, "SomethingElse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionUnknown, event.ActionType)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	_, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", []byte("{invalid"))
	assert.Error(t, err)
}

func TestParseHookEvent_NonUUIDSessionID(t *testing.T) {
	ctx := context.Background()
	data := []byte(`{"session_id":"not-a-uuid","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"shell_command","tool_input":{"command":"ls"}}`)

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, "not-a-uuid", event.AgentSessionID)
}

func TestParseHookEvent_SensitivePathDetection(t *testing.T) {
	ctx := context.Background()
	data := []byte(`{"session_id":"s1","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"read_file","tool_input":{"absolute_path":"/home/user/.env"}}`)

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.True(t, event.IsSensitive, "reads of sensitive paths must be flagged")
}

func TestParseHookEvent_FullLoggingGeneratesDiff(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_edit.json")

	event, err := testAdapterWithLevel(t, config.LoggingFull).ParseEvent(ctx, "PostToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Contains(t, event.DiffContent, "println")
}

func TestToolNameMapping(t *testing.T) {
	assert.Equal(t, events.ActionCommandExec, ToolNameMapping["shell_command"])
	assert.Equal(t, events.ActionFileRead, ToolNameMapping["read_file"])
	assert.Equal(t, events.ActionFileWrite, ToolNameMapping["write_file"])
	assert.Equal(t, events.ActionFileWrite, ToolNameMapping["edit_file"])
}

func TestUnmarshalToolResponseString(t *testing.T) {
	// PostToolUse tool_response is a plain string in Command Code; the
	// wrapper must turn it into an object without losing the content.
	raw := []byte(`{"session_id":"s1","cwd":"/p","hook_event_name":"PostToolUse","tool_name":"shell_command","tool_input":{"command":"ls"},"tool_response":"file1\nfile2"}`)

	wrapped, err := wrapToolResponse(raw)
	require.NoError(t, err)

	var input PostToolUseInput
	require.NoError(t, json.Unmarshal(wrapped, &input))
	assert.Equal(t, "file1\nfile2", input.ToolResponse["output"])
}

package piagent

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

func TestParseHookEvent_ToolCall_Read(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_read.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "read", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.Equal(t, "pi-session-abc", event.AgentSessionID)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_ToolCall_Write(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "write", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview, "package main")
}

func TestParseHookEvent_ToolCall_Edit_WithOldTextNewText(t *testing.T) {
	// Test that edit tool with oldText/newText fields (Pi Agent format) is parsed correctly
	ctx := context.Background()
	data := loadFixture(t, "tool_call_edit_oldtext.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "edit", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Equal(t, "func main() {\n    fmt.Println(\"hello\")\n}", payload.OldString)
	assert.Equal(t, "func main() {\n    fmt.Println(\"hello world\")\n}", payload.NewString)
	// Lines: old has 3 lines, new has 3 lines but only the middle line differs
	assert.Equal(t, 1, payload.LinesRemoved)
	assert.Equal(t, 1, payload.LinesAdded)
}

func TestParseHookEvent_ToolCall_Bash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_bash.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "bash", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
	assert.Equal(t, "Install dependencies", payload.Description)
}

func TestParseHookEvent_ToolResult_Success(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_result_success.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_result", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "read", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_ToolResult_Error(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_result_error.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_result", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "bash", event.ToolName)
	assert.Equal(t, events.ResultError, event.ResultStatus)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "session_start", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload)
}

func TestParseHookEvent_SessionShutdown(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_shutdown.json")

	event, err := testAdapter(t).ParseEvent(ctx, "session_shutdown", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload)
}

func TestParseHookEvent_SessionIDDeterministic(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_bash.json")
	adapter := testAdapter(t)

	event1, err := adapter.ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestToolNameMapping(t *testing.T) {
	testCases := []struct {
		toolName   string
		actionType events.ActionType
	}{
		{"read", events.ActionFileRead},
		{"write", events.ActionFileWrite},
		{"edit", events.ActionFileWrite},
		{"bash", events.ActionCommandExec},
		{"grep", events.ActionFileRead},
		{"find", events.ActionFileRead},
		{"ls", events.ActionFileRead},
		{"unknown_tool", events.ActionToolUse},
	}

	for _, tc := range testCases {
		t.Run(tc.toolName, func(t *testing.T) {
			result := getActionType(tc.toolName)
			assert.Equal(t, tc.actionType, result)
		})
	}
}

func TestHookResponse_ExitCodes(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		exitCode int
	}{
		{"Allow", NewAllowResponse(), 0},
		{"Block", NewBlockResponse("blocked"), 2},
		{"Error", NewErrorResponse("error"), 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.exitCode, tc.response.ExitCode())
		})
	}
}

func TestHookResponse_JSON(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		expected map[string]string
	}{
		{
			"Allow",
			NewAllowResponse(),
			map[string]string{"decision": "allow"},
		},
		{
			"Block",
			NewBlockResponse("security violation"),
			map[string]string{"decision": "block", "reason": "security violation"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonData := tc.response.JSON()
			var result map[string]string
			err := json.Unmarshal(jsonData, &result)
			require.NoError(t, err)
			assert.Equal(t, tc.expected["decision"], result["decision"])
			if reason, ok := tc.expected["reason"]; ok {
				assert.Equal(t, reason, result["reason"])
			}
		})
	}
}

func TestHookResponse_Stderr(t *testing.T) {
	allow := NewAllowResponse()
	assert.Empty(t, allow.Stderr())

	block := NewBlockResponse("blocked reason")
	assert.Equal(t, "blocked reason", block.Stderr())

	errResp := NewErrorResponse("error message")
	assert.Equal(t, "error message", errResp.Stderr())
}

func TestParseHookEvent_ContentHash_Enabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.NotEmpty(t, payload.ContentHash, "ContentHash should be populated when content hashing is enabled")
	assert.Len(t, payload.ContentHash, 64, "ContentHash should be a SHA-256 hex string")
}

func TestParseHookEvent_ContentHash_Disabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_write.json")

	adapter := New(testPrivacyChecker(t), config.LoggingStandard, false)
	event, err := adapter.ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Empty(t, payload.ContentHash, "ContentHash should be empty when content hashing is disabled")
}

func TestParseHookEvent_DiffGeneration_FullLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_write.json")

	event, err := testAdapterWithLevel(t, config.LoggingFull).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.NotEmpty(t, event.DiffContent, "DiffContent should be populated at full logging level")
	assert.Contains(t, event.DiffContent, "--- a/")
	assert.Contains(t, event.DiffContent, "+++ b/")
}

func TestParseHookEvent_NoDiff_StandardLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Empty(t, event.DiffContent, "DiffContent should be empty at standard logging level")
}

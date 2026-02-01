package openclaw

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

func TestParseHookEvent_BeforeToolCall_Read(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_read.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "read", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "openclaw-session-abc123", event.AgentSessionID)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_BeforeToolCall_Write(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "write", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview, "package main")
}

func TestParseHookEvent_BeforeToolCall_Exec(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_exec.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "exec", event.ToolName)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
	assert.Equal(t, "Install dependencies", payload.Description)
}

func TestParseHookEvent_AfterToolCall_Success(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_tool_call_read.json")

	event, err := testAdapter(t).ParseEvent(ctx, "after_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "read", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_AfterToolCall_Error(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_tool_call_error.json")

	event, err := testAdapter(t).ParseEvent(ctx, "after_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "exec", event.ToolName)
	assert.Equal(t, events.ResultError, event.ResultStatus)
}

func TestParseHookEvent_BeforeToolCall_ToolUse(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_web_search.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "web_search", event.ToolName)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "session_start", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, "openclaw-session-abc123", event.AgentSessionID)
	assert.NotEmpty(t, event.Payload)
}

func TestParseHookEvent_SessionEnd(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_end.json")

	event, err := testAdapter(t).ParseEvent(ctx, "session_end", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "openclaw-session-abc123", event.AgentSessionID)
	assert.NotEmpty(t, event.Payload)
}

func TestParseHookEvent_SessionIDDeterministic(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")
	adapter := testAdapter(t)

	event1, err := adapter.ParseEvent(ctx, "session_start", data)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "session_start", data)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_SessionIDFromEnv(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	t.Setenv("OPENCLAW_SESSION_ID", "env-session-override")

	event, err := testAdapter(t).ParseEvent(ctx, "session_start", data)
	require.NoError(t, err)

	expected := resolveSessionID("env-session-override")
	assert.Equal(t, expected, event.SessionID)
}

func TestParseHookEvent_SessionIDFallback(t *testing.T) {
	sessionID := resolveSessionID("")
	assert.NotEmpty(t, sessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestParseHookEvent_UnknownHookType(t *testing.T) {
	ctx := context.Background()
	data := []byte(`{"hook_type": "unknown"}`)

	event, err := testAdapter(t).ParseEvent(ctx, "unknown", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionUnknown, event.ActionType)
}

func TestToolNameMapping(t *testing.T) {
	testCases := []struct {
		toolName   string
		actionType events.ActionType
	}{
		{"read", events.ActionFileRead},
		{"grep", events.ActionFileRead},
		{"glob", events.ActionFileRead},
		{"list", events.ActionFileRead},
		{"write", events.ActionFileWrite},
		{"edit", events.ActionFileWrite},
		{"exec", events.ActionCommandExec},
		{"bash", events.ActionCommandExec},
		{"message", events.ActionToolUse},
		{"browser", events.ActionToolUse},
		{"web_search", events.ActionToolUse},
		{"web_fetch", events.ActionToolUse},
		{"image", events.ActionToolUse},
		{"cron", events.ActionToolUse},
		{"tts", events.ActionToolUse},
		{"canvas", events.ActionToolUse},
		{"sessions_spawn", events.ActionToolUse},
		{"agents_list", events.ActionToolUse},
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
	data := loadFixture(t, "before_tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.NotEmpty(t, payload.ContentHash, "ContentHash should be populated when content hashing is enabled")
	assert.Len(t, payload.ContentHash, 64, "ContentHash should be a SHA-256 hex string")
}

func TestParseHookEvent_ContentHash_Disabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_write.json")

	adapter := New(testPrivacyChecker(t), config.LoggingStandard, false)
	event, err := adapter.ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Empty(t, payload.ContentHash, "ContentHash should be empty when content hashing is disabled")
}

func TestParseHookEvent_DiffGeneration_FullLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_write.json")

	event, err := testAdapterWithLevel(t, config.LoggingFull).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.NotEmpty(t, event.DiffContent, "DiffContent should be populated at full logging level")
	assert.Contains(t, event.DiffContent, "--- a/")
	assert.Contains(t, event.DiffContent, "+++ b/")
}

func TestParseHookEvent_NoDiff_StandardLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_tool_call_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "before_tool_call", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Empty(t, event.DiffContent, "DiffContent should be empty at standard logging level")
}

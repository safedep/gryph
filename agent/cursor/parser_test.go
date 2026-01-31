package cursor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

func TestParseHookEvent_PreToolUseShell(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_shell.json")

	event, err := ParseHookEvent(ctx, "preToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Shell", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.Equal(t, "conv-test-123", event.AgentSessionID)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
}

func TestParseHookEvent_PreToolUseWrite(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := ParseHookEvent(ctx, "preToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "Write", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview, "package main")
}

func TestParseHookEvent_PostToolUseRead(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_read.json")

	event, err := ParseHookEvent(ctx, "postToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "Read", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_PostToolUseFailure(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_failure.json")

	event, err := ParseHookEvent(ctx, "postToolUseFailure", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Shell", event.ToolName)
	assert.Equal(t, events.ResultError, event.ResultStatus)
	assert.Contains(t, event.ErrorMessage, "Command failed")
}

func TestParseHookEvent_BeforeShellExecution(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_shell_execution.json")

	event, err := ParseHookEvent(ctx, "beforeShellExecution", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Shell", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "ls -la", payload.Command)
}

func TestParseHookEvent_BeforeReadFile(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_read_file.json")

	event, err := ParseHookEvent(ctx, "beforeReadFile", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "Read", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/.env", payload.Path)

	assert.True(t, event.IsSensitive, ".env should be marked sensitive")
}

func TestParseHookEvent_AfterFileEdit(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_file_edit.json")

	event, err := ParseHookEvent(ctx, "afterFileEdit", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "Edit", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Equal(t, "fmt.Println(\"hello\")", payload.OldString)
	assert.Equal(t, "fmt.Println(\"world\")", payload.NewString)
}

func TestParseHookEvent_BeforeSubmitPrompt(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_submit_prompt.json")

	event, err := ParseHookEvent(ctx, "beforeSubmitPrompt", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "beforeSubmitPrompt", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := ParseHookEvent(ctx, "sessionStart", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload)

	var payload events.SessionPayload
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "claude-3-opus", payload.Model)
}

func TestParseHookEvent_SessionEnd(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_end.json")

	event, err := ParseHookEvent(ctx, "sessionEnd", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload)

	var payload events.SessionEndPayload
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "completed", payload.Reason)
}

func TestParseHookEvent_Stop(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "stop.json")

	event, err := ParseHookEvent(ctx, "stop", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	var payload events.SessionEndPayload
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "completed", payload.Reason)
}

func TestParseHookEvent_SessionIDDeterministic(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_shell.json")

	event1, err := ParseHookEvent(ctx, "preToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)

	event2, err := ParseHookEvent(ctx, "preToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := ParseHookEvent(ctx, "preToolUse", data, testPrivacyChecker(t))
	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestToolNameMapping(t *testing.T) {
	testCases := []struct {
		toolName   string
		actionType events.ActionType
	}{
		{"Shell", events.ActionCommandExec},
		{"Read", events.ActionFileRead},
		{"Write", events.ActionFileWrite},
		{"Edit", events.ActionFileWrite},
		{"Grep", events.ActionFileRead},
		{"Glob", events.ActionFileRead},
		{"Task", events.ActionToolUse},
		{"UnknownTool", events.ActionToolUse},
	}

	for _, tc := range testCases {
		t.Run(tc.toolName, func(t *testing.T) {
			result, ok := ToolNameToActionType[tc.toolName]
			if tc.toolName == "UnknownTool" {
				assert.False(t, ok)
			} else {
				assert.True(t, ok)
				assert.Equal(t, tc.actionType, result)
			}
		})
	}
}

func TestHookResponse(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		decision HookDecision
	}{
		{"Allow", NewAllowResponse(), HookAllow},
		{"Deny", NewDenyResponse("blocked"), HookDeny},
		{"Ask", NewAskResponse("confirm?"), HookAsk},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.decision, tc.response.Decision)
		})
	}
}

func TestGeneratePreToolUseResponse(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		expected map[string]interface{}
	}{
		{
			"Allow",
			NewAllowResponse(),
			map[string]interface{}{"decision": "allow"},
		},
		{
			"Deny",
			NewDenyResponse("not permitted"),
			map[string]interface{}{"decision": "deny", "reason": "not permitted"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := GeneratePreToolUseResponse(tc.response)
			var result map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &result))
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGeneratePermissionResponse(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		expected map[string]interface{}
	}{
		{
			"Allow",
			NewAllowResponse(),
			map[string]interface{}{"permission": "allow"},
		},
		{
			"Deny",
			NewDenyResponse("blocked"),
			map[string]interface{}{"permission": "deny", "user_message": "blocked"},
		},
		{
			"Ask",
			NewAskResponse("confirm?"),
			map[string]interface{}{"permission": "ask", "user_message": "confirm?"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := GeneratePermissionResponse(tc.response)
			var result map[string]interface{}
			require.NoError(t, json.Unmarshal(data, &result))
			assert.Equal(t, tc.expected, result)
		})
	}
}

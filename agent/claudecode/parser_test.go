package claudecode

import (
	"context"
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

func TestParseHookEvent_PreToolUseBash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_bash.json")

	event, err := ParseHookEvent(ctx, "PreToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.Equal(t, "test-session-123", event.AgentSessionID)

	// Check payload
	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
	assert.Equal(t, "Install dependencies", payload.Description)
}

func TestParseHookEvent_PreToolUseWrite(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := ParseHookEvent(ctx, "PreToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "Write", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	// Check payload
	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview, "package main")
}

func TestParseHookEvent_PostToolUseRead(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_read.json")

	event, err := ParseHookEvent(ctx, "PostToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, "Read", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	// Check payload
	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/README.md", payload.Path)
}

func TestParseHookEvent_PostToolUseFailure(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_failure.json")

	event, err := ParseHookEvent(ctx, "PostToolUseFailure", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, events.ResultError, event.ResultStatus)
	assert.Contains(t, event.ErrorMessage, "Command failed")
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := ParseHookEvent(ctx, "SessionStart", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload, "Payload should not be empty")
}

func TestParseHookEvent_SessionEnd(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_end.json")

	event, err := ParseHookEvent(ctx, "SessionEnd", data, testPrivacyChecker(t))
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.NotEmpty(t, event.Payload, "Payload should not be empty")
}

func TestParseHookEvent_SessionIDDeterministic(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_bash.json")

	// Parse twice
	event1, err := ParseHookEvent(ctx, "PreToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)

	event2, err := ParseHookEvent(ctx, "PreToolUse", data, testPrivacyChecker(t))
	require.NoError(t, err)

	// Same agent session ID should produce same session ID
	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := ParseHookEvent(ctx, "PreToolUse", data, testPrivacyChecker(t))
	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestToolNameMapping(t *testing.T) {
	testCases := []struct {
		toolName   string
		actionType events.ActionType
	}{
		{"Read", events.ActionFileRead},
		{"View", events.ActionFileRead},
		{"Write", events.ActionFileWrite},
		{"Edit", events.ActionFileWrite},
		{"Bash", events.ActionCommandExec},
		{"Execute", events.ActionCommandExec},
		{"Grep", events.ActionFileRead},
		{"Glob", events.ActionFileRead},
		{"WebSearch", events.ActionToolUse},
		{"UnknownTool", events.ActionToolUse},
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

func TestHookResponse_Stderr(t *testing.T) {
	allow := NewAllowResponse()
	assert.Empty(t, allow.Stderr())

	block := NewBlockResponse("blocked reason")
	assert.Equal(t, "blocked reason", block.Stderr())

	errResp := NewErrorResponse("error message")
	assert.Equal(t, "error message", errResp.Stderr())
}

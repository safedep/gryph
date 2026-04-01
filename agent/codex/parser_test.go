package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
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

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "SessionStart", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionStart, event.ActionType)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "codex-session-abc", event.AgentSessionID)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload := events.SessionPayload{}
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "startup", payload.Source)
	assert.Equal(t, "o4-mini", payload.Model)
}

func TestParseHookEvent_PreToolUse_Bash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_bash.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PreToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
}

func TestParseHookEvent_PostToolUse_Bash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_bash.json")

	event, err := testAdapter(t).ParseEvent(ctx, "PostToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Bash", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
	assert.Contains(t, payload.Output, "added 150 packages")
}

func TestParseHookEvent_UserPromptSubmit(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "user_prompt_submit.json")

	event, err := testAdapter(t).ParseEvent(ctx, "UserPromptSubmit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "UserPromptSubmit", event.ToolName)

	payload, err := event.GetToolUsePayload()
	require.NoError(t, err)
	assert.Equal(t, "UserPromptSubmit", payload.ToolName)
}

func TestParseHookEvent_Stop(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "stop.json")

	event, err := testAdapter(t).ParseEvent(ctx, "Stop", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionSessionEnd, event.ActionType)

	payload := events.SessionEndPayload{}
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "All tests are now passing.", payload.Reason)
}

func TestParseHookEvent_DeterministicSessionID(t *testing.T) {
	ctx := context.Background()
	adapter := testAdapter(t)

	data1 := loadFixture(t, "pre_tool_use_bash.json")
	data2 := loadFixture(t, "post_tool_use_bash.json")

	event1, err := adapter.ParseEvent(ctx, "PreToolUse", data1)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "PostToolUse", data2)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)

	expected := uuid.NewSHA1(uuid.NameSpaceOID, []byte("codex-session-abc"))
	assert.Equal(t, expected, event1.SessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	adapter := testAdapter(t)

	_, err := adapter.ParseEvent(ctx, "SessionStart", []byte("not-json"))
	assert.Error(t, err)
}

func TestHookResponse_Allow(t *testing.T) {
	resp := NewAllowResponse()
	data := resp.JSON()

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "allow", hookOutput["permissionDecision"])
}

func TestHookResponse_Block(t *testing.T) {
	resp := NewBlockResponse("dangerous command")
	data := resp.JSON()

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "dangerous command", hookOutput["permissionDecisionReason"])
}

func TestHookResponse_ExitCodes(t *testing.T) {
	assert.Equal(t, 0, NewAllowResponse().ExitCode())
	assert.Equal(t, 2, NewBlockResponse("reason").ExitCode())
	assert.Equal(t, 1, NewErrorResponse("error").ExitCode())
}

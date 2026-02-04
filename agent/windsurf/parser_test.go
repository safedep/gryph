package windsurf

import (
	"context"
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

func TestParseHookEvent_PreReadCode(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_read_code.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_read_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileRead, event.ActionType)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "traj-test-123", event.AgentSessionID)

	payload, err := event.GetFileReadPayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/.env", payload.Path)

	assert.True(t, event.IsSensitive, ".env should be marked sensitive")
}

func TestParseHookEvent_PreWriteCode(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_write_code.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "pre_write_code", event.ToolName)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Equal(t, "fmt.Println(\"hello\")", payload.OldString)
	assert.Equal(t, "fmt.Println(\"world\")", payload.NewString)
}

func TestParseHookEvent_PostWriteCode(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_write_code.json")

	event, err := testAdapter(t).ParseEvent(ctx, "post_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
}

func TestParseHookEvent_PreRunCommand(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_run_command.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_run_command", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command)
}

func TestParseHookEvent_PreMCPToolUse(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_mcp_tool_use.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_mcp_tool_use", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "create_issue", event.ToolName)
}

func TestParseHookEvent_PreUserPrompt(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_user_prompt.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_user_prompt", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "pre_user_prompt", event.ToolName)
}

func TestParseHookEvent_PostCascadeResponse(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_cascade_response.json")

	event, err := testAdapter(t).ParseEvent(ctx, "post_cascade_response", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionNotification, event.ActionType)
	assert.Equal(t, "post_cascade_response", event.ToolName)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)
}

func TestParseHookEvent_PostSetupWorktree(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_setup_worktree.json")

	event, err := testAdapter(t).ParseEvent(ctx, "post_setup_worktree", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionToolUse, event.ActionType)
	assert.Equal(t, "post_setup_worktree", event.ToolName)
	assert.Equal(t, "/tmp/worktree-abc123", event.WorkingDirectory)
	assert.Equal(t, events.ResultSuccess, event.ResultStatus)
}

func TestParseHookEvent_SessionIDDeterministic(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_read_code.json")
	adapter := testAdapter(t)

	event1, err := adapter.ParseEvent(ctx, "pre_read_code", data)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "pre_read_code", data)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_read_code", data)
	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestParseHookEvent_ContentHash_Enabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_write_code.json")

	event, err := testAdapter(t).ParseEvent(ctx, "pre_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.NotEmpty(t, payload.ContentHash)
	assert.Len(t, payload.ContentHash, 64)
}

func TestParseHookEvent_ContentHash_Disabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_write_code.json")

	adapter := New(testPrivacyChecker(t), config.LoggingStandard, false)
	event, err := adapter.ParseEvent(ctx, "pre_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Empty(t, payload.ContentHash)
}

func TestParseHookEvent_DiffGeneration_FullLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_write_code.json")

	event, err := testAdapterWithLevel(t, config.LoggingFull).ParseEvent(ctx, "post_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.NotEmpty(t, event.DiffContent)
	assert.Contains(t, event.DiffContent, "--- a/")
	assert.Contains(t, event.DiffContent, "+++ b/")
}

func TestParseHookEvent_NoDiff_StandardLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_write_code.json")

	event, err := testAdapter(t).ParseEvent(ctx, "post_write_code", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Empty(t, event.DiffContent)
}

func TestHookTypeMapping(t *testing.T) {
	testCases := []struct {
		hookType   string
		actionType events.ActionType
	}{
		{"pre_read_code", events.ActionFileRead},
		{"post_read_code", events.ActionFileRead},
		{"pre_write_code", events.ActionFileWrite},
		{"post_write_code", events.ActionFileWrite},
		{"pre_run_command", events.ActionCommandExec},
		{"post_run_command", events.ActionCommandExec},
		{"pre_mcp_tool_use", events.ActionToolUse},
		{"post_mcp_tool_use", events.ActionToolUse},
		{"pre_user_prompt", events.ActionToolUse},
		{"post_cascade_response", events.ActionNotification},
		{"post_setup_worktree", events.ActionToolUse},
	}

	for _, tc := range testCases {
		t.Run(tc.hookType, func(t *testing.T) {
			result, ok := HookTypeMapping[tc.hookType]
			assert.True(t, ok)
			assert.Equal(t, tc.actionType, result)
		})
	}
}

func TestHookResponse(t *testing.T) {
	testCases := []struct {
		name     string
		response *HookResponse
		decision HookDecision
		exitCode int
	}{
		{"Allow", NewAllowResponse(), HookAllow, 0},
		{"Block", NewBlockResponse("blocked"), HookBlock, 2},
		{"Error", NewErrorResponse("error"), HookError, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.decision, tc.response.Decision)
			assert.Equal(t, tc.exitCode, tc.response.ExitCode())
		})
	}
}

func TestIsPreHook(t *testing.T) {
	assert.True(t, isPreHook("pre_read_code"))
	assert.True(t, isPreHook("pre_write_code"))
	assert.True(t, isPreHook("pre_run_command"))
	assert.True(t, isPreHook("pre_mcp_tool_use"))
	assert.True(t, isPreHook("pre_user_prompt"))
	assert.False(t, isPreHook("post_read_code"))
	assert.False(t, isPreHook("post_cascade_response"))
	assert.False(t, isPreHook("post_setup_worktree"))
}

package cursor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
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

func testPrivacyChecker(t *testing.T) *privacy.Redactor {
	t.Helper()

	pc, err := privacy.NewRedactor(privacy.DefaultSensitivePatterns(), nil)
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

	event, err := testAdapter(t).ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Shell", event.ToolName)
	assert.Equal(t, AgentName, event.AgentName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
	assert.Equal(t, "conv-test-123", event.AgentSessionID)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "npm install", payload.Command.Value)
}

func TestParseHookEvent_PreToolUseWrite(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "Write", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Contains(t, payload.ContentPreview.Value, "package main")
}

func TestParseHookEvent_PostToolUseRead(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "post_tool_use_read.json")

	event, err := testAdapter(t).ParseEvent(ctx, "postToolUse", data)
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

	event, err := testAdapter(t).ParseEvent(ctx, "postToolUseFailure", data)
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

	event, err := testAdapter(t).ParseEvent(ctx, "beforeShellExecution", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionCommandExec, event.ActionType)
	assert.Equal(t, "Shell", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetCommandExecPayload()
	require.NoError(t, err)
	assert.Equal(t, "ls -la", payload.Command.Value)
}

func TestParseHookEvent_BeforeReadFile(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_read_file.json")

	event, err := testAdapter(t).ParseEvent(ctx, "beforeReadFile", data)
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

	event, err := testAdapter(t).ParseEvent(ctx, "afterFileEdit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionFileWrite, event.ActionType)
	assert.Equal(t, "Edit", event.ToolName)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project/src/main.go", payload.Path)
	assert.Equal(t, "fmt.Println(\"hello\")", payload.OldString.Value)
	assert.Equal(t, "fmt.Println(\"world\")", payload.NewString.Value)
}

func TestParseHookEvent_BeforeSubmitPrompt(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "before_submit_prompt.json")

	event, err := testAdapter(t).ParseEvent(ctx, "beforeSubmitPrompt", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Equal(t, events.ActionUserPrompt, event.ActionType)
	payload, err := event.GetUserPromptPayload()
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Equal(t, "Fix the build errors", payload.Prompt.Value)
	assert.Equal(t, privacy.OriginUser, payload.Prompt.Label.Origin)
	assert.Equal(t, "Fix the build errors", event.FullContent)
	assert.Equal(t, "/home/user/project", event.WorkingDirectory)
}

func TestParseHookEvent_SessionStart(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "session_start.json")

	event, err := testAdapter(t).ParseEvent(ctx, "sessionStart", data)
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

	event, err := testAdapter(t).ParseEvent(ctx, "sessionEnd", data)
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

	event, err := testAdapter(t).ParseEvent(ctx, "stop", data)
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
	adapter := testAdapter(t)

	event1, err := adapter.ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)

	event2, err := adapter.ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)

	assert.Equal(t, event1.SessionID, event2.SessionID)
	assert.Equal(t, event1.AgentSessionID, event2.AgentSessionID)
}

func TestParseHookEvent_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	data := []byte("not valid json")

	event, err := testAdapter(t).ParseEvent(ctx, "preToolUse", data)
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

func TestParseHookEvent_ContentHash_Enabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	event, err := testAdapter(t).ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.NotEmpty(t, payload.ContentHash, "ContentHash should be populated when content hashing is enabled")
	assert.Len(t, payload.ContentHash, 64, "ContentHash should be a SHA-256 hex string")
}

func TestParseHookEvent_ContentHash_Disabled(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "pre_tool_use_write.json")

	adapter := New(testPrivacyChecker(t), config.LoggingStandard, false)
	event, err := adapter.ParseEvent(ctx, "preToolUse", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.Empty(t, payload.ContentHash, "ContentHash should be empty when content hashing is disabled")
}

func TestParseHookEvent_AfterFileEdit_ContentHash(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_file_edit.json")

	event, err := testAdapter(t).ParseEvent(ctx, "afterFileEdit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	payload, err := event.GetFileWritePayload()
	require.NoError(t, err)
	assert.NotEmpty(t, payload.ContentHash, "ContentHash should be populated for afterFileEdit")
	assert.Len(t, payload.ContentHash, 64)
}

func TestParseHookEvent_AfterFileEdit_DiffGeneration_FullLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_file_edit.json")

	event, err := testAdapterWithLevel(t, config.LoggingFull).ParseEvent(ctx, "afterFileEdit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.NotEmpty(t, event.DiffContent.Value, "DiffContent should be populated at full logging level")
	assert.Contains(t, event.DiffContent.Value, "--- a/")
	assert.Contains(t, event.DiffContent.Value, "+++ b/")
}

func TestParseHookEvent_AfterFileEdit_NoDiff_StandardLevel(t *testing.T) {
	ctx := context.Background()
	data := loadFixture(t, "after_file_edit.json")

	event, err := testAdapter(t).ParseEvent(ctx, "afterFileEdit", data)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.Empty(t, event.DiffContent.Value, "DiffContent should be empty at standard logging level")
}

func TestNewGuidanceResponse(t *testing.T) {
	resp := NewGuidanceResponse("advisory text")
	assert.Equal(t, HookAllow, resp.Decision)
	assert.Equal(t, "advisory text", resp.Reason)
}

func TestGeneratePermissionResponse_AllowWithMessage(t *testing.T) {
	resp := NewGuidanceResponse("be careful with .env")
	data := GeneratePermissionResponse(resp)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "allow", result["permission"])
	assert.Equal(t, "be careful with .env", result["user_message"])
}

func TestGenerateContinueResponse_GuidanceCarriesMessage(t *testing.T) {
	resp := NewGuidanceResponse("note this")
	data := GenerateContinueResponse(true, resp.Reason)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, true, result["continue"])
	assert.Equal(t, "note this", result["user_message"])
}

func TestMCPSource(t *testing.T) {
	cases := []struct {
		url, command, want string
	}{
		{"https://MCP.Example.com/sse", "", "mcp.example.com"},
		{"https://mcp.example.com/evil/sse", "", "mcp.example.com/evil"},
		{"https://mcp.example.com/good/sse", "", "mcp.example.com/good"},
		{"https://mcp.example.com/mcp", "", "mcp.example.com"},
		{"http://localhost:3001/", "", "localhost:3001"},
		{"https://evil.example./sse", "", "evil.example"},
		{"https://evil.example:443/", "", "evil.example"},
		{"https://Evil.Example.:443/sse", "", "evil.example"},
		{"http://evil.example:80/x/sse", "", "evil.example/x"},
		{"https://evil.example:80/", "", "evil.example:80"},
		{"https://[::1]:443/sse", "", "[::1]"},
		{"http://[::1]:8080/sse", "", "[::1]:8080"},
		{"", "/usr/local/bin/github-mcp-server stdio", "/usr/local/bin/github-mcp-server"},
		{"", "/tmp/evil/github-mcp-server", "/tmp/evil/github-mcp-server"},
		{"", "./bin/server", "./bin/server"},
		{"", "github-mcp-server stdio", "github-mcp-server"},
		{"", "npx -y @evil/mcp-server", "@evil/mcp-server"},
		{"", "npx -y @modelcontextprotocol/server-github@1.2.0", "@modelcontextprotocol/server-github"},
		{"", "npx --package pkg -y mcp-bin --port 3", "pkg"},
		{"", "npx -p @evil/pkg github-mcp-server", "@evil/pkg"},
		{"", "npx --package=@evil/x@2.0 good-server", "@evil/x"},
		{"", "npx -p good -p @evil/x good-server", "npx"},
		{"", "npx good-server -p @evil/x", "good-server"},
		{"", "npm exec --package=@evil/x -- good-server", "@evil/x"},
		{"", "uvx --from evil good", "evil"},
		{"", "uvx --from evil==1.0 good", "evil"},
		{"", "pipx run --spec evil good", "evil"},
		{"", "uvx -p 3.12 mcp-server-time", "mcp-server-time"},
		{"", `"C:\Program Files\nodejs\npx.cmd" -y server-x`, "server-x"},
		{"", "npx -y", "npx"},
		{"", "bunx server-y@latest", "server-y"},
		{"", "npx -y good@file:/tmp/evil", "good@file:/tmp/evil"},
		{"", "npx -y good@https://evil.example/x.tgz", "good@https://evil.example/x.tgz"},
		{"", "npx -y good@npm:evil", "good@npm:evil"},
		{"", "npx -y @scope/good@1.2.3", "@scope/good"},
		{"", "pnpm dlx @scope/server", "@scope/server"},
		{"", "npm exec -- @scope/server", "@scope/server"},
		{"", "uvx mcp-server-git==0.6.2 --repository .", "mcp-server-git"},
		{"", "uvx --from git+https://x/y mcp-server-fetch", "git+https://x/y"},
		{"", "uv --directory /srv/weather run weather-server", "weather-server"},
		{"", "uv tool run mcp-server-time", "mcp-server-time"},
		{"", "pipx run mcp-server-sqlite[all]", "mcp-server-sqlite"},
		{"", "python3.12 -u -m mcp_server_time --tz UTC", "mcp_server_time"},
		{"", "python -m", "python"},
		{"", "node --require ./hook.js /srv/evil/index.js", "/srv/evil/index.js"},
		{"", "deno run --allow-net jsr:@scope/server", "jsr:@scope/server"},
		{"", "docker run -i --rm -e GITHUB_TOKEN ghcr.io/github/github-mcp-server:v1", "ghcr.io/github/github-mcp-server"},
		{"", "docker run -it --name x -v /a:/b localhost:5000/evil/mcp@sha256:abc", "localhost:5000/evil/mcp"},
		{"", "docker run --env=A=1 --rm", "docker"},
		{"", "docker ps", "docker"},
		{"", `npx -y "@evil/mcp server"`, "@evil/mcp server"},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url+tc.command, func(t *testing.T) {
			assert.Equal(t, tc.want, MCPServer{URL: tc.url, Command: tc.command}.source())
		})
	}
}

func TestParseMCPExecution_Origin(t *testing.T) {
	cases := []struct {
		hook, server, want string
	}{
		{"beforeMCPExecution", `"url":"https://mcp.example.com/sse"`, "mcp.example.com"},
		{"afterMCPExecution", `"url":"https://mcp.example.com/sse"`, "mcp.example.com"},
		{"afterMCPExecution", `"command":"/usr/bin/github-mcp-server stdio"`, "/usr/bin/github-mcp-server"},
		{"beforeMCPExecution", `"command":"npx -y @evil/mcp-server"`, "@evil/mcp-server"},
		{"afterMCPExecution", `"url":"https://mcp.example.com/evil/sse"`, "mcp.example.com/evil"},
		{"afterMCPExecution", `"duration":5`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.hook+"/"+tc.want, func(t *testing.T) {
			data := []byte(`{"conversation_id":"c","generation_id":"g","hook_event_name":"` + tc.hook + `",
				"workspace_roots":["/work"],"tool_name":"search","tool_input":{"q":"x"},` + tc.server + `}`)
			event, err := testAdapter(t).ParseEvent(context.Background(), tc.hook, data)
			require.NoError(t, err)
			assert.Equal(t, privacy.OriginMCP, event.Origin)
			assert.Equal(t, tc.want, event.OriginSource)
		})
	}
}

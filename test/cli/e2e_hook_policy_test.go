package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockCommandPolicy returns a policy that blocks command_exec events whose
// command matches `pattern`. The block message embeds the marker so the test
// can assert the rule fired.
func blockCommandPolicy(ruleID, pattern, message string) string {
	return fmt.Sprintf(`version: "1"
rules:
  - id: %s
    action: block
    severity: high
    match:
      action_types: [command_exec]
      command_patterns:
        - %q
    message: %q
`, ruleID, pattern, message)
}

// guidanceFilePolicy returns a policy that emits guidance on file_read events
// whose path matches `glob`. The message embeds the marker so the test can
// assert the rule fired.
func guidanceFilePolicy(ruleID, glob, message string) string {
	return fmt.Sprintf(`version: "1"
rules:
  - id: %s
    action: guidance
    severity: medium
    match:
      action_types: [file_read]
      file_patterns:
        - %q
    message: %q
`, ruleID, glob, message)
}

func assertSingleEventStatus(t *testing.T, env *testEnv, expected events.ResultStatus) {
	t.Helper()
	store, cleanup := env.openStore()
	defer cleanup()
	evts, err := store.QueryEvents(context.Background(), events.NewEventFilter())
	require.NoError(t, err)
	require.Len(t, evts, 1, "expected exactly one persisted event")
	assert.Equal(t, expected, evts[0].ResultStatus, "unexpected ResultStatus on persisted event")
}

func TestPolicy_ClaudeCode_Block_DangerousCommand(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: refusing npm install",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/claudecode/testdata/pre_tool_use_bash.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("claude-code", "PreToolUse", payload)
	assertHookBlocked(t, stdout, stderr, runErr, "blocked-by-policy")
	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_ClaudeCode_Guidance_SensitivePath(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-env-read",
		"**/.env*",
		"guidance-advisory: heads up on .env",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/claudecode/testdata/pre_tool_use_read.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("claude-code", "PreToolUse", payload)
	assertHookGuidance(t, stdout, stderr, runErr, "guidance-advisory")
	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_Cursor_Block_DangerousCommand(t *testing.T) {
	policy := blockCommandPolicy(
		"block-ls-la",
		`^ls\s+-la$`,
		"blocked-by-policy: ls -la is gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/cursor/testdata/before_shell_execution.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("cursor", "beforeShellExecution", payload)

	require.Equal(t, 0, hookExitCode(t, runErr),
		"cursor block uses JSON response on stdout at exit 0, not exit 2 (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "deny", resp["permission"], "cursor permission hook should report deny on block")
	msg, ok := resp["user_message"].(string)
	require.True(t, ok, "user_message should be a string")
	assert.Contains(t, msg, "blocked-by-policy")

	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_Cursor_Guidance_SensitivePath(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-env-read",
		"**/.env*",
		"guidance-advisory: .env access noted",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/cursor/testdata/before_read_file.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("cursor", "beforeReadFile", payload)
	require.Equal(t, 0, hookExitCode(t, runErr),
		"cursor guidance is exit 0 with JSON on stdout (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "allow", resp["permission"], "cursor permission hook should report allow on guidance")
	msg, ok := resp["user_message"].(string)
	require.True(t, ok, "user_message should be a string")
	assert.Contains(t, msg, "guidance-advisory")

	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_Gemini_Block(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install-gemini",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: gemini npm install gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/gemini/testdata/before_tool_shell.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("gemini", "BeforeTool", payload)

	assert.Equal(t, 2, hookExitCode(t, runErr), "gemini block should exit 2 (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "block", resp["decision"], "gemini block should emit decision=block JSON")
	assert.Contains(t, resp["reason"], "blocked-by-policy")

	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_Gemini_Guidance(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-readme-read-gemini",
		"**/README*",
		"guidance-advisory: gemini readme noted",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/gemini/testdata/before_tool_read_file.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("gemini", "BeforeTool", payload)
	require.Equal(t, 0, hookExitCode(t, runErr),
		"gemini guidance is exit 0 with JSON on stdout (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "allow", resp["decision"], "gemini guidance should emit decision=allow JSON")
	assert.Contains(t, resp["reason"], "guidance-advisory")

	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_OpenCode_Block(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install-opencode",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: opencode npm install gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/opencode/testdata/tool_execute_before_bash.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("opencode", "tool.execute.before", payload)
	assert.Equal(t, 2, hookExitCode(t, runErr), "opencode block should exit 2 (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "block", resp["decision"], "opencode block should emit decision=block JSON")
	assert.Contains(t, resp["reason"], "blocked-by-policy")

	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_OpenCode_Guidance(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-readme-read-opencode",
		"**/README*",
		"guidance-advisory: opencode readme noted",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/opencode/testdata/tool_execute_before_read.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("opencode", "tool.execute.before", payload)
	require.Equal(t, 0, hookExitCode(t, runErr),
		"opencode guidance is exit 0 with JSON on stdout (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "allow", resp["decision"])
	assert.Contains(t, resp["reason"], "guidance-advisory")

	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_Windsurf_Block(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install-windsurf",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: windsurf npm install gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/windsurf/testdata/pre_run_command.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("windsurf", "pre_run_command", payload)
	assertHookBlocked(t, stdout, stderr, runErr, "blocked-by-policy")
	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_Windsurf_Guidance(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-env-read-windsurf",
		"**/.env*",
		"guidance-advisory: windsurf env noted",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/windsurf/testdata/pre_read_code.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("windsurf", "pre_read_code", payload)
	assertHookGuidance(t, stdout, stderr, runErr, "guidance-advisory")
	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_PiAgent_Block(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install-piagent",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: pi-agent npm install gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/piagent/testdata/tool_call_bash.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("pi-agent", "tool_call", payload)
	assert.Equal(t, 2, hookExitCode(t, runErr), "pi-agent block should exit 2 (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	assert.Equal(t, "block", resp["decision"], "pi-agent block should emit decision=block JSON")
	assert.Contains(t, resp["reason"], "blocked-by-policy")

	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_PiAgent_Guidance(t *testing.T) {
	policy := guidanceFilePolicy(
		"guide-readme-read-piagent",
		"**/README*",
		"guidance-advisory: pi-agent readme noted",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/piagent/testdata/tool_call_read.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("pi-agent", "tool_call", payload)
	require.Equal(t, 0, hookExitCode(t, runErr),
		"pi-agent guidance is exit 0 with JSON on stdout (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	assert.Equal(t, "allow", resp["decision"])
	assert.Contains(t, resp["reason"], "guidance-advisory")

	assertSingleEventStatus(t, env, events.ResultSuccess)
}

func TestPolicy_Codex_Block(t *testing.T) {
	policy := blockCommandPolicy(
		"block-npm-install-codex",
		`(?i)\bnpm\s+install\b`,
		"blocked-by-policy: codex npm install gated",
	)
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/codex/testdata/pre_tool_use_bash.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("codex", "PreToolUse", payload)

	assert.Equal(t, 2, hookExitCode(t, runErr), "codex block should exit 2 (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout should be a JSON object")
	hookOutput, ok := resp["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok, "codex block JSON should carry hookSpecificOutput")
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	reason, ok := hookOutput["permissionDecisionReason"].(string)
	require.True(t, ok, "permissionDecisionReason should be a string")
	assert.Contains(t, reason, "blocked-by-policy")

	assertSingleEventStatus(t, env, events.ResultBlocked)
}

func TestPolicy_Codex_Guidance(t *testing.T) {
	policy := `version: "1"
rules:
  - id: guide-codex-npm
    action: guidance
    severity: medium
    match:
      action_types: [command_exec]
      command_patterns:
        - '(?i)\bnpm\s+install\b'
    message: "guidance-advisory: codex consider lockfile"
`
	env := newTestEnvWithPolicy(t, policy)

	payload, err := os.ReadFile("../../agent/codex/testdata/pre_tool_use_bash.json")
	require.NoError(t, err)

	stdout, stderr, runErr := env.runHookCapturingStd("codex", "PreToolUse", payload)
	require.Equal(t, 0, hookExitCode(t, runErr),
		"codex guidance is exit 0 with JSON on stdout (stdout=%q stderr=%q err=%v)", stdout, stderr, runErr)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	hookOutput, ok := resp["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok, "codex guidance JSON should carry hookSpecificOutput")
	assert.Equal(t, "allow", hookOutput["permissionDecision"])
	reason, ok := hookOutput["permissionDecisionReason"].(string)
	require.True(t, ok, "permissionDecisionReason should be a string")
	assert.Contains(t, reason, "guidance-advisory")

	assertSingleEventStatus(t, env, events.ResultSuccess)
}

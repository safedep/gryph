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

func claudePreToolUse(t *testing.T, tool string, input map[string]any) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"session_id":      "receipt-level-session",
		"cwd":             "/home/user/project",
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"tool_input":      input,
		"tool_use_id":     "tool-use-receipt",
	})
	require.NoError(t, err)
	return payload
}

func TestPolicy_ReceiptFollowsLoggingLevel(t *testing.T) {
	const tokenURL = "https://example.com/invite/k7Qz9xWm"
	cases := []struct {
		name     string
		level    string
		payload  []byte
		wantKeep map[string]any
		wantDrop []string
	}{
		{
			name:     "full level keeps the url",
			level:    "full",
			payload:  claudePreToolUse(t, "WebFetch", map[string]any{"url": tokenURL, "prompt": "read"}),
			wantKeep: map[string]any{"url": tokenURL},
		},
		{
			name:     "minimal level drops the url",
			level:    "minimal",
			payload:  claudePreToolUse(t, "WebFetch", map[string]any{"url": tokenURL, "prompt": "read"}),
			wantDrop: []string{"url"},
		},
		{
			name:     "sensitive url drops the url",
			level:    "full",
			payload:  claudePreToolUse(t, "WebFetch", map[string]any{"url": "https://example.com/app/.env", "prompt": "read"}),
			wantDrop: []string{"url"},
		},
		{
			name:     "sensitive write drops the line counts",
			level:    "full",
			payload:  claudePreToolUse(t, "Write", map[string]any{"file_path": "/home/user/project/.env", "content": "A=1\nB=2\n"}),
			wantKeep: map[string]any{"path": "/home/user/project/.env"},
			wantDrop: []string{"lines_added", "lines_removed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnvWithPolicy(t, blockCommandPolicy("block-npm-install", `\bnpm\s+install\b`, "blocked"))
			_, _, err := env.run("config", "set", "logging.level", tc.level)
			require.NoError(t, err)

			_, _, err = env.runHookCapturingStd("claude-code", "PreToolUse", tc.payload)
			require.NoError(t, err)

			receipt := loadLatestMatchingReceipt(t, env, "allow")
			require.NotNil(t, receipt)
			for key, want := range tc.wantKeep {
				assert.Equal(t, want, receipt.ActionPayload[key], key)
			}
			for _, key := range tc.wantDrop {
				assert.NotContains(t, receipt.ActionPayload, key)
			}
		})
	}
}

func TestPolicy_CustomClassifyLabel_Blocks(t *testing.T) {
	env := newTestEnvWithPolicy(t, `version: "1"
rules:
  - id: block-customer-data
    action: block
    severity: high
    match:
      action_types: [file_read]
    condition: "'customer_data' in action.data_classifications"
    message: "blocked-by-policy: customer data"
`)
	f, err := os.OpenFile(env.configPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("  classify:\n    extra_patterns:\n      customer_data: [\"**/customers/**\"]\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	payload := claudePreToolUse(t, "Read", map[string]any{"file_path": "/home/user/project/customers/list.csv"})
	stdout, stderr, runErr := env.runHookCapturingStd("claude-code", "PreToolUse", payload)
	assertHookBlocked(t, stdout, stderr, runErr, "blocked-by-policy")
	assertSingleEventStatus(t, env, events.ResultBlocked)
}

// TestPolicy_StoredMessageFollowsLoggingLevel checks that a rule message
// that names a stripped value reaches the agent, but not the stored event,
// the export line, or the receipt.
func TestPolicy_StoredMessageFollowsLoggingLevel(t *testing.T) {
	const (
		tokenURL = "https://example.com/invite/k7Qz9xWm"
		secret   = "zq7Rk2Wm9xTokenValue"
	)
	cases := []struct {
		name        string
		level       string
		failMode    string
		rule        string
		payload     []byte
		leak        string
		decision    string
		exportArgs  []string
		wantStatus  events.ResultStatus
		wantStored  string
		wantBlocked bool
	}{
		{
			name:  "minimal level drops the url from the block message",
			level: "minimal",
			rule: `    action: block
    match: { action_types: [tool_use] }
    message: "blocked fetch to {{.Action.Params.URL}}"`,
			payload:     claudePreToolUse(t, "WebFetch", map[string]any{"url": tokenURL, "prompt": "read"}),
			leak:        tokenURL,
			decision:    "block",
			exportArgs:  []string{"export"},
			wantStatus:  events.ResultBlocked,
			wantStored:  "blocked fetch to",
			wantBlocked: true,
		},
		{
			name:  "sensitive write drops the content from the block message",
			level: "full",
			rule: `    action: block
    match: { action_types: [file_write] }
    message: "refused {{.Action.Params.Content}}"`,
			payload:     claudePreToolUse(t, "Write", map[string]any{"file_path": "/home/user/project/.env", "content": secret}),
			leak:        secret,
			decision:    "block",
			exportArgs:  []string{"export", "--sensitive"},
			wantStatus:  events.ResultBlocked,
			wantStored:  "refused",
			wantBlocked: true,
		},
		{
			name:     "a stored render error keeps the block under fail_mode open",
			level:    "full",
			failMode: "open",
			rule: `    action: block
    match: { action_types: [file_write] }
    message: "refused write starting {{slice .Action.Params.Content 0 4}}"`,
			payload:     claudePreToolUse(t, "Write", map[string]any{"file_path": "/home/user/project/.env", "content": secret}),
			leak:        secret[:4],
			decision:    "block",
			exportArgs:  []string{"export", "--sensitive"},
			wantStatus:  events.ResultBlocked,
			wantStored:  "rule leak-rule",
			wantBlocked: true,
		},
		{
			name:  "sensitive write drops the content from the warn message",
			level: "full",
			rule: `    action: warn
    match: { action_types: [file_write] }
    message: "careful {{.Action.Params.Content}}"`,
			payload:    claudePreToolUse(t, "Write", map[string]any{"file_path": "/home/user/project/.env", "content": secret}),
			leak:       secret,
			decision:   "warn",
			exportArgs: []string{"export", "--sensitive"},
			wantStatus: events.ResultSuccess,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnvWithPolicy(t, "version: \"1\"\nrules:\n  - id: leak-rule\n"+tc.rule+"\n")
			_, _, err := env.run("config", "set", "logging.level", tc.level)
			require.NoError(t, err)
			if tc.failMode != "" {
				_, _, err = env.run("config", "set", "policy.fail_mode", tc.failMode)
				require.NoError(t, err)
			}

			stdout, stderr, runErr := env.runHookCapturingStd("claude-code", "PreToolUse", tc.payload)
			if tc.wantBlocked {
				assertHookBlocked(t, stdout, stderr, runErr, tc.leak)
			} else {
				assertHookGuidance(t, stdout, stderr, runErr, tc.leak)
			}

			store, cleanup := env.openStore()
			evts, err := store.QueryEvents(context.Background(), events.NewEventFilter())
			cleanup()
			require.NoError(t, err)
			require.Len(t, evts, 1)
			assert.Equal(t, tc.wantStatus, evts[0].ResultStatus)
			assert.Equal(t, tc.wantStored, evts[0].ErrorMessage)

			exported, _, err := env.run(tc.exportArgs...)
			require.NoError(t, err)
			assert.Contains(t, exported, evts[0].ID.String())
			assert.NotContains(t, exported, tc.leak)

			receipt := loadLatestMatchingReceipt(t, env, tc.decision)
			require.NotNil(t, receipt)
			assert.NotEmpty(t, receipt.Message)
			assert.NotContains(t, receipt.Message, tc.leak)
		})
	}
}

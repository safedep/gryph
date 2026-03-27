# Codex Agent Adapter Design

## Overview

Add a Gryph agent adapter for OpenAI Codex, enabling hook-based event capture for session lifecycle, tool usage (currently Bash only), user prompt submission, and session stop events.

Codex hooks are configured via `~/.codex/hooks.json` and require the `codex_hooks = true` feature flag in the user's Codex config.

## Hook Types

| Codex Hook | Gryph ActionType | Matcher | Notes |
|---|---|---|---|
| `SessionStart` | `session_start` | `""` (match all) | Source: "startup" or "resume" |
| `PreToolUse` | `command_exec` | `Bash` | Currently only Bash supported by Codex |
| `PostToolUse` | `command_exec` | `Bash` | Includes `tool_response` with output |
| `UserPromptSubmit` | `tool_use` | `""` (match all) | Captures user prompt text |
| `Stop` | `session_end` | `""` (match all) | `last_assistant_message` in payload |

## Stdin JSON Structures

### Common Fields (all hooks)

```json
{
  "session_id": "string",
  "transcript_path": "string|null",
  "cwd": "string",
  "hook_event_name": "string",
  "model": "string"
}
```

Turn-scoped hooks (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`) add `turn_id`.

### SessionStart

Additional field: `source` (`"startup"` | `"resume"`)

### PreToolUse

Additional fields: `tool_name` (string), `tool_use_id` (string), `tool_input` (object with `command` for Bash)

### PostToolUse

Additional fields: `tool_name`, `tool_use_id`, `tool_input`, `tool_response` (object)

### UserPromptSubmit

Additional field: `prompt` (string)

### Stop

Additional fields: `stop_hook_active` (bool), `last_assistant_message` (string|null)

## Hook Response Semantics

Codex uses a different response pattern from most other Gryph adapters:

| Context | Behavior |
|---|---|
| `PreToolUse` allow | Exit 0, JSON: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"approve"}}` |
| `PreToolUse` block | Exit 0, JSON: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"..."}}` |
| Other hooks allow | Exit 0, no output or `{}` |
| Error | Exit 2, reason on stderr |

Key difference: PreToolUse blocking uses exit 0 + JSON (not exit code 2). Exit 2 signals error.

## Detection

- Check for `~/.codex/` directory existence
- Run `codex --version` (with 5s timeout) for version detection
- Fallback: if directory exists but binary not found, still mark as installed
- Config path: `~/.codex/`
- Hooks path: `~/.codex/hooks.json`

## Hooks Config Format

Codex uses a matcher-based `hooks.json` structure. Gryph generates:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "gryph _hook codex SessionStart",
            "timeout": 30
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "gryph _hook codex PreToolUse",
            "timeout": 30
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "gryph _hook codex PostToolUse",
            "timeout": 30
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "gryph _hook codex UserPromptSubmit",
            "timeout": 30
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "gryph _hook codex Stop",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

Tool hooks use `"matcher": "Bash"` since that is currently the only tool Codex supports for hooks. Non-tool hooks use empty matcher.

## Tool Name Mapping

```go
var ToolNameMapping = map[string]events.ActionType{
    "Bash": events.ActionCommandExec,
}
```

Future Codex tool support (Edit, Write, etc.) will expand this map.

## File Changes

### New Files

| File | Purpose |
|---|---|
| `agent/codex/adapter.go` | Adapter struct, interface methods, Register function |
| `agent/codex/detect.go` | Detection logic (directory + binary check) |
| `agent/codex/hooks.go` | Hook install/uninstall/status using matcher-based hooks.json |
| `agent/codex/parser.go` | Event parsing, hook responses, payload building |
| `agent/codex/parser_test.go` | Table-driven tests with JSON fixtures |
| `agent/codex/testdata/*.json` | Test fixtures for each hook type |

### Modified Files

| File | Change |
|---|---|
| `agent/adapter.go` | Add `AgentCodex = "codex"`, `DisplayCodex = "Codex"`, case in `AgentDisplayName()` |
| `agent/registry.go` | Add `"codex"` to `SupportedAgents()` |
| `config/config.go` | Add `agentNameCodex`, `Codex AgentConfig` field, cases in `GetAgentLoggingLevel()` and `IsAgentEnabled()` |
| `config/defaults.go` | Add `v.SetDefault("agents.codex.enabled", true)` |
| `config/validate.go` | Add `cfg.Agents.Codex.LoggingLevel` validation |
| `cli/root.go` | Import `agent/codex`, add `codex.Register(...)` call |
| `cli/hook.go` | Import `agent/codex`, add cases in `sendHookResponse()` and `sendSecurityBlockedResponse()` |
| `tui/component/livelog/model.go` | Add `"codex"` to `agentCycle` |
| `tui/component/livelog/styles.go` | Add `colorYellow` and `"codex"` case in `agentBadge()` |

## Patterns to Follow

- Hooks.json management follows the Windsurf adapter pattern (standalone file, not nested in settings)
- Matcher-based hook config follows the Gemini pattern (matcher groups with hook command arrays)
- Parser follows the Gemini pattern (base input parsing, switch on hook type, typed input structs)
- Session ID derivation: `uuid.NewSHA1(uuid.NameSpaceOID, []byte(sessionID))`
- Privacy checking, content hashing, logging levels, string truncation: reuse existing patterns from Gemini parser
- Hook response: custom JSON format for PreToolUse (permissionDecision), exit-code-based for errors

## Testing

- Table-driven unit tests for parser covering all 5 hook types
- Deterministic session ID derivation test
- Invalid JSON error handling test
- Hook response JSON serialization tests
- E2E test in `test/cli/e2e_hook_test.go` following the existing `TestHook_Windsurf`/`TestHook_Gemini` pattern

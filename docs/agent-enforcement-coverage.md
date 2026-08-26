# Agent Enforcement Coverage

This table records, per supported agent, which hook events Gryph receives and
whether each fires **before** the operation executes (pre-execution, where a
`block` decision actually prevents the action) or **after** it has already run
(post-execution, where a `block` is detection only, not prevention).

The AARM mediation layer stamps each action with `action.phase` (`pre`, `post`,
or `unknown`) derived from the source hook. Policy authors can scope true
prevention rules with `condition: action.phase == 'pre'`. Post-phase blocks
still return a block-shaped response to the agent (several agents feed it back
to the model as a correction signal), and the receipt records `phase = post`,
so the audit trail distinguishes prevention from detection.

The mapping is implemented in `aarm/mediation/phase.go` (a prefix/suffix
heuristic plus explicit overrides). The hook lists below come from each
agent's `parseHookEvent` dispatch.

## Coverage by agent

| Agent | Pre-execution hooks (enforceable) | Post-execution hooks (detection) | Lifecycle / unclassified |
|---|---|---|---|
| Claude Code | `PreToolUse` | `PostToolUse`, `PostToolUseFailure` | `SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `SubagentStop` |
| Cursor | `preToolUse`, `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`, `beforeTabFileRead`, `beforeSubmitPrompt` | `postToolUse`, `postToolUseFailure`, `afterFileEdit`, `afterTabFileEdit`, `afterShellExecution`, `afterMCPExecution`, `afterAgentThought` | `sessionStart`, `sessionEnd`, `stop`, `subagentStart`, `subagentStop` |
| Gemini | `BeforeTool` | `AfterTool` | `SessionStart`, `SessionEnd`, `Notification` |
| OpenCode | `tool.execute.before` | `tool.execute.after` | `session.created`, `session.idle`, `session.error` |
| Windsurf | `pre_read_code`, `pre_write_code`, `pre_run_command`, `pre_mcp_tool_use`, `pre_user_prompt` | `post_read_code`, `post_write_code`, `post_run_command`, `post_mcp_tool_use`, `post_cascade_response`, `post_setup_worktree` | (none) |
| Pi Agent | `tool_call` | `tool_result` | `session_start`, `session_shutdown` |
| Codex | `PreToolUse` | `PostToolUse` | `SessionStart`, `UserPromptSubmit`, `Stop` |
| OpenClaw | `before_tool_call` | `after_tool_call` | `session_start`, `session_end` |

## What this means for enforcement

- Every supported agent exposes at least one pre-execution hook covering tool
  calls, so a `block` on the primary tool-use path prevents the operation.
- Agents that expose distinct pre-hooks for file reads, shell, and MCP (Cursor,
  Windsurf) allow finer-grained pre-execution policy than agents with a single
  pre-tool hook.
- Post-execution hooks (`afterFileEdit`, `PostToolUse`, `tool.execute.after`,
  etc.) are recorded and can drive guidance or alerting, but a block on them
  does not undo the action. Write prevention rules against pre-phase only.
- `unknown` phase (lifecycle events, or any future hook the heuristic does not
  recognize) is the safe default: rules scoped to `action.phase == 'pre'` will
  not fire on it, so an unrecognized hook is never mistaken for an enforceable
  pre-execution point.

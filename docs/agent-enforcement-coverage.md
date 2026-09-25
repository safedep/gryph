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

Each adapter declares its hooks in a `Hooks()` table (`agent/<name>/hooks.go`).
Each entry gives the hook's phase, whether a block from Gryph stops the
operation, and whether the hook carries a user prompt. The decision service
reads the phase from that table. A hook that the adapter does not declare gets
phase `unknown`.

## Coverage by agent

The table below is generated from the `Hooks()` tables. A test in `cli` fails
when the two differ. Run
`GRYPH_UPDATE_DOCS=1 go test ./cli -run TestEnforcementCoverageDoc` to update it.

<!-- BEGIN generated hook table: go test ./cli -run TestEnforcementCoverageDoc -->

| Agent | Blocking pre-execution hooks | Post-execution hooks (detection) | Other hooks |
|---|---|---|---|
| Claude Code | `PreToolUse` | `PostToolUse`, `PostToolUseFailure` | `SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `SubagentStop` |
| Codex | `PreToolUse` | `PostToolUse` | `SessionStart`, `UserPromptSubmit` (prompt), `Stop` |
| Command Code | `PreToolUse` | `PostToolUse` | `Stop`, `SessionStart` |
| Cursor | `preToolUse`, `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`, `beforeTabFileRead`, `beforeSubmitPrompt` (prompt) | `postToolUse`, `postToolUseFailure`, `afterFileEdit`, `afterTabFileEdit`, `afterShellExecution`, `afterMCPExecution`, `afterAgentResponse`, `afterAgentThought` | `sessionStart`, `sessionEnd`, `stop`, `subagentStart`, `subagentStop`, `preCompact` |
| Devin | `PreToolUse` | `PostToolUse` | `SessionStart`, `UserPromptSubmit` (prompt), `Stop`, `SessionEnd` |
| Gemini CLI | `BeforeTool` | `AfterTool` | `SessionStart`, `SessionEnd`, `Notification` |
| OpenCode | `tool.execute.before` | `tool.execute.after` | `session.created`, `session.idle`, `session.error` |
| Pi Agent | `tool_call` | `tool_result` | `session_start`, `session_shutdown` |
| Windsurf | `pre_read_code`, `pre_write_code`, `pre_run_command`, `pre_mcp_tool_use`, `pre_user_prompt` (prompt) | `post_read_code`, `post_write_code`, `post_run_command`, `post_mcp_tool_use`, `post_cascade_response`, `post_setup_worktree` | (none) |
| OpenClaw (inactive) | `before_tool_call` | `after_tool_call` | `session_start`, `session_end` |

<!-- END generated hook table -->

The OpenClaw adapter is not registered (`cli/root.go`), because it is
non-functional. Installation cannot select it, and `runHook` returns
`unknown agent` for it. Gryph enforces no OpenClaw hook until the adapter is
registered. The row above records the planned mapping only.

## What this means for enforcement

- Every supported agent exposes at least one pre-execution hook covering tool
  calls, so a `block` on the primary tool-use path prevents the operation.
- Agents that expose distinct pre-hooks for file reads, shell, and MCP (Cursor,
  Windsurf) allow finer-grained pre-execution policy than agents with a single
  pre-tool hook.
- Post-execution hooks (`afterFileEdit`, `PostToolUse`, `tool.execute.after`,
  etc.) are recorded and can drive guidance or alerting, but a block on them
  does not undo the action. Write prevention rules against pre-phase only.
- `unknown` phase (lifecycle events, or any hook the adapter does not
  declare) is the safe default: rules scoped to `action.phase == 'pre'` will
  not fire on it, so an unrecognized hook is never mistaken for an enforceable
  pre-execution point.

## Tool call links

A post event is an observation when Gryph links it to the pre event of the
same tool call. The adapter reads the agent's tool call ID into
`tool_call_id`. The decision service finds the pre event with that ID in the
same session and stores its ID in `linked_event_id`. A post event without a
link is an action, because Gryph did not see the call before it ran.

These agents send a tool call ID: Claude Code, Codex, Command Code, Cursor
(`preToolUse`, `postToolUse`, `postToolUseFailure`), Devin, OpenCode, and Pi
Agent.

Known gaps:

- Cursor sends no tool call ID on `afterShellExecution`, `afterFileEdit`, and
  `afterMCPExecution`. For one command, Cursor also fires `preToolUse` and
  `postToolUse`. The unlinked post hooks are stored as actions, so one command
  can give more than one action row.
- Gemini CLI, Windsurf, and OpenClaw send no tool call ID. Their post events
  are actions.
- Cursor `sessionStart` can stop a session with `continue: false`. It is not a
  tool call, so its phase is `unknown` and its `Blocking` flag is false.

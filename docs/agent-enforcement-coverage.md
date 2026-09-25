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
| Claude Code | `PreToolUse`, `UserPromptSubmit` (prompt) | `PostToolUse`, `PostToolUseFailure` | `SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `SubagentStop` |
| Codex | `PreToolUse`, `UserPromptSubmit` (prompt) | `PostToolUse` | `SessionStart`, `Stop` |
| Command Code | `PreToolUse` | `PostToolUse` | `Stop`, `SessionStart` |
| Cursor | `preToolUse`, `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`, `beforeTabFileRead`, `beforeSubmitPrompt` (prompt) | `postToolUse`, `postToolUseFailure`, `afterFileEdit`, `afterTabFileEdit`, `afterShellExecution`, `afterMCPExecution`, `afterAgentResponse`, `afterAgentThought` | `sessionStart`, `sessionEnd`, `stop`, `subagentStart`, `subagentStop`, `preCompact` |
| Devin | `PreToolUse`, `UserPromptSubmit` (prompt) | `PostToolUse` | `SessionStart`, `Stop`, `SessionEnd` |
| Gemini CLI | `BeforeAgent` (prompt), `BeforeTool` | `AfterTool` | `SessionStart`, `SessionEnd`, `Notification` |
| OpenCode | `chat.message` (prompt), `tool.execute.before` | `tool.execute.after` | `session.created`, `session.idle`, `session.error` |
| Pi Agent | `input` (prompt), `tool_call` | `tool_result` | `session_start`, `session_shutdown` |
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

## Prompt hooks

A hook marked `(prompt)` carries the user prompt. Gryph records it as a
`user_prompt` event. A prompt that a person typed is an intent entry. A block
on a blocking prompt hook stops the prompt before the model sees it. Command
Code has no prompt hook, so its sessions have no intent.

- Cursor blocks a prompt with `continue: false` at exit 0. The other agents
  block at exit 2.
- Gemini CLI turns hooks on by default from v0.26.0, and Pi fires `input` from
  v0.47.0. `gryph doctor` warns when the detected version is older. An older
  Gemini CLI fires the hooks only when the user turns hooks on.
- Gemini CLI appends the files that the user names with `@` to the prompt.
  Gryph records the text before the `--- Content from referenced files ---`
  line only.
- OpenCode has no documented block result for `chat.message`. Its
  `Plugin.trigger` runs each hook in `Effect.promise`, so the error that the
  Gryph plugin throws on a block fails the prompt before OpenCode saves the
  user message. The plugin sends only the text parts that the user typed. It
  skips the `synthetic` parts, such as file contents.
- The model writes the prompt of an OpenCode subagent session, which has a
  parent session. Pi marks input from extension code with `source: extension`.
  Gryph gives both prompts the origin `agent`, and records them as
  observations, not intents.
- Pi input with `source: rpc` comes from a program that drives Pi. Gryph gives
  it the origin `user`.
- An install without the prompt hook stays valid. `gryph doctor` warns about
  it. An OpenCode or Pi plugin from an older Gryph lacks the prompt hook, and
  `gryph install --force` replaces it.

Known gaps:

- Pi runs the `input` handlers before it expands `/skill:` commands and prompt
  templates. A content rule sees `/review`, not the expanded text. An
  extension command never reaches `input`.
- Gemini CLI ignores guidance on `BeforeAgent`, because Gryph writes it to
  stderr at exit 0.
- When a Gemini CLI `AfterAgent` hook asks for a retry, Gemini CLI fires
  `BeforeAgent` again with text that the hook wrote. Gryph records it with the
  origin `user`.

# Build Notes: feat/subagent-observation

## What changed

6 files, +122 lines. Adds subagent observation to gryph's Claude Code adapter.

### Core changes
- `core/events/types.go` — Added `ActionSubagentStart`, `ActionSubagentStop` action types + display names
- `core/events/event.go` — Added `SubagentID`, `SubagentType` fields to Event struct + SubagentStartPayload, SubagentStopPayload types
- `agent/claudecode/hooks.go` — Added `SubagentStart`, `SubagentStop` to HookTypes registration
- `agent/claudecode/parser.go` — Added SubagentStartInput, SubagentStopInput structs, parseSubagentStart/parseSubagentStop functions, AgentID/AgentType fields to PreToolUseInput/PostToolUseInput, agent_id propagation in parsePreToolUse/parsePostToolUse

### Storage changes
- `storage/ent/schema/auditevent.go` — Added `subagent_start`, `subagent_stop` to action_type enum; added `subagent_id`, `subagent_type` optional string fields
- `storage/sqlite.go` — Store and read SubagentID/SubagentType in create and entToEvent

## Before this compiles

1. **Regenerate ent code:** `go generate ./storage/ent` — the schema changes (new enum values, new fields) require ent codegen to produce SetSubagentID/SetSubagentType methods
2. ~~**Update tests:** `agent/claudecode/parser_test.go` needs test cases~~ DONE — 4 new tests added
3. **Update event_test.go:** Add `ActionSubagentStart`, `ActionSubagentStop` to the allActions slice in DisplayName coverage test

## Tests added

4 new test functions in `agent/claudecode/parser_test.go`:
- `TestParseHookEvent_SubagentStart` — verifies SubagentStart parsing + payload extraction
- `TestParseHookEvent_SubagentStop` — verifies SubagentStop parsing + transcript path + last message
- `TestParseHookEvent_PreToolUseBash_SubagentAttribution` — verifies inline agent_id in subagent tool calls
- `TestParseHookEvent_PreToolUseBash_MainAgentNoSubagentID` — verifies main agent has empty SubagentID

3 new fixture files in `agent/claudecode/testdata/`:
- `subagent_start.json` — based on real captured hook data
- `subagent_stop.json` — based on real captured hook data
- `pre_tool_use_bash_subagent.json` — based on real captured hook data

## Validated against real Claude Code hook data (2026-04-14)

SubagentStart JSON:
```json
{"session_id":"...","agent_id":"aeaa22c674a7c3c34","agent_type":"Explore","hook_event_name":"SubagentStart"}
```

SubagentStop JSON:
```json
{"session_id":"...","agent_id":"aeaa22c674a7c3c34","agent_type":"Explore","hook_event_name":"SubagentStop","agent_transcript_path":".../<session>/subagents/agent-<id>.jsonl","last_assistant_message":"Done."}
```

Subagent PreToolUse JSON (key finding: agent_id IS inline):
```json
{"session_id":"...","hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"toolu_...","tool_input":{"command":"..."},"agent_id":"ae2409b562560f364","agent_type":"Explore"}
```

Main-agent PreToolUse JSON (NO agent_id field):
```json
{"session_id":"...","hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"toolu_...","tool_input":{"command":"..."}}
```

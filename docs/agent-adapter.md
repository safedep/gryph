# Adding a New Agent Adapter

This guide walks through adding support for a new AI coding agent to Gryph. The adapter pattern is in `agent/` - see the existing `claudecode/`, `cursor/`, and `gemini/` packages for reference.

## Overview

Each adapter is a Go package under `agent/` that implements the `agent.Adapter` interface (`agent/adapter.go`):

```go
type Adapter interface {
    Name() string
    DisplayName() string
    Detect(ctx context.Context) (*DetectionResult, error)
    Install(ctx context.Context, opts InstallOptions) (*InstallResult, error)
    Uninstall(ctx context.Context, opts UninstallOptions) (*UninstallResult, error)
    Status(ctx context.Context) (*HookStatus, error)
    ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error)
}
```

## Step-by-step

### 1. Create the package

```
agent/youragent/
  adapter.go      # Adapter struct + interface methods
  detect.go       # Agent detection logic
  hooks.go        # Hook install/uninstall/status
  parser.go       # Event parsing + hook responses
  parser_test.go  # Tests
  testdata/        # JSON fixtures for tests
```

### 2. Implement the adapter (`adapter.go`)

The adapter struct holds a privacy checker, logging level, and content hash flag. Delegate each interface method to the corresponding helper function.

```go
var _ agent.Adapter = (*Adapter)(nil) // compile-time check

type Adapter struct {
    privacyChecker *events.PrivacyChecker
    loggingLevel   config.LoggingLevel
    contentHash    bool
}

func Register(registry *agent.Registry, pc *events.PrivacyChecker, level config.LoggingLevel, contentHash bool) {
    registry.Register(New(pc, level, contentHash))
}
```

See `agent/gemini/adapter.go` for the full pattern.

### 3. Implement detection (`detect.go`)

Check whether the agent is installed (config directory exists, binary in PATH) and return a `DetectionResult` with version, config path, and hooks path.

Key fields: `Installed`, `Version`, `ConfigPath`, `HooksPath`.

See `agent/gemini/detect.go` or `agent/claudecode/detect.go`.

### 4. Implement hook management (`hooks.go`)

Three operations:

- **Install** - Read the agent's config file, merge gryph hook entries, write back. Support `--force`, `--dry-run`, and `--backup` flags via `InstallOptions`.
- **Uninstall** - Filter out commands starting with `"gryph"` from the hook config.
- **Status** - Validate that expected hook entries exist.

Each agent has its own config format. Claude Code and Gemini use `settings.json` with matcher-based hooks. Cursor uses `hooks.json` with a simpler array format. Match whatever your target agent expects.

See `agent/gemini/hooks.go` for the settings.json pattern.

### 5. Implement event parsing (`parser.go`)

This is where hook stdin JSON gets converted to `events.Event` objects.

**Key responsibilities:**

1. Parse the base JSON to extract session ID and hook event name
2. Derive a deterministic UUID from the session ID using `uuid.NewSHA1(uuid.NameSpaceOID, []byte(rawSessionID))`
3. Switch on hook type to parse type-specific input structs
4. Map tool names to action types (`events.ActionFileRead`, `ActionFileWrite`, `ActionCommandExec`, `ActionToolUse`)
5. Build typed payloads (`FileReadPayload`, `FileWritePayload`, `CommandExecPayload`, etc.)
6. Mark sensitive paths via the privacy checker
7. Generate diffs at `LoggingFull` level using `utils.GenerateDiff()`
8. Hash content when `contentHash` is enabled using `utils.HashContent()`

**Hook response types** - Define allow/block/error responses with the exit code semantics your agent expects. Common pattern: exit 0 = allow, exit 2 = block.

See `agent/gemini/parser.go` for a complete example.

### 6. Register the adapter

Modify these files to wire everything up:

| File | Change |
|---|---|
| `agent/adapter.go` | Add `AgentYourAgent` and `DisplayYourAgent` constants, add case to `AgentDisplayName()` |
| `agent/registry.go` | Add agent name to `SupportedAgents()` slice |
| `config/config.go` | Add agent name constant, `AgentConfig` field in `AgentsConfig`, cases in `GetAgentLoggingLevel()` and `IsAgentEnabled()` |
| `config/defaults.go` | Add `v.SetDefault("agents.youragent.enabled", true)` |
| `config/validate.go` | Add logging level validation |
| `cli/root.go` | Import package and call `Register()` |
| `cli/hook.go` | Add cases in `sendHookResponse()` and `sendSecurityBlockedResponse()`, add `handleYourAgentResponse()` |
| `tui/component/livelog/model.go` | Add agent name to `agentCycle` slice |
| `tui/component/livelog/styles.go` | Add agent badge color case in `agentBadge()` |

### 7. Write tests

**Unit tests** (`agent/youragent/parser_test.go`): Use table-driven tests and JSON fixtures in `testdata/`. Test:

- Each hook type parses correctly (action type, tool name, payload fields)
- Session ID derivation is deterministic
- Invalid JSON returns an error
- Tool name -> action type mapping
- Hook response exit codes and JSON serialization
- Content hash and diff generation at different logging levels

See `agent/gemini/parser_test.go`.

**E2E tests** (`test/cli/e2e_hook_test.go`): Add a `TestHook_YourAgent` function that exercises the full hook pipeline (stdin → parse → store → query). Each test case sends a fixture through `env.runHook("youragent", hookType, payload)` and verifies the event was stored with the correct action type and payload. Also add a deterministic session ID test that sends two events with the same session identifier and asserts they share the same UUID. See `TestHook_Windsurf` or `TestHook_Gemini` for the pattern.

### 8. Verify

```bash
make test                                         # all tests pass
make gryph                                        # binary builds
./bin/gryph install --agent youragent --dry-run   # hook generation works
```

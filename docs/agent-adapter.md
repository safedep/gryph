# Adding a New Agent Adapter

This guide shows how to add support for a new AI coding agent to Gryph. The adapter pattern is in `agent/`. Use an existing adapter for reference. The current adapters are `claudecode/`, `codex/`, `cursor/`, `devin/`, `gemini/`, `openclaw/`, `opencode/`, `piagent/`, and `windsurf/`. The `gemini/` package is the recommended reference for the `settings.json` pattern.

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
    RenderResponse(hookType string, decision HookDecision, detail string) HookResponse
}
```

The registry (`agent/registry.go`) is the single source of truth for which
agents exist. Per-agent knowledge lives inside the adapter package. The CLI,
config, and tui layers do not enumerate agents.

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

The adapter struct holds a redactor, logging level, and content hash flag. Delegate each interface method to the corresponding helper function.

```go
var _ agent.Adapter = (*Adapter)(nil) // compile-time check

type Adapter struct {
    privacyChecker *privacy.Redactor
    loggingLevel   config.LoggingLevel
    contentHash    bool
}

func Register(registry *agent.Registry, pc *privacy.Redactor, level config.LoggingLevel, contentHash bool) {
    registry.Register(New(pc, level, contentHash))
}
```

See `agent/gemini/adapter.go` for the full pattern.

#### Declare the hooks

`Hooks()` returns a `[]events.HookSpec` table with one entry for every hook the
adapter installs and parses. Keep the table in `hooks.go`, and derive the
install list from it with `agent.HookTypeNames(Hooks)`:

```go
var Hooks = []events.HookSpec{
    {Type: "PreToolUse", Phase: events.PhasePre, Blocking: true},
    {Type: "PostToolUse", Phase: events.PhasePost},
    {Type: "SessionStart", Phase: events.PhaseUnknown},
}

var HookTypes = agent.HookTypeNames(Hooks)

func (a *Adapter) Hooks() []events.HookSpec { return Hooks }
```

- `Phase` is `pre` for a hook that fires before the operation runs, `post` for
  one that fires after it, and `unknown` for lifecycle hooks. The decision
  service reads the phase from this table. It does not guess from the name.
- `Blocking` is true when a block from Gryph stops the operation. Only a `pre`
  hook can be blocking.
- `Prompt` is true when the hook carries a user prompt. Parse it into an
  `ActionUserPrompt` event with `event.SetPrompt`. The install is valid without
  a prompt hook, and `gryph doctor` warns when it is missing.
- Set `event.HookType` in `ParseEvent` to the declared name. A hook that the
  table does not declare gets phase `unknown`.
- Set `event.ToolCallID` when the agent sends a tool call identifier. The
  decision service uses it to link a post event to its pre event.
- Regenerate the coverage table with
  `GRYPH_UPDATE_DOCS=1 go test ./cli -run TestEnforcementCoverageDoc`.

#### Declare the hook config paths

`HookConfigPaths()` returns doublestar globs for every file your install step
writes. Use `agent.HomeConfigGlob` to build them:

```go
func (a *Adapter) HookConfigPaths() []string {
    return []string{agent.HomeConfigGlob(".youragent", "settings.json")}
}
```

The built-in self-protection rule blocks agent writes, deletes, and shell
commands that change these paths. Without them, a governed agent can remove its
own hooks and turn off Gryph. When hooks live in a directory of plugins or
extensions, protect the whole directory (`agent.HomeConfigGlob(".youragent",
"plugins", "**")`). Any file there can hook the agent.

Do not add agent paths or command regexes to `aarm/loader`. The loader takes
the globs from the registry and derives the shell-command check from the same
paths. `TestAdapters_InstallWritesOnlyDeclaredHookConfig` in
`cli/hookconfig_test.go` installs every registered adapter into an empty home
directory. It fails when the install writes a file that no glob covers.

Self-protection is best effort. It parses shell commands for the paths they
change, so it cannot see a path built at run time, a script file, or an
interpreter. Kernel-based self-protection is on the roadmap. See
`docs/security-policy.md`.

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
6. Mark sensitive paths via the redactor
7. Put agent content in `privacy.Text` fields. Use `privacy.Preview(v, n)` for a truncated preview. It records the size and the digest of the whole value. Do not redact, label, or strip content. The decision service does it (see `docs/content-labels.md`)
8. Generate diffs at `LoggingFull` level using `utils.GenerateDiff()`
9. Hash content when `contentHash` is enabled using `utils.HashContent()`

**Hook response types** - The Security Policy Engine drives three response paths. Define a response type for each:

- **Allow** - `NewAllowResponse()`. The event passes.
- **Block** - `NewBlockResponse(reason)`. The engine blocks the action.
- **Guidance** - `NewGuidanceResponse(guidance)`. The engine sends an advisory but does not block.

See `agent/gemini/parser.go` for a complete example.

**RenderResponse** - Implement `RenderResponse` on the adapter. It maps a
`agent.HookDecision` (allow, block, guidance) and a hook type to an
`agent.HookResponse` value: the stdout bytes, the stderr text, and the exit
code. The adapter owns the per-hook-type wire knowledge. The CLI performs
the IO through one generic `sendResponse` routine in `cli/hook.go`. Return
an `agent.RenderedResponse` built from your response constructors. The
common pattern is exit 0 for allow and guidance, and exit 2 with the reason
on stderr for block. See `agent/gemini/adapter.go` for the JSON-channel
pattern and `agent/cursor/adapter.go` for a per-hook schema switch.

Add a `render_test.go` table test over the hook type and decision matrix.
Assert `Stdout()`, `Stderr()`, and `ExitCode()`.

### 6. Register the adapter

Required edits outside the adapter package:

| File | Change |
|---|---|
| `agent/adapter.go` | Add the `AgentYourAgent` name constant |
| `cli/root.go` | Import the package and call `Register()` in `registerAdapters` |
| `config/defaults.go` | Add `v.SetDefault("agents.youragent.enabled", true)` |

Optional:

| File | Change |
|---|---|
| `tui/component/livelog/styles.go` | Add an entry to `agentBadgeColors`. Without it, the agent badge renders in the default dim color |

Everything else derives from registration. The config map accepts any agent
key. The display name comes from the adapter through the registry. The
livelog filter cycle comes from `Registry.List()`. The self-protection globs
come from `Registry.HookConfigGlobs()`.

An adapter can exist in code but stay out of the registry. To deactivate an
adapter, comment out its `Register()` call in `registerAdapters` in
`cli/root.go`. See the
`openclaw` adapter for this pattern.

### 7. Write tests

**Unit tests** (`agent/youragent/parser_test.go`): Use table-driven tests and JSON fixtures in `testdata/`. Test:

- Each hook type parses correctly (action type, tool name, payload fields)
- Session ID derivation is deterministic
- Invalid JSON returns an error
- Tool name -> action type mapping
- Hook response exit codes and JSON serialization
- Content hash and diff generation at different logging levels

See `agent/gemini/parser_test.go`.

**E2E tests** (`test/cli/e2e_hook_test.go`): Add a `TestHook_YourAgent` function that exercises the full hook pipeline (stdin -> parse -> store -> query). Each test case sends a fixture through `env.runHook("youragent", hookType, payload)` and verifies the event was stored with the correct action type and payload. Also add a deterministic session ID test that sends two events with the same session identifier and asserts they share the same UUID. See `TestHook_Windsurf` or `TestHook_Gemini` for the pattern.

### 8. Verify

```bash
make test                                         # all tests pass
make gryph                                        # binary builds
./bin/gryph install --agent youragent --dry-run   # hook generation works
```

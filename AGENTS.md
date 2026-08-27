# Gryph

## Build

```bash
make deps      # Install dependencies
make generate  # Generate ent code
make gryph     # Build binary
make test      # Run tests
make lint      # Run linter (golangci-lint)
```

Always run `make lint` before submitting changes to catch issues early.

When modifying the Event model (`core/events/event.go`) or any payload types, run `make generate-schema`.

## Architecture

```
core/           Domain models (events, sessions, audit, security) - most stable
config/         Viper-based configuration
storage/        SQLite + ent ORM
agent/          Adapter pattern (claudecode/, cursor/ and more)
cli/            Cobra commands as an App pattern
tui/            Output formatters (table, json, csv)
```

## Key Entry Points

- `cmd/gryph/main.go` - Entry point
- `cli/root.go` - App struct, dependency injection
- `agent/adapter.go` - Agent adapter interface
- `storage/storage.go` - Store interface

## Notes

- SQLite driver: `modernc.org/sqlite` (pure Go, uses `sqlite` not `sqlite3`)
- Config paths: macOS `~/Library/Application Support/safedep/gryph/`, Linux `~/.config/safedep/gryph/`. Overrides: `GRYPH_CONFIG_DIR`, `GRYPH_DATA_DIR`, `GRYPH_CACHE_DIR`
- Storage layer fully implemented with ent ORM
- Self-audit logs tool actions (install, uninstall, config changes)

## Hook Implementation

### Claude Code

- Hooks configured in `~/.claude/settings.json` (per official docs)
- Hook types: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `SessionStart`, `SessionEnd`, `Notification`
- Uses matcher pattern `"*"` for tool hooks to capture all tools
- Version detection via `claude -v` command

### Cursor

- Hooks configured in `~/.cursor/hooks.json`
- Hook types: `beforeSubmitPrompt`, `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`, `afterFileEdit`, `stop`

## Session Tracking

- **SessionID**: Deterministic UUID derived from agent's session_id using `uuid.NewSHA1(uuid.NameSpaceOID, []byte(agentSessionID))`
- **AgentSessionID**: Original session ID string from the agent, stored for correlation with agent's own storage (e.g., Claude Code transcripts)
- Sessions are created on first event and updated on subsequent events
- Session end detected from `SessionEnd` (Claude Code) or `stop` (Cursor) hook types

## Acceptance Suite

- `test/acceptance/` runs the real `gryph` binary through `testscript` txtar scripts in a
  sandboxed HOME. It gates CI on every PR and push. It owns the real-binary contracts: exit
  codes, on-disk file shapes, stdout and stderr formats, hook protocol responses.
- A user-facing guarantee needs a `<category>/.../<name>.txtar` script AND a matching
  `catalog.yaml` row (with a `tier`, and optional `labels`). `TestCatalogIntegrity` (runs
  under `go test ./...`) fails when a script has no catalog row. Do not add scaffolding.
  See `test/acceptance/README.md`.
- `test/cli/` (in-process pipeline tests) and `test/conformance/aarm/` (AARM spec) stay
  separate and do not overlap with the acceptance suite.

## Dev Docs

- `docs/e2e.md` - Writing and running E2E tests (`test/cli/`)
- `docs/agent-adapter.md` - Adding a new agent adapter
- `docs/aarm-dev.md` - AARM / policy layer (`aarm/`, `cli/policy.go`)

## IMPORTANT

- NO EMOJI
- Write all docs, markdown, and code comments in ASD-STE100 (Simplified Technical English): short sentences, active voice, one instruction per sentence, approved simple words, present tense.
- Keep code comments minimal. Add a comment only when the code cannot reveal its own intent. Make the code intention revealing through clear names and structure instead of comments. No inline comments except for complex logic.
- Re-use existing code and patterns. Refactor to share code instead of adding duplicate code.
- Follow idiomatic Go code conventions.
- Use `testify/assert` and `testify/require` for tests. Never use `t.Fatal` or `t.Error` directly.
- `dry/log` for internal logging. `log.Warnf` for soft failures.
- DO NOT use `;` to join sentences. No em-dash, unnecessary compound words.
- DO NOT use non-ascii characters in code or markdown docs.

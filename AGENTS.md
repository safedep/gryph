# Gryph

## Build

```bash
make deps      # Install dependencies
make generate  # Generate ent code
make gryph     # Build binary
make test      # Run tests
```

## Architecture

```
core/           → Domain models (events, sessions, audit, security) - most stable
config/         → Viper-based configuration
storage/        → SQLite + ent ORM
agent/          → Adapter pattern (claudecode/, cursor/ and more)
cli/            → Cobra commands as an App pattern
tui/            → Output formatters (table, json, csv)
```

## Key Entry Points

- `cmd/gryph/main.go` - Entry point
- `cli/root.go` - App struct, dependency injection
- `agent/adapter.go` - Agent adapter interface
- `storage/storage.go` - Store interface

## Notes

- SQLite driver: `modernc.org/sqlite` (pure Go, uses `sqlite` not `sqlite3`)
- Config paths: macOS `~/Library/Application Support/gryph/`, Linux `~/.config/gryph/`
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

## Dev Docs

- `docs/e2e.md` - Writing and running E2E tests (`test/cli/`)
- `docs/agent-adapter.md` - Adding a new agent adapter

## IMPORTANT

- Do not use unnecessary code comments
- Re-use existing code and patterns whenever possible
- Keep things simple, minimal, extensible, maintainable
- Code should be easy to understand and maintain
- Follow idiomatic Go code conventions, especially for error handling, testing (table driven tests) and documentation

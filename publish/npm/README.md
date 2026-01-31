# Gryph

AI coding agents read files, write code, and execute commands on your behalf. But what exactly did they do?

**Gryph** is a local-first audit trail for AI coding agents. It hooks into your agents, logs every action to a local SQLite database, and gives you powerful querying capabilities to understand, review, and debug agent activity.

## Why Gryph?

- **Transparency** - See exactly what files were read, written, and what commands were run
- **Pre-commit review** - Verify agent changes before committing to git
- **Debugging** - Replay sessions to understand what went wrong
- **Privacy** - All data stays local. No cloud, no telemetry

## Installation

```bash
npm install -g @safedep/gryph
```

## Quick Start

```bash
# Install hooks for all detected agents
gryph install

# Verify installation
gryph status

# Start using your AI coding agent (Claude Code, Cursor, Gemini CLI, etc.)
# ...

# Review what happened
gryph logs
```


See more details at our [GitHub Project](https://github.com/safedep/gryph)

# Gryph
The AI Coding Agent Observability Tool.

## Installation

```bash
# From source
make build
```

## First run

```bash
gryph install
```

This initializes the database, writes the default configuration, and installs hooks for supported agents.

## Common commands

```bash
gryph status
gryph logs
gryph query --agent claude-code --since 24h
gryph session <id>
gryph export --format jsonl -o audit.jsonl
gryph config show
```

## Configuration

Configuration is stored at `~/.config/gryph/config.yaml` by default and includes logging, storage, privacy, and display settings.

## Development

```bash
make build
./bin/gryph status
```

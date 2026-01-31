# CLI Reference

Complete reference for all `gryph` commands, flags, and options.

## Global Flags

These flags are available on all commands:

| Flag         | Short | Description                   |
| ------------ | ----- | ----------------------------- |
| `--config`   | `-c`  | Path to config file           |
| `--verbose`  | `-v`  | Increase output verbosity     |
| `--quiet`    | `-q`  | Suppress non-essential output |
| `--no-color` |       | Disable colored output        |

Color output can also be disabled via the `NO_COLOR` or `GRYPH_NO_COLOR` environment variables.

## Commands

### install

Install hooks for AI coding agents. Discovers all supported agents on the system and installs hooks to enable audit logging.

```bash
gryph install
gryph install --agent claude-code
gryph install --dry-run
gryph install --force
```

| Flag          | Type                | Default | Description                                |
| ------------- | ------------------- | ------- | ------------------------------------------ |
| `--agent`     | string (repeatable) | all     | Install for specific agent only            |
| `--dry-run`   | bool                | false   | Show what would be installed               |
| `--force`     | bool                | false   | Overwrite existing hooks without prompting |
| `--no-backup` | bool                | false   | Skip backup of existing hooks              |

### uninstall

Remove hooks from AI coding agents.

```bash
gryph uninstall
gryph uninstall --agent claude-code
gryph uninstall --purge
gryph uninstall --restore-backup
```

| Flag               | Type                | Default | Description                            |
| ------------------ | ------------------- | ------- | -------------------------------------- |
| `--agent`          | string (repeatable) | all     | Uninstall from specific agent only     |
| `--purge`          | bool                | false   | Also remove database and configuration |
| `--dry-run`        | bool                | false   | Show what would be removed             |
| `--restore-backup` | bool                | false   | Restore backed-up hooks if available   |

### status

Show installation status and health. Displays tool version, installed agents, hook status, database info, and configuration.

```bash
gryph status
```

No additional flags.

### doctor

Diagnose issues with installation. Checks database health, config validity, hook installation, and schema version.

```bash
gryph doctor
```

No additional flags.

### logs

Display recent agent activity, grouped by session.

```bash
gryph logs
gryph logs --follow
gryph logs --since "1h"
gryph logs --today
gryph logs --agent claude-code
gryph logs --format json
```

| Flag         | Short | Type     | Default | Description                                        |
| ------------ | ----- | -------- | ------- | -------------------------------------------------- |
| `--follow`   | `-f`  | bool     | false   | Stream new events                                  |
| `--interval` |       | duration | 2s      | Poll interval for follow mode                      |
| `--since`    |       | string   |         | Show events since (e.g., `1h`, `2d`, `2025-01-15`) |
| `--until`    |       | string   |         | Show events until                                  |
| `--today`    |       | bool     | false   | Shorthand for since midnight                       |
| `--limit`    |       | int      | 50      | Maximum events                                     |
| `--session`  |       | string   |         | Filter by session ID                               |
| `--agent`    |       | string   |         | Filter by agent                                    |
| `--format`   |       | string   | table   | Output format: `table`, `json`, `jsonl`            |

### query

Query audit logs with filters.

```bash
gryph query --file "src/**/*.ts"
gryph query --since "1w" --agent claude-code
gryph query --action file_write --today
gryph query --command "npm *"
gryph query --action file_write --show-diff
gryph query --action file_write --today --count
```

| Flag          | Type                | Default | Description                                    |
| ------------- | ------------------- | ------- | ---------------------------------------------- |
| `--since`     | string              |         | Start time                                     |
| `--until`     | string              |         | End time                                       |
| `--today`     | bool                | false   | Filter to today                                |
| `--yesterday` | bool                | false   | Filter to yesterday                            |
| `--agent`     | string (repeatable) |         | Filter by agent                                |
| `--session`   | string              |         | Filter by session ID (prefix match)            |
| `--action`    | string (repeatable) |         | Filter by action type                          |
| `--file`      | string              |         | Filter by file path (glob)                     |
| `--command`   | string              |         | Filter by command (glob)                       |
| `--status`    | string              |         | Filter by result status                        |
| `--show-diff` | bool                | false   | Include diff content in output                 |
| `--format`    | string              | table   | Output format: `table`, `json`, `jsonl`, `csv` |
| `--limit`     | int                 | 100     | Maximum results                                |
| `--offset`    | int                 | 0       | Skip first n results                           |
| `--count`     | bool                | false   | Show count only                                |

### sessions

List recorded sessions with summary statistics.

```bash
gryph sessions
gryph sessions --agent claude-code
gryph sessions --since "1w"
```

| Flag       | Type   | Default | Description                    |
| ---------- | ------ | ------- | ------------------------------ |
| `--agent`  | string |         | Filter by agent                |
| `--since`  | string |         | Filter by start time           |
| `--limit`  | int    | 20      | Maximum sessions               |
| `--format` | string | table   | Output format: `table`, `json` |

### session

Show detailed view of a specific session. Displays all actions in chronological order.

```bash
gryph session <id>
gryph session abc123 --show-diff
```

The `<id>` argument supports full UUID or prefix match.

| Flag          | Type   | Default | Description                                |
| ------------- | ------ | ------- | ------------------------------------------ |
| `--format`    | string | table   | Output format: `table`, `json`             |
| `--show-diff` | bool   | false   | Include diff content for file_write events |

### diff

View unified diff for a specific file_write event.

```bash
gryph diff <event-id>
gryph diff a1b2c3d4 --format json
```

The `<event-id>` argument supports full UUID or prefix match.

| Flag       | Type   | Default | Description                      |
| ---------- | ------ | ------- | -------------------------------- |
| `--format` | string | unified | Output format: `unified`, `json` |

### export

Export audit data for external analysis.

```bash
gryph export --format json -o events.json
gryph export --since "1w" --format csv
gryph export --agent claude-code --format jsonl
```

| Flag       | Short | Type   | Default | Description                           |
| ---------- | ----- | ------ | ------- | ------------------------------------- |
| `--since`  |       | string |         | Export events since                   |
| `--until`  |       | string |         | Export events until                   |
| `--agent`  |       | string |         | Filter by agent                       |
| `--format` |       | string | jsonl   | Output format: `json`, `jsonl`, `csv` |
| `--output` | `-o`  | string | stdout  | Write to file                         |

### config

View or modify configuration. Changes are logged to the self-audit trail.

#### config show

Display current configuration.

```bash
gryph config show
gryph config show --format json
```

| Flag       | Type   | Default | Description                    |
| ---------- | ------ | ------- | ------------------------------ |
| `--format` | string | table   | Output format: `table`, `json` |

#### config get

Get a specific configuration value.

```bash
gryph config get logging.level
gryph config get retention_days
```

#### config set

Set a configuration value.

```bash
gryph config set logging.level full
gryph config set retention_days 90
```

#### config reset

Reset all configuration to defaults.

```bash
gryph config reset
```

### retention

Manage data retention policy.

#### retention status

Show retention policy and statistics about events that would be affected by cleanup.

```bash
gryph retention status
```

#### retention cleanup

Delete events older than the configured retention period. Self-audit entries are preserved.

```bash
gryph retention cleanup
gryph retention cleanup --dry-run
```

| Flag        | Type | Default | Description                                 |
| ----------- | ---- | ------- | ------------------------------------------- |
| `--dry-run` | bool | false   | Show what would be deleted without deleting |

### self-log

View gryph's own audit trail: installations, uninstallations, configuration changes, and retention cleanups.

```bash
gryph self-log
gryph self-log --limit 10
gryph self-log --since "1w"
```

| Flag       | Type   | Default | Description                    |
| ---------- | ------ | ------- | ------------------------------ |
| `--since`  | string |         | Filter by time                 |
| `--limit`  | int    | 50      | Maximum entries                |
| `--format` | string | table   | Output format: `table`, `json` |

## Time Filters

Commands accepting `--since` and `--until` flags support:

| Format       | Example                | Description       |
| ------------ | ---------------------- | ----------------- |
| Minutes      | `30m`                  | Last 30 minutes   |
| Hours        | `1h`                   | Last hour         |
| Days         | `2d`                   | Last 2 days       |
| Weeks        | `1w`                   | Last 7 days       |
| ISO date     | `2025-01-31`           | Specific date     |
| ISO datetime | `2025-01-31T15:04:05Z` | Specific datetime |

## Action Types

Values for the `--action` filter:

| Action            | Display Name  | Description       |
| ----------------- | ------------- | ----------------- |
| `file_read`       | read          | File read         |
| `file_write`      | write         | File write        |
| `file_delete`     | delete        | File deletion     |
| `command_exec`    | exec          | Command execution |
| `network_request` | http          | Network request   |
| `tool_use`        | tool          | Tool usage        |
| `session_start`   | session_start | Session started   |
| `session_end`     | session_end   | Session ended     |
| `notification`    | notification  | Notification      |

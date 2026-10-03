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

Each hook entry names the `gryph` binary by its absolute path, with symbolic
links resolved, so the hook does not depend on `PATH`. Entries from an
earlier install that name the bare program `gryph` stay valid. Run
`gryph install --force` to rewrite them with the path. A binary that runs
under another name keeps the bare program name.

| Flag          | Type                | Default | Description                                |
| ------------- | ------------------- | ------- | ------------------------------------------ |
| `--agent`     | string (repeatable) | all     | Install for specific agent only            |
| `--dry-run`   | bool                | false   | Show what would be installed               |
| `--force`     | bool                | false   | Overwrite existing hooks without prompting |
| `--no-backup` | bool                | false   | Skip backup of existing hooks              |
| `--repair-timer` | bool             | false   | Also install the timer that runs `gryph supervisor reconcile --once` every 15 minutes |
| `--managed`   | bool                | false   | As root: write the managed configuration and the managed hook entries for every user of the host |
| `--policy`    | string              |         | With `--managed`: the managed policy file to install |
| `--json`      | bool                | false   | With `--managed`: print the report as JSON |

`--repair-timer` writes a per-user job for the scheduler of the operating
system and asks the scheduler to run it: a systemd user timer on Linux
(`~/.config/systemd/user/gryph-reconcile.{service,timer}`), a launchd agent
on macOS (`~/Library/LaunchAgents/io.safedep.gryph-reconcile.plist`), or a
scheduled task on Windows (`SafeDep\gryph-reconcile`). When the scheduler is
out of reach, for example a systemd user instance that is not running, the
files stay in place and the output names the command to run by hand. The
job runs as the user. It repairs only when `policy.self_protection.repair`
is on. See [supervisor reconcile](#supervisor-reconcile).

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
| `--repair-timer`   | bool                | false   | Also remove the timer that runs `gryph supervisor reconcile` |
| `--managed`        | bool                | false   | As root: remove the managed hook entries, the Gryph entries in every home, and the managed policy and configuration |
| `--json`           | bool                | false   | With `--managed`: print the report as JSON |

#### Managed uninstall

`sudo gryph uninstall --managed [--purge] [--dry-run] [--json]` reverses
[Managed install](#managed-install). It removes the Gryph entries from the
managed hook file of every agent and keeps the other entries. It then removes
the Gryph entries from the agent hook files in every account's home, the
system-wide reconcile job, and last the managed policy and configuration. The per-user state (the database, the
keys, the receipts) stays unless `--purge`. A second run changes nothing.

The walk of the homes is the one place where a root process touches a user's
home, and it never does so as root. For each account with a home directory,
Gryph starts a helper that drops to the account's uid and gid before it opens
a file, with the account's home as `HOME` and a minimal environment. The
helper applies the repair rules: no link in the path below the home, a
temporary file and a rename, Gryph entries only. A link in a home reaches only
what the account can already write. On Windows, Gryph cannot drop to another
account, so the report says the homes were skipped.

Exit codes: 0 success, 1 failure, 10 partial, with the JSON naming the
degraded agent or account. The JSON lists `agents` (the managed files),
`users` (one row per account, with the hooks removed per agent or the error),
`removed` (the managed files) and `users_skipped`.

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
gryph doctor --format json
```

| Flag       | Short | Type   | Default | Description                              |
| ---------- | ----- | ------ | ------- | ---------------------------------------- |
| `--format` |       | string | table   | Output format: `table`, `json`, `jsonl`, `csv` |

After the checks, `doctor` prints the self-protection table. It has one row per
Gryph asset: the hook configuration of each agent on the host, the binary, the
policy, the config, the database and the keys. Each row shows the level at
which the asset resists a change, its path, and its drift when the asset does
not match a current install. The `Profile` line is the label that the levels
earn: `none`, `guard`, `locked` or `managed`. The
[threat model](./security-policy-threat-model.md#assets-levels-and-profiles)
defines the levels and the profiles.

```
Self-protection
  Profile: guard

  ASSET        AGENT         LEVEL                     DETAIL
  hook_config  claude-code   detect                    /home/me/.claude
  hook_traffic claude-code   detect                    1 process(es), pid 4242, last hook event 12s ago
  binary                     mediated                  /opt/safedep/gryph/bin/gryph
  policy                     mediated                  /home/me/.config/safedep/gryph/policy.yaml, ...
  config                     mediated                  /home/me/.config/safedep/gryph/config.yml
  store                      mediated                  /home/me/.local/share/safedep/gryph/audit.db
  key                        mediated                  /home/me/.config/safedep/gryph/keys/receipt.key
  key                        mediated                  /home/me/.local/share/safedep/gryph/export.key

Host posture
  [ok]  kernel.yama.ptrace_scope = 1
        only a parent can trace the hook
  [..]  claude-code on a hook error or timeout = lets the action through
        hook timeout 600 s
```

The [self-protection guide](./self-protection.md) explains the table.

With `policy.enabled: false`, the default, the built-in rules do not run. Every
asset that only they protect is at `none`, the row names the config key, and
the profile is `none`.

The table also has one `hook_traffic` row per agent with a live process. A
process of an agent that ran through the census window (10 minutes by
default, `policy.self_protection.census_window`) while the agent sent no
hook call is drift on that row: the hooks do not reach Gryph, because they
were removed, stopped, or never fired. Doctor records it as a `silent_agent`
tamper event. The census matches the processes of the account by program
name, so it does not see an agent that runs under another name. Set
`policy.self_protection.census: false` on a host where an agent runs for
another reason than the user's own work.

After the table, `doctor` prints the host posture: the kernel settings that
decide how far a same-user adversary gets, and what each present agent does
when a hook fails. On Linux it reads `kernel.apparmor_restrict_unprivileged_userns`
or `user.max_user_namespaces`, and `kernel.yama.ptrace_scope`, and warns
when a user can make a user namespace or trace the hook. For every present
agent it names the longest hook timeout, and for Cursor whether the Gryph
entries set `failClosed`. The [threat model](./security-policy-threat-model.md#gaps-and-hardened-deployment)
names the values to set on a fleet.

`doctor` runs one reconcile pass. It records a tamper event in the system
session of the account when a hook configuration drifts, when the drift
changes or clears, and when the level of an asset changes. The line `Tamper
events recorded: N` names the count. A repeated run with no change records
nothing. With `--repair`, the pass also repairs hook configurations. See
[supervisor reconcile](#supervisor-reconcile) and
[system session and tamper events](./security-policy.md#system-session-and-tamper-events).

| Flag       | Short | Type | Default | Description                                                           |
| ---------- | ----- | ---- | ------- | --------------------------------------------------------------------- |
| `--repair` |       | bool | false   | Rewrite a hook configuration that differs from a current install      |

### supervisor reconcile

Run one self-protection pass: assess every Gryph asset, record each change
since the last pass as a tamper event, print the self-protection table, and
repair hook configurations.

```bash
gryph supervisor reconcile --once
gryph supervisor reconcile --once --repair
gryph supervisor reconcile --once --format json
```

| Flag       | Short | Type   | Default | Description                                                                             |
| ---------- | ----- | ------ | ------- | --------------------------------------------------------------------------------------- |
| `--once`   |       | bool   | false   | Run one pass and exit. Required in this version.                                        |
| `--repair` |       | bool   | false   | Repair in this pass, also when `policy.self_protection.repair` is off                   |
| `--format` |       | string | table   | Output format: `table`, `json`, `jsonl`, `csv`                                          |

The pass repairs when `policy.self_protection.repair` is `true` or `--repair`
is set. The key is `false` by default in the user scope and `true` by default
under a system managed configuration. A repair follows these rules:

- It runs as the user who owns the home directory. Root never repairs a file
  in a user's home.
- It refuses a symbolic link in the path of the file below the home
  directory, and leaves the link and its target as they are.
- It writes a temporary file in the same directory and renames it into
  place, so a crash leaves the old file.
- It rewrites only the Gryph entries. The user's other settings stay.
- It leaves a file that does not parse. A human must look at it.
- After three repairs of one asset within an hour, failed or not, it stops
  repairing that asset and records a `rate_limited` tamper event. A pass
  that fights an agent over a file must not loop.

`gryph install --repair-timer` installs the timer that runs this command every
15 minutes. The command exits with code 1 when a repair failed or an asset
reached the rate limit, so a timer job shows up in its log. The outcome of each repair
is a tamper event: `repair`, `repair_failed` or `rate_limited`. See
[system session and tamper events](./security-policy.md#system-session-and-tamper-events).

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
| `--live`     |       | bool     | false   | Interactive full-screen TUI monitor                |
| `--interval` |       | duration | 2s      | Poll interval for follow mode                      |
| `--since`    |       | string   |         | Show events since (e.g., `1h`, `2d`, `2025-01-15`) |
| `--until`    |       | string   |         | Show events until                                  |
| `--today`    |       | bool     | false   | Shorthand for since midnight                       |
| `--limit`    |       | int      | 50      | Maximum events                                     |
| `--session`  |       | string   |         | Filter by session ID                               |
| `--agent`    |       | string   |         | Filter by agent                                    |
| `--format`   |       | string   | table   | Output format: `table`, `json`, `jsonl`            |
| `--sort`     |       | string   | desc    | Sort order: `asc`, `desc`                          |

### query

Query audit logs with filters.

```bash
gryph query --file "src/**/*.ts"
gryph query --since "1w" --agent claude-code
gryph query --action file_write --today
gryph query --command "npm *"
gryph query --action file_write --today --count
gryph query --interactive
```

| Flag            | Short | Type                | Default | Description                                    |
| --------------- | ----- | ------------------- | ------- | ---------------------------------------------- |
| `--since`       |       | string              |         | Start time                                     |
| `--until`       |       | string              |         | End time                                       |
| `--today`       |       | bool                | false   | Filter to today                                |
| `--yesterday`   |       | bool                | false   | Filter to yesterday                            |
| `--agent`       |       | string (repeatable) |         | Filter by agent                                |
| `--session`     |       | string              |         | Filter by session ID (prefix match)            |
| `--action`      |       | string (repeatable) |         | Filter by action type                          |
| `--file`        |       | string              |         | Filter by file path (glob)                     |
| `--command`     |       | string              |         | Filter by command (glob)                       |
| `--status`      |       | string              |         | Filter by result status                        |
| `--sensitive`   |       | bool                | false   | Filter to events with sensitive file access    |
| `--interactive` | `-i`  | bool                | false   | Launch interactive TUI browser                 |
| `--format`      |       | string              | table   | Output format: `table`, `json`, `jsonl`, `csv` |
| `--limit`       |       | int                 | 100     | Maximum results                                |
| `--offset`      |       | int                 | 0       | Skip first n results                           |
| `--count`       |       | bool                | false   | Show count only                                |
| `--sort`        |       | string              | asc     | Sort order: `asc`, `desc`                      |

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

### cat

Show the full detail of one or more events: payload, diff, raw event, and conversation context, subject to the configured logging level.

```bash
gryph cat <event-id>
gryph cat a1b2c3d4 e5f6a7b8 --format json
```

Each `<event-id>` argument supports full UUID or prefix match.

| Flag       | Type   | Default | Description                                    |
| ---------- | ------ | ------- | ---------------------------------------------- |
| `--format` | string | table   | Output format: `table`, `json`, `jsonl`, `csv` |

### export

Export raw events as JSON Lines for external analysis. Each line is one complete event object with a `$schema` field. The summary line goes to stderr, so stdout stays clean for pipes. An export profile decides what happens to each content value. See [content labels](content-labels.md#export).

```bash
gryph export
gryph export --since "1w" -o audit.jsonl
gryph export --agent claude-code --export-profile metadata
```

| Flag          | Short | Type   | Default | Description                         |
| ------------- | ----- | ------ | ------- | ----------------------------------- |
| `--since`     |       | string | 1h      | Export events since                 |
| `--until`     |       | string |         | Export events until                 |
| `--agent`     |       | string |         | Filter by agent                     |
| `--session`   |       | string |         | Filter by session ID (prefix match) |
| `--export-profile` | | string | default | Export profile: `default`, `metadata`, `full`, or a name under `export.profiles` |
| `--sensitive` |       | bool   | false   | Deprecated. Same as `--export-profile full` |
| `--output`    | `-o`  | string | stdout  | Write to file                       |

### cost

Show per-session token usage and estimated cost across models and agents. See [docs/cost.md](cost.md) for how cost data is collected.

```bash
gryph cost
gryph cost --since 7d --by model
gryph cost --agent claude-code --sync
```

| Flag          | Type   | Default | Description                                 |
| ------------- | ------ | ------- | ------------------------------------------- |
| `--since`     | string |         | Show costs since (e.g., `1h`, `2d`)         |
| `--until`     | string |         | Show costs until                            |
| `--today`     | bool   | false   | Shorthand for since midnight                |
| `--yesterday` | bool   | false   | Filter to yesterday                         |
| `--agent`     | string |         | Filter by agent                             |
| `--model`     | string |         | Filter by model name                        |
| `--session`   | string |         | Filter by session ID (prefix match)         |
| `--by`        | string | session | Group by: `session`, `model`, `agent`, `day` |
| `--sync`      | bool   | false   | Collect or refresh cost data before display |
| `--force`     | bool   | false   | With `--sync`: recompute even if computed   |
| `--limit`     | int    | 100     | Maximum sessions                            |
| `--format`    | string | table   | Output format: `table`, `json`              |

### stats

Open an interactive full-screen statistics dashboard.

```bash
gryph stats
gryph stats --since 7d
gryph stats --since 30d --agent claude-code
```

| Flag      | Type   | Default | Description                                            |
| --------- | ------ | ------- | ------------------------------------------------------ |
| `--since` | string | today   | Time range: `today`, `7d`, `30d`, `all`, or a duration |
| `--until` | string |         | End of time window (same syntax as `--since`)          |
| `--agent` | string |         | Filter by agent name                                   |

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

#### Managed install

An administrator, or an MDM script that runs as root, installs Gryph for every
user of a host with one command:

```bash
sudo gryph install --managed --config /path/to/managed.yml [--policy /path/to/policy.yaml] [--json]
```

The command validates the whole input first. An invalid input exits 3 and
changes nothing. It then writes the configuration to the
[system managed location](#system-managed-configuration), the policy to the
managed `policy.yaml` when `--policy` is given, and the managed hook entry of
every agent in `managed.agents`, also when the agent is not installed yet. An
agent that left the list loses its entry on the next run. A second run with
the same input changes nothing and exits 0.

```yaml
# managed.yml
policy:
  enabled: true
managed:
  agents: [claude-code, codex]   # the agents that get a managed hook entry
  lock_hooks: [claude-code]      # also turn on the agent's own lock: only managed hooks run
  binary: /usr/libexec/safedep/gryph/gryph   # optional, the default of the platform
```

The hook entries name the `gryph` binary by an absolute path. Root must own
the binary and every directory above it, and nothing in the chain may be
writable by group or other, or the command exits 3. The default is
`/usr/libexec/safedep/gryph/gryph` on Linux, `/Library/SafeDep/gryph/bin/gryph`
on macOS and `%ProgramFiles%\SafeDep\gryph\gryph.exe` on Windows. `--config`
and `--policy` must pass the same check, so root never acts on a file another
user wrote.

| Agent | Class | Managed file | Lock switch |
|---|---|---|---|
| Claude Code | `locked` | `managed-settings.d/50-gryph.json` next to `managed-settings.json`: `/etc/claude-code/` on Linux, `/Library/Application Support/ClaudeCode/` on macOS, `C:\Program Files\ClaudeCode\` on Windows. Gryph owns this one file and never edits `managed-settings.json`. | `allowManagedHooksOnly: true` in the drop-in |
| Codex | `locked` | `/etc/codex/requirements.toml` on Linux and macOS, `%ProgramData%\OpenAI\Codex\requirements.toml` on Windows. Gryph rewrites the file: it keeps every key, pins `[features] hooks = true`, and replaces its own `[hooks]` entries. Comments do not survive the rewrite. | `allow_managed_hooks_only = true` |
| Cursor | `system_path` | `/etc/cursor/hooks.json` on Linux, `/Library/Application Support/Cursor/hooks.json` on macOS, `%ProgramData%\Cursor\hooks.json` on Windows. Gryph keeps the other entries and sets `failClosed: true` on its own, so a hook crash or timeout blocks the action. | none |
| Gemini CLI | `system_path` | `/etc/gemini-cli/settings.json` on Linux, `/Library/Application Support/GeminiCli/settings.json` on macOS, `%ProgramData%\gemini-cli\settings.json` on Windows. Gryph keeps the other settings and pins `hooksConfig.enabled: true`. | none |
| Windsurf | `system_path` | `/etc/devin/hooks.json` on Linux, `/Library/Application Support/Devin/hooks.json` on macOS, `%ProgramData%\Devin\hooks.json` on Windows. Gryph keeps the other entries. | none |

The class says how far the managed file resists the user, from the vendor
documentation. `locked`: the vendor documents that a user cannot turn the
managed hooks off. `system_path`: the agent reads the system file first, but
the vendor does not document that the user cannot override or disable its
hooks, so the reconcile pass keeps checking the user scope. `gryph doctor`
shows the difference on the `hook_config` row: a `locked` entry that matches
the managed configuration puts the row at `prevent_same_user`, whatever the
user scope holds, and a `system_path` entry only adds a note while the row
stays at `detect`. Devin, OpenCode, Pi Agent and Command Code have no
documented managed location, so `managed.agents` cannot name them.

The lock stops the developer's own hooks too, so it is off by default, and
only a `locked` agent accepts it. The managed hooks hold without it: a user
`disableAllHooks` cannot turn off a managed Claude Code hook, and Codex marks
managed hooks as trusted.

The install also writes the system-wide reconcile job, so every account of
the host runs `gryph supervisor reconcile --once` as itself at login and
every 15 minutes, with the managed binary as the program: systemd user units
under `/etc/systemd/user/` enabled with `systemctl --global` on Linux, a
launchd agent at `/Library/LaunchAgents/io.safedep.gryph-reconcile.plist` on
macOS, and a scheduled task for the Users group on Windows. The job runs as
the account, never as root, so the install never touches a home. An account
that is logged in picks the job up at its next login, or on Linux after
`systemctl --user daemon-reload`. When the scheduler is out of reach the
files stay in place and the report names the command to finish by hand.
Under a managed configuration `policy.self_protection.repair` is on by
default, so the job repairs as well as detects.

Under `--managed`, Gryph reads no `HOME`, `XDG_*` or `GRYPH_*` variable and
runs no program. Every path comes from the input file and the platform.

Exit codes: 0 success, 1 failure, 3 invalid input with nothing changed, 10
partial success. With 10, the JSON report lists the degraded agents with
their error.

```json
{
  "status": "ok",
  "config": "/etc/safedep/gryph/config.yml",
  "binary": "/usr/libexec/safedep/gryph/gryph",
  "changed": true,
  "agents": [
    {"name": "claude-code", "path": "/etc/claude-code/managed-settings.d/50-gryph.json", "class": "locked", "action": "install", "changed": true, "locked": true},
    {"name": "codex", "path": "/etc/codex/requirements.toml", "class": "locked", "action": "install", "changed": true, "locked": false},
    {"name": "cursor", "path": "/etc/cursor/hooks.json", "class": "system_path", "action": "install", "changed": true, "locked": false}
  ]
}
```

#### Managed doctor

An MDM tool reads the state of a managed install with one command and
treats the JSON as a compliance attribute:

```bash
gryph doctor --managed --json
```

The command reads the managed configuration, the managed policy, the binary
that the hook entries name, and the managed hook entry of every agent in
`managed.agents`. It reads no per-user state, so it gives the same answer
for every user of the host. The exit code is 0 for the `locked` profile and
1 otherwise, so a script can gate on it.

The profile is `locked` when every part is in place: a trusted managed
configuration, a trusted managed policy, a binary that passes the chain
check, and a managed entry that matches the configuration for every agent in
the allowlist. Anything missing puts the profile at `none` and names the
reason in `issues`. A binary that root does not own, or that sits below a
directory another user can write, is refused.

```json
{
  "schema_version": 1,
  "profile": "locked",
  "summary": "Locked (hook entry and policy files, decision and audit trail not protected)",
  "issues": [],
  "config": {"path": "/etc/safedep/gryph/config.yml", "chain": "ok"},
  "policy": {"path": "/etc/safedep/gryph/policy.yaml", "chain": "ok", "version": "1", "sha256": "...", "allow_user_policy": false},
  "binary": {"path": "/usr/libexec/safedep/gryph/gryph", "chain": "ok"},
  "agents": [
    {"name": "claude-code", "class": "locked", "path": "/etc/claude-code/managed-settings.d/50-gryph.json", "level": "prevent_same_user", "match": true, "locked": true},
    {"name": "cursor", "class": "system_path", "path": "/etc/cursor/hooks.json", "level": "detect", "match": true, "locked": false}
  ],
  "key": {"scope": "user", "protected": false},
  "supervisor": {"state": "absent"},
  "collection": {"level": "none"}
}
```

| Field | Meaning |
|---|---|
| `schema_version` | The version of this contract. A field never changes meaning. A new field raises nothing. A removed or renamed field raises the version. |
| `profile` | `locked` or `none`. |
| `summary` | One line for a human. Until the supervisor ships it reads "Locked (hook entry and policy files, decision and audit trail not protected)". |
| `issues` | What keeps the profile from `locked`. Empty for `locked`. |
| `config`, `binary` | `path` and `chain`: `ok`, `missing`, `untrusted` (with `error`), or `absent` when the platform has no managed location. |
| `policy` | The same, plus `version` (the file's version field), `sha256` of the file, and `allow_user_policy` from the managed configuration. |
| `agents` | One row per agent in `managed.agents`: `class`, `path`, `level` (`prevent_same_user` for a `locked` entry that matches, `detect` for a `system_path` entry that matches, `none` otherwise), `match`, `locked` (the lock switch), `error`. |
| `key` | `scope` is `user`: the receipt signing key lives in each user's home, and `protected` is false. |
| `supervisor` | `state` is `absent`: no decision service runs outside the user yet. |
| `collection` | `level` is `none`: no evidence leaves the host. |

The text form prints the same facts, with "Key: user-owned (not protected)"
on its own line.

#### System managed configuration

An administrator can place a configuration file at a system location. When it
exists, it is the only configuration source: Gryph reads no per-user file, no
`--config` file and no `GRYPH_*` environment variable. `config set` and
`config reset` refuse to run.

Per-user state (the database, the keys, the cache) then lives under the home
directory that the system account database gives for the real user. `HOME`,
`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `GRYPH_CONFIG_DIR`,
`GRYPH_DATA_DIR` and `GRYPH_CACHE_DIR` have no effect. On Windows the
`AppData` folders come from the shell, not from the environment.

| Platform | Path                                                     |
| -------- | -------------------------------------------------------- |
| Linux    | `/etc/safedep/gryph/config.yml`                          |
| macOS    | `/Library/Application Support/safedep/gryph/config.yml`  |
| Windows  | `%PROGRAMDATA%\safedep\gryph\config.yml`                 |

Gryph trusts the file only when the whole path is protected:

- On Linux and macOS, root must own the file and every directory above it.
  No component may be writable by group or other, except a directory with the
  sticky bit set. A symbolic link in the path must be owned by root, and its
  target must pass the same check.
- On Windows, Gryph resolves `%PROGRAMDATA%` through the shell, not from the
  environment. `SYSTEM`, `Administrators` or `TrustedInstaller` must own every
  component. Below `%PROGRAMDATA%`, no access control entry may let another
  principal write, delete or take ownership. Create `safedep` and `gryph` with
  inheritance disabled.

A file that fails the check is ignored, and Gryph logs one warning. `gryph
doctor` reports the reason.

The same directory holds the managed policy: `policy.yaml` and `policies/`.
They load as policy sources on every host that has the directory, each file
behind the same check. A managed policy file that fails the check does not
load, and the policy load fails. With `allow_user_policy: false` in the
managed configuration file, the user's own policy sources do not load. While
the managed configuration is in force, `policy.self_protection.enabled` has
no effect: the built-in rules stay on. See
[where policy files live](./security-policy.md#where-policy-files-live).

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

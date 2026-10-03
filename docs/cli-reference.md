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
| `--trust-store` | string            |         | With `--managed`: the receipt trust store to install, whose public keys every user's verifier trusts |
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
[Managed install](#managed-install). With the decision service on, it
first writes the configuration with the switch off, so the hooks decide in
process again, and then stops and removes the units of the service (the
`service` section of the report). It removes the Gryph entries from the
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

### run

Start an agent under a Landlock ruleset that keeps the Gryph configuration
directory out of its write reach. Linux 5.13 or later, no root. See
[the Landlock launcher](./self-protection.md#the-landlock-launcher).

```bash
gryph run claude-code
gryph run claude-code --resume
gryph run codex
```

The first argument is an agent name or a program on `PATH`. The rest of
the arguments go to the program. The launcher replaces itself with the
program, so the exit code is the program's. A kernel without Landlock, or
a program that is not on `PATH`, is an error: the launcher never starts a
program without the ruleset.

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

#### supervisor run

Run the decision service for every account of the host. The service takes
its socket from the service manager with socket activation, else it opens
`--socket`. It runs as the service account, never as root.

```bash
gryph supervisor run
gryph supervisor run --socket /tmp/hook.sock --state-dir /tmp/gryph-state
```

| Flag           | Type    | Default              | Description |
| -------------- | ------- | -------------------- | ----------- |
| `--socket`     | string  | the configured path  | Listen at this socket path instead of the one the service manager passes |
| `--state-dir`  | string  | the configured path  | Hold the partitions here |
| `--allow-root` | bool    | false                | Allow a run as root, for a test |
| `--max-conns`  | int     | 16                   | Open connections per account |
| `--rate`       | float   | 20                   | Requests per second per account |
| `--spool-dir`  | string  | the configured path  | Read the spool here. The service creates the root when it is missing |
| `--ingest-interval` | duration | 1m             | Time between two passes over the spool |
| `--spool-max-files` | int | 256                 | Entries one pass takes from one account's spool |

With the service on, the read commands of every account (`logs`, `query`,
`sessions`, `session`, `cat`, `diff`, `stats`, `cost`, `policy receipts`,
`policy context`, `policy approve list`, `policy approve show`, `policy
approve history`, `policy deferrals list`) read through the service and
see the partition of their own account, nothing else. `policy context
--window` reads a local database and is not available through the
service. A developer keeps access to their own
log on a managed host. Without the service the commands fail with the
socket path in the error. See [reads](./supervisor-dev.md#reads) in the
developer guide.

The service answers every escalation of the policy. It keeps the requests
of each account, asks on the terminal of the hook when the rule accepts
it, and expires a request that nobody answers. `gryph policy approve
list` shows the open requests and `gryph policy approve show ID` one of
them. A member of `policy.approval.local_admin.group` sees the requests of
every account and answers with `gryph policy approve resolve --id ID
--decision allow|deny [--scope once|session|window] [--note TEXT]`, which
confirms on the terminal and refuses `--yes`. On a host with polkit the
service also asks for the password of the approver, through `pkttyagent`
on the terminal of the command. `gryph policy approve watch` prints each
new request. The keys under `policy.approval` set the
channels, the floor, the waits, the grant scope and the admin group. See
[the approval workflow](./security-policy.md#approval-workflow).

At start the service makes the machine keys below the state directory when
they are missing: `keys/receipt.key` (the receipt signing key, mode 0600),
`keys/receipt-pub.json` (the public halves of the keys the service has
had, the current one last, readable by every account) and
`keys/export.key`. Every receipt of the service carries the key scope
`supervisor`. The service writes its pid to `supervisor.pid` in the state
directory and reloads its partitions on `SIGHUP`, so a key rotation
reaches it without a restart.

#### supervisor fanotify

Run the kernel watcher that stops a write to the managed files and the
binary by a process of a non-privileged account. Linux only. The managed
install starts it in its own unit when `supervisor.fanotify.enabled` is
on. See [the kernel watcher](./supervisor.md#the-kernel-watcher).

```bash
gryph supervisor fanotify
gryph supervisor fanotify --state-file /tmp/fanotify.json --pid-file /tmp/fanotify.pid --allow-root
```

| Flag           | Type   | Default                           | Description |
| -------------- | ------ | --------------------------------- | ----------- |
| `--state-file` | string | `fanotify.json` next to the socket | Report the watched paths and the changes here |
| `--pid-file`   | string | `fanotify.pid` next to the state file | Write the pid here |
| `--allow-root` | bool   | false                             | Allow a run as root, for a test |

The watcher refuses to run without a managed configuration, and without
`CAP_SYS_ADMIN`. It removes its state file when it stops.

#### supervisor import

Carry this account's own database into the decision service. The command
runs as the account, on a managed host with the service on.

```bash
gryph supervisor import [--force] [--json]
```

It sends the sessions, the events and the receipts of the database in the
account's home to the service, which stores them in the partition of the
account marked imported. The receipts keep the account's own signatures.
A session the service already has is skipped, and a marker file
`import.done` in the data directory ends a later run before it starts;
`--force` runs it again. The reconcile job of the account runs the import
once on a managed host. The JSON report has `database`, `sessions`,
`events`, `receipts`, `skipped`, `done` and `note`. See
[import](./supervisor-dev.md#import) in the developer guide.

#### supervisor keys rotate

Replace the receipt signing key of the decision service. Root runs it.

```bash
sudo gryph supervisor keys rotate [--note "why"] [--no-reload] [--json]
```

The command writes a new private key under the state directory, hands it
to the owner of the state directory (the service account), puts the public
half in the managed trust store next to the keys already there, and sends
the reload signal to the pid in `supervisor.pid`. The old public key stays
in the trust store, so the receipts it signed still verify. `--no-reload`
leaves the running service on the old key until it restarts. `--state-dir`
names another state directory, for a test. The JSON report has `key_id`,
`receipt_key`, `trust_store`, `owner`, `reloaded`, `pid` and `note`.

The managed configuration sets the service: `supervisor.enabled` (the hook
becomes a client of the service), `supervisor.socket` (default
`/run/safedep/gryph/hook.sock` on Linux, `/var/run/safedep/gryph/hook.sock`
on macOS), `supervisor.profile` (`enforce` or `pilot`) with
`supervisor.pilot_until` (the date the pilot ends, `2006-01-02` or RFC 3339;
a pilot needs one, and after it the host runs `enforce`), `supervisor.state_dir`
(default `/var/lib/safedep/gryph` on Linux, the managed directory on macOS),
`supervisor.spool_dir` (default `/var/spool/safedep/gryph`) and
`supervisor.unavailable.{blocking,prompt,other}` (`block` or `allow`: what a
hook does when the service is out of reach, default `block` for a blocking
hook and `allow` for the rest), `supervisor.server_identity` (the account
behind the socket that the hook client accepts next to root, default
`_gryph`) and `supervisor.fanotify.enabled` (Linux: the kernel watcher,
off by default). Only the managed file sets them. The
[developer guide](./supervisor-dev.md) has the wire format, the limits and
the fail-mode table of the client.

#### Managed install

An administrator, or an MDM script that runs as root, installs Gryph for every
user of a host with one command. The [managed install guide](./mdm.md) has
the contract and the recipes for Jamf, Kandji and Intune.

```bash
sudo gryph install --managed --config /path/to/managed.yml [--policy /path/to/policy.yaml] [--trust-store /path/to/receipt-pub.json] [--json]
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
  binary: /opt/safedep/gryph/bin/gryph   # optional, the default of the platform
```

With `supervisor.enabled: true` the command installs the decision service
in an order that cannot block the agents of the host by mistake. It writes
the configuration with the switch off first, so the hooks keep deciding in
process. It makes the service account (`supervisor.server_identity`,
default `_gryph`, a system account with no login shell) when the host does
not have it, the state directory, the spool root and the machine keys,
hands them to the account, and writes the units of the service manager:
on Linux `/etc/systemd/system/gryph-supervisor.socket` (root-owned socket
at `supervisor.socket`, mode 0666, backlog 1024) and
`gryph-supervisor.service` (`User=_gryph`, `Restart=always`, the hardening
set: no new privileges, no capability, `ProtectSystem=strict`,
`ProtectHome=yes`, private tmp and devices, kernel and cgroup protection,
no namespaces, no SUID, locked personality, no writable and executable
memory, Unix sockets only, the `@system-service` call filter, umask 0077;
`ProtectProc` stays off for the peer check). It starts the socket, waits
up to 10 seconds for the service to answer over it with the identity check
of a hook, and only then writes the configuration as given, with the
switch on. A service that does not answer leaves the switch off, degrades
the run (exit 10) and names the next step. The JSON report carries this
under `service`: `account`, `units`, `changed`, `enabled`, `next`,
`running`, `switch`, `error`. With `supervisor.fanotify.enabled` the
command also writes `gryph-fanotify.service`, which runs the kernel
watcher as the service account with `CAP_SYS_ADMIN` and
`CAP_SYS_PTRACE`, waits for its state file, and reports it under
`fanotify` with the same fields. With socket activation the socket stays open
across a restart or an upgrade of the service: a hook that connects in
that window waits for the welcome.

The command also makes the machine keys of
the decision service when they are missing, under the state directory
(`supervisor.state_dir`, default `/var/lib/safedep/gryph`): the receipt
signing key `keys/receipt.key`, its published public half
`keys/receipt-pub.json` and the export key `keys/export.key`. A key that
exists stays, and root never reads it: the key id comes from the published
file. It hands the state directory and the
keys to the service account (`supervisor.server_identity`, default
`_gryph`) when the host has it, else they stay root's until a later run
finds the account. It puts the public half in the managed trust store
`keys/receipt-pub.json` of the managed directory, so every verifier on the
host trusts the key. The JSON report carries them under `keys`:
`receipt_key`, `key_id`, `export_key`, `trust_store`, `owner`, `changed`.
A failure degrades the install (exit 10). In managed mode
`policy.receipts.key_path` and `policy.receipts.trust_store` come from the
managed file alone. See [supervisor keys rotate](#supervisor-keys-rotate)
for a later rotation.

The hook entries name the `gryph` binary by an absolute path. Root must own
the binary and every directory above it, and nothing in the chain may be
writable by group or other, or the command exits 3. The default is
`/opt/safedep/gryph/bin/gryph` on Linux and macOS, and
`%ProgramFiles%\SafeDep\gryph\gryph.exe` on Windows. `--config`
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

`--trust-store` installs the receipt trust store of the fleet at
`keys/receipt-pub.json` in the managed directory, root-owned and readable by
every user. The verifier of every user trusts its public keys next to the
user's own store, and `gryph policy keys list` shows both. A key command of a
user (`generate`, `trust`, `revoke`) writes the user's own store, never the
managed one. An entry whose `key_id` does not match its `pub` exits 3.

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

On Windows the command runs from an elevated prompt. Gryph reads the managed
directory from the shell, so a `PROGRAMDATA` variable has no effect. The input
files and the binary must pass the Windows chain check of the
[system managed configuration](#system-managed-configuration): `SYSTEM`,
`Administrators` or `TrustedInstaller` own every component below
`%ProgramData%` or `%ProgramFiles%`, and no access control entry lets another
principal write. A file in a user's profile fails the check and the command
exits 3 with nothing changed.

Exit codes: 0 success, 1 failure, 3 invalid input with nothing changed, 10
partial success. With 10, the JSON report lists the degraded agents with
their error.

```json
{
  "status": "ok",
  "config": "/etc/safedep/gryph/config.yml",
  "binary": "/opt/safedep/gryph/bin/gryph",
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
  "binary": {"path": "/opt/safedep/gryph/bin/gryph", "chain": "ok"},
  "agents": [
    {"name": "claude-code", "class": "locked", "path": "/etc/claude-code/managed-settings.d/50-gryph.json", "level": "prevent_same_user", "match": true, "locked": true},
    {"name": "cursor", "class": "system_path", "path": "/etc/cursor/hooks.json", "level": "detect", "match": true, "locked": false}
  ],
  "key": {"scope": "supervisor", "protected": true, "path": "/var/lib/safedep/gryph/keys/receipt.key", "owner": "_gryph"},
  "supervisor": {"state": "absent", "profile": "pilot", "pilot_until": "2026-12-31", "pilot_remaining_seconds": 7603200},
  "collection": {"level": "policy", "profile": "policy", "target": "none"}
}
```

| Field | Meaning |
|---|---|
| `schema_version` | The version of this contract. A field never changes meaning. A new field raises nothing. A removed or renamed field raises the version. |
| `profile` | `locked` or `none`. |
| `summary` | One line for a human. "Locked (hook entry and policy files, decision and audit trail not protected)" while the hooks decide in process. "Locked (hook entry and policy files, decision and audit trail in the decision service)" with `supervisor.enabled` and a protected machine key. |
| `issues` | What keeps the profile from `locked`. Empty for `locked`. |
| `config`, `binary` | `path` and `chain`: `ok`, `missing`, `untrusted` (with `error`), or `absent` when the platform has no managed location. |
| `policy` | The same, plus `version` (the file's version field), `sha256` of the file, and `allow_user_policy` from the managed configuration. |
| `trust_store` | The managed receipt trust store: `path`, `chain`, `keys` (the count of public keys). A missing store is a fact, not an issue. |
| `agents` | One row per agent in `managed.agents`: `class`, `path`, `level` (`prevent_same_user` for a `locked` entry that matches, `detect` for a `system_path` entry that matches, `none` otherwise), `match`, `locked` (the lock switch), `error`. |
| `key` | `scope` is `user` while the receipt signing key lives in each user's home, with `protected` false. With `supervisor.enabled` and the machine key in place, `scope` is `supervisor`, `path` and `owner` name the key, and `protected` is true when a system account owns it and nobody else can read it. |
| `supervisor` | `state` is `absent`, because the command runs no program and asks no socket. With `supervisor.enabled`, `profile` is the profile in force, `pilot_until` the end of the pilot as the file sets it, `pilot_remaining_seconds` the time left while the pilot runs, and `approval_channels` and `approval_group` the approver channel. See [the decision service](./supervisor.md). |
| `collection` | `level` is the [collection level](./supervisor.md#the-collection-level) of the managed configuration (`evidence`, `policy` or `full`, `none` without a managed file), `profile` the export profile it names, and `target` the receiver. The target is `none` until a cloud target exists: nothing leaves the host. |

The text form prints the same facts, with "Key: user-owned (not protected)" or "Key: supervisor-owned (protected)"
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

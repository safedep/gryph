# Self-protection

Gryph protects agents by hooks, and the hooks live in files that the agent's
own user can change. Self-protection is how Gryph resists a change to its own
assets, how it notices one, and how it puts a hook back. This page is the
guide. The [CLI reference](./cli-reference.md) has every flag, and the
[threat model](./security-policy-threat-model.md) has the full tables of
levels, profiles and claims.

## What Gryph protects

Gryph names the parts of itself that an adversary can change as assets. For
each asset, a level says how far a change is stopped or noticed:

| Level | Meaning |
|---|---|
| `none` | Nothing stops or notices a change. |
| `mediated` | The built-in policy rules block a change, but only through a hooked tool call. |
| `detect` | Gryph notices a change and records it as a tamper event. |
| `repair` | Gryph restores the asset and records the repair. |
| `prevent_same_user` | The operating system stops a change by a non-admin process of the user. |
| `prevent_root_best_effort` | A kernel mechanism raises the cost of a change for root. It never stops root. |

The assets of a user install are the hook configuration of each agent on the
host, the hook traffic of each agent with a live process, the `gryph` binary,
the policy files, the configuration file, the audit database and the keys.

Run `gryph doctor` to see them:

```
Self-protection
  Profile: guard

  ASSET         AGENT         LEVEL      DETAIL
  hook_config   claude-code   detect     /home/me/.claude
  hook_traffic  claude-code   detect     1 process(es), pid 4242, last hook event 12s ago
  binary                      mediated   /opt/safedep/gryph/bin/gryph
  policy                      mediated   /home/me/.config/safedep/gryph/policy.yaml, ...
  config                      mediated   /home/me/.config/safedep/gryph/config.yml
  store                       mediated   /home/me/.local/share/safedep/gryph/audit.db
  key                         mediated   /home/me/.config/safedep/gryph/keys/receipt.key
  key                         mediated   /home/me/.local/share/safedep/gryph/export.key
```

A row shows `drift:` on a second line when the asset does not match a current
install, for example `drift: hooks not installed`.

## The profile

The profile is one word for the whole install, from the lowest level of any
row:

- `none`: at least one asset is at `none`. The default configuration has
  `policy.enabled: false`, so the built-in rules do not run and every asset
  they protect is at `none`. Set `policy.enabled: true` to leave this state.
- `guard`: every asset is at least `mediated`, and Gryph detects a change to
  every hook configuration. This is the profile of a user install with policy
  on. No root is needed.
- `locked` and `managed`: root installs with the agent's managed settings, and
  with evidence off the host. They are planned. `gryph doctor` reports them when
  their conditions hold.

### What you can say about `guard`

Gryph in the `guard` profile guards against agent mistakes and against simple
attempts to change Gryph. It detects a change to a hook configuration, and it
repairs the change when you turn repair on.

Do not say that Gryph in `guard` is secure, tamper-resistant, or cannot be
disabled. Gryph runs as the same operating-system user as the agent. A process
of that user, or a human at the keyboard, can edit the hook file, stop the hook
process, or replace the binary. Gryph then notices, and with repair on it puts
the hook back, but it does not prevent the change. A user with root or admin
rights can disable any local control. The
[threat model](./security-policy-threat-model.md#claims) has the claims for
every profile and the list of known bypasses.

## Tamper events

A change to an asset becomes a tamper event. Tamper events do not go into an
agent session. They go into the system session of the account: one session
per operating-system account, with the agent name `gryph`. Every tamper event
has a receipt in that session's chain, with the decision `tamper`, so the same
commands verify it:

```bash
gryph policy receipts --decision tamper
gryph policy receipts --verify --all-sessions
```

The receipt message names the asset, the agent, the level before and after,
and the drift, and the chain hashes it. The operations are `drift`, `level`,
`resolved`, `repair`, `repair_failed`, `rate_limited` and `silent_agent`. See
[system session and tamper events](./security-policy.md#system-session-and-tamper-events).

## The reconcile pass

One reconcile pass assesses every asset, records each change since the last
pass as a tamper event, and prints the table above. Three commands run it:

```bash
gryph doctor                              # with the health checks
gryph doctor --repair                     # and repair in this run
gryph supervisor reconcile --once         # the pass alone, for a timer
```

A pass with no change records nothing, so the system session stays small.

### Repair

With `policy.self_protection.repair: true`, or with `--repair`, the pass
rewrites a hook configuration that differs from a current install. The key is
`false` by default in the user scope, because a repair writes into the user's
files. It is `true` by default under a
[system managed configuration](./cli-reference.md#system-managed-configuration),
because an administrator who manages the host wants the hooks to stay.

```bash
gryph config set policy.self_protection.repair true
```

A repair follows these rules:

- It runs as the user who owns the home directory. Root never repairs a file in
  a user's home.
- It refuses a symbolic link in the path of the file below the home directory.
  The link and its target stay as they are. A link that an adversary planted
  cannot send the write elsewhere.
- It writes a temporary file in the same directory and renames it into place.
  A crash leaves the old file.
- It rewrites only the Gryph entries. The user's other settings stay.
- It leaves a file that does not parse. A human must look at it.
- After three repairs of one asset within an hour, failed or not, it stops
  repairing that asset and records a `rate_limited` tamper event. A pass that
  fights an agent over a file must not loop. Fix the cause, and the next hour
  the repairs resume.

Each repair is a tamper event: `repair`, `repair_failed` or `rate_limited`.

### The timer

A repair that nobody runs repairs nothing. Install the timer that runs the
pass every 15 minutes:

```bash
gryph install --repair-timer
gryph uninstall --repair-timer
```

The timer is a per-user job of the scheduler of the operating system: a
systemd user timer on Linux, a launchd agent on macOS, a scheduled task on
Windows. It runs as the user. When the scheduler is out of reach, for example
a systemd user instance that is not running, the files stay in place and the
output names the command to finish by hand. The
[install reference](./cli-reference.md#install) has the paths.

## The silent agent

A removed hook is one way to run an agent without Gryph. Another is to leave
the hook in place and stop the hook process until the agent times out and lets
the action through. Gryph then sees nothing. The census closes part of this
gap: it lists the processes of the account, matches them to the agents by
program name, and compares each live agent with its hook traffic. An agent
whose process ran through the census window (10 minutes by default) while the
agent sent no hook call is a silent agent. Doctor shows it as drift on the
`hook_traffic` row and records a `silent_agent` tamper event. A hook call
clears it.

An agent that idles at its prompt for ten minutes also sends no hook call, so
a silent agent is a signal to look at, not proof of an attack. Set
`policy.self_protection.census_window` to fit how you work, or
`policy.self_protection.census: false` on a host where an agent runs for
another reason than the user's own work.

## Host posture

Two kernel settings on Linux decide how far a same-user adversary gets, and
`gryph doctor` reports both under "Host posture":

- `kernel.apparmor_restrict_unprivileged_userns = 1`, or on a kernel without
  that setting `user.max_user_namespaces = 0`. Otherwise a user can make a
  user namespace and bind-mount a file of their own over a managed settings
  file or over the binary.
- `kernel.yama.ptrace_scope = 1` or more. With scope 0, a process of the user
  can attach to the hook process and change its answer.

For every present agent, doctor also names what the agent does when the hook
fails. Every supported agent lets the action through on a hook error or a
timeout, so a block exists only while Gryph answers in time. The
[enforcement coverage](./agent-enforcement-coverage.md) page has the timeout
of each agent and the date it was checked.

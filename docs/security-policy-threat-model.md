# Gryph Threat Model

Gryph applies a policy layer to AI coding agents. It mediates each agent action
through hooks and returns a decision: allow, warn, guidance, defer, escalate, or
block. It writes a signed receipt for each mediated action. This document states
what that layer defends and where its limits are. Read it with
[security-policy.md](./security-policy.md).

## Adversaries

Gryph names four tiers of adversary. Each row says what Gryph does against
that tier today and what it cannot do.

| Tier | Adversary | What Gryph does | What Gryph cannot do |
|---|---|---|---|
| A | An agent that acts only through its hooked tools, with a bad or injected instruction | Blocks the action on a pre-execution hook. The built-in rules keep the policy, the keys and the hook configuration out of the agent's write reach. | Stop an action on a hook that fires after the action, or on a tool that has no hook. |
| B | An agent or malware that runs any process as the same user: a shell, `python -c`, a script | Detects a removed or changed hook entry when `gryph doctor` runs. A root install with `gryph install --managed` writes the hook entry into the agent's managed settings and the policy into the system directory, out of the user's reach. With the [decision service](./supervisor.md), the decision, the audit trail, the signing key and the approval requests sit in a service outside the user, which knows every caller from the kernel. | Stop a change outside the hook path: a direct write, a network call, a second agent binary, or an agent with a private config directory. Stop the user from taking the service out of reach for their own account: a blocking hook then blocks. |
| C | A user with root or admin rights, or an agent that runs as root | Nothing on the host. Off-host evidence (planned) reports a stopped service, a changed asset, or a receipt chain that goes back. | Prevent anything. Root can stop the service, edit any file, and replace the binary. |
| D | A compromised kernel or firmware | Nothing on the host. | Give any local guarantee. |

In one sentence: Gryph prevents tier A, contains tier B on a managed host
with the decision service and part of it without, and detects tier C only
with evidence off the host.

Root never writes into a user's home. The reconcile pass and the repair run
as the user. The one exception is `gryph uninstall --managed`, which cleans
the agent hook files of every account through a helper that drops to that
account's uid and gid before it opens a file.

The largest tier B gap: an agent with a shell can work outside the hook path.
The pre-execution hook sees the command, and Gryph parses it for the paths it
changes, but a script file, an interpreter, or a path built at run time escapes
that analysis. Only a kernel mechanism can close this gap, and only in part.

## Trust boundary

Gryph is a control on the agent, not on the host. It does not defend
against tiers C and D, and it does not stop a tier B adversary that works
outside the hook path.

Where the trust line sits depends on the install:

- A user install trusts the agent's user. The hook decides in its own
  process, as that user, with the store and the keys in that user's home.
  A process of the user can read the key, edit the store, stop the hook or
  replace the binary.
- A managed host with the [decision service](./supervisor.md) trusts root
  and the kernel, and nothing below them. The hook runs as the user but
  decides nothing. The service runs as a system account, knows every
  caller by the peer credentials the kernel reports, keeps the store, the
  signing key and the approval requests of every account out of the
  user's reach, and signs with a machine key. The managed files, the
  binary and the socket path are root-owned. A user can take the service
  out of reach for their own account, and a blocking hook then blocks.

The built-in self-protection rules make tampering harder in both
installs, but they are a best-effort, same-user control. They match file
actions by path and parse shell commands for the paths they change. The
read rule blocks agent reads of the database and the receipt signing key
in the same way. It stops only the reads that the agent makes through a
mediated tool or a shell command that Gryph can parse. A human or an agent
can bypass the rules in many ways, for example with a path built at run
time, a script file, an interpreter, or a process outside the hook path.
On a user install a determined same-user adversary can defeat them. On a
managed host with the service they are one more layer in front of the
service. The [gaps](#gaps-and-hardened-deployment) section lists the
lower-level controls that close the rest.

## Assets, levels and profiles

Gryph describes its own protection per asset. `gryph doctor` prints one row per
asset with its level, and the profile that the levels earn. The
[self-protection guide](./self-protection.md) explains how to read the table
and how the reconcile pass and the repair work.

| Asset | What it is |
|---|---|
| `hook_config` | The hook configuration of one agent. One row per agent that is on the host. |
| `hook_traffic` | The flow of hook calls from a live agent process. One row per agent with a process. A process that runs through the census window while the agent sends no hook call is drift. |
| `binary` | The gryph binary that the hook entries run. |
| `policy` | The policy file and the policies directory. |
| `config` | The configuration file in force: the system managed file when it exists, else the user's file. |
| `store` | The audit database. |
| `key` | A secret key: the receipt signing key and the export key. On a managed host with the decision service, the machine key of the service. |
| `supervisor` | The decision service process. The service records its own findings under this asset: a hook that refused a socket that was not the system's, hooks that decided alone while the service ran, and spool files it refused. |

The level says how far a change to the asset is stopped or noticed. The levels
are ordered.

| Level | Meaning |
|---|---|
| `none` | Nothing stops or notices a change. |
| `mediated` | The built-in policy rules block a change, but only through a hooked tool call. |
| `detect` | Gryph notices a change and records it. |
| `repair` | Gryph restores the asset and records the repair. |
| `prevent_same_user` | The operating system stops a change by a non-admin process of the agent's user. |
| `prevent_root_best_effort` | A kernel mechanism raises the cost of a change for root. It never stops root. |

A row can also carry `attest`. It says that evidence of the asset's state exists
off the host.

A provider at `detect` records a change as a tamper event in the system session
of the account, with a receipt in that session's chain. See
[system session and tamper events](./security-policy.md#system-session-and-tamper-events).

### Profiles

The profile is the label that the minimum levels earn. One weak row lowers the
profile of the whole host.

| Profile | Minimum levels | What it is |
|---|---|---|
| `guard` | `mediated` on every asset, and `detect` on every hook configuration | The profile of a user install with policy on. No root. Repair of a hook configuration is a choice: `policy.self_protection.repair`. |
| `locked` | `guard`, plus `prevent_same_user` on `binary`, `policy` and `config`, and at least `repair` on every hook configuration | A root install that uses the agent's managed settings. `gryph doctor --managed` reports it. The hook entry of an agent with a `locked` managed class (Claude Code, Codex and Cursor) resists the user. An agent with a `system_path` class (Gemini, Windsurf) reads a root-owned file but has no lock of its own, so its row stays at `detect`. With the decision service, the decision and the audit trail are the service's too. |
| `managed` | `locked`, plus `attest` on every asset | Locked with off-host evidence. Planned. |

The default configuration has `policy.enabled: false`. Then the built-in rules
do not run, every asset that only they protect is at `none`, and the profile is
`none`. Each such row names the config key in its detail. Set
`policy.enabled: true` to reach `guard`.

### Claims

| Profile | You can say | You cannot say |
|---|---|---|
| `guard` | Gryph guards against agent mistakes and simple attempts to change Gryph. It detects a change to a hook configuration. | Secure. Tamper-resistant. Cannot be disabled. |
| `locked` | The hook entry and the policy files resist the non-admin user and their agents. With the decision service: the decision, the audit trail, the signing key and the approvals sit outside the user. | Tamper-proof. Stops root. Tamper-evident against root. |
| `managed` | A change to the hooks, the policy, or the receipts, or a stopped decision service, is detected off the host. | Tamper-proof. Prevents root. |

Every profile has the same limit: a user with root or admin rights can disable
any local control. Only the `managed` profile reports it, as a report that
stops.

## How policy resolves and combines

Gryph resolves policy from three sources: the global file
`${ConfigDir}/policy.yaml`, the files in `${ConfigDir}/policies/`, and the
built-in self-protection rules. Both user sources sit inside the config
directory. Gryph loads no file at any other path.

The engine keeps the decision with the highest precedence:
`block > escalate > defer > guidance > warn > allow`. Two properties follow:

- An allow can never override a block. Precedence decides, not file order.
- A new file can only add rules. It cannot lower a decision another rule makes,
  so it cannot weaken policy. The worst it can do is block too much, which is the
  safe direction.

The built-in rules load last and always apply. No policy file can remove them: a
user file may not use the reserved `gryph-builtin-` prefix, and `disabled:` acts
only on rules in the file that declares it. The operator turns the built-in rules
off only with `policy.self_protection.enabled` in the config file, an
all-or-nothing switch. The rules are part of the policy, so `policy.enabled:
false` turns them off too. Under a system managed configuration, the switch has
no effect and the rules stay on.

An administrator adds a fourth source: the managed policy in the system
directory, behind the same path chain check as the managed configuration. It
can drop the user's sources with `allow_user_policy: false`, so the user cannot
weaken it and cannot widen it either.

## Candidate review

An agent cannot write into the config directory. The built-in file rule blocks
its mediated `file_write` there. So an agent drafts a candidate at an unprotected
path, and a human installs it with `gryph policy install`. This is the review
gate.

The gate is hard against the agent's file tools, but soft against a shell: an
agent that runs commands as the same user can run `install` itself. This is the
same-user limit above. Every install writes a self-audit row, so it is visible.
Because a new file can only add rules, an installed file cannot remove the
built-in rules or the operator's rules. The remaining risk is over-blocking, the
safe direction.

## Bypass catalog

The table lists the known ways past Gryph, with the status in the `guard`
profile of a user install and in the `locked` profile of a managed host
with the decision service: P for prevented, D for detected, O for open.
The last column names the control that changes the status. A control
marked planned does not exist yet. The red-team suite (`make redteam`)
tries the bypasses of row 6 against the protected paths under each
provider and measures what each provider costs on the hook path. The
nightly kernel job runs it and keeps the report as an artifact.

| # | Tier | Bypass | In `guard` | In `locked` with the service | Control |
|---|---|---|---|---|---|
| 1 | B | Edit the agent's hook configuration and remove the Gryph entry | D, when a reconcile pass runs. Repaired when `policy.self_protection.repair` is on, up to three times an hour. | P for Claude Code, Codex and Cursor: the entry sits in the agent's managed settings, owned by root, and the agent runs it whatever the user scope holds. D for Gemini and Windsurf: the entry is a root-owned system file the agent reads, with no lock of its own. | `gryph install --managed`, with `managed.lock_hooks` for the agents that have a lock. |
| 2 | B | Stop or trace the hook process until the agent times out and lets the action through | D, when the agent process runs through the census window with no hook call | D, the same. A hook that cannot reach the service blocks. | The `hook_traffic` row and the `silent_agent` tamper event. Set `kernel.yama.ptrace_scope` to 1 or more. Set the agent to fail closed where the vendor offers it. See [agent-enforcement-coverage.md](./agent-enforcement-coverage.md). |
| 3 | B | Run a second agent binary, or an agent with a private config directory | D for a binary with a known program name, O otherwise | D, the same. A hook from outside the agent of its session is a low-trust connection, and its prompts and escalations count less. | The census matches the processes of the account by program name. An agent under another name stays out of scope. |
| 4 | B | Run `gryph _hook` from a shell and record a forged event or prompt | P through a mediated shell command, O otherwise | D: the service knows the caller from the kernel and records the trust of every connection. A forged prompt from a low-trust connection does not become the intent and does not reset the counters. | The built-in `gryph-builtin-hook-command` rule, and [the trust of a connection](./security-policy.md#the-trust-of-a-connection). |
| 5 | B | Patch the gryph binary, put another one earlier on `PATH`, or preload a library | O | P for the hook entry: it names an absolute path under a root-owned directory. O for a binary the user starts by hand. | `gryph install --managed`. Set `kernel.apparmor_restrict_unprivileged_userns` or `user.max_user_namespaces`, so a user namespace cannot mount over the path. |
| 6 | B | Change a protected file outside the shell analysis: a script file, an interpreter, a path built at run time | O | P for the policy, the configuration, the store and the keys: they are root's or the service account's. O for the user's own policy files when `allow_user_policy` is on. With the [kernel watcher](./supervisor.md#the-kernel-watcher), a write to a managed file or the binary is refused by the kernel whatever the mode of the file says. Under `gryph run`, a write to the user's configuration directory is refused by Landlock, whatever path it takes, from everything the agent starts. The red-team suite shows what each stops: the watcher does not stop a `truncate(2)` or a read through a user namespace mount, and the launcher does not reach a program started outside it. | `gryph install --managed` with the decision service, `allow_user_policy: false`, and `supervisor.fanotify.enabled` on Linux. |
| 7 | B | Run `gryph policy install` from an agent shell | O. The self-audit log shows it. A new file can only add rules. | O with `allow_user_policy: true`, P with `false`. | `gryph install --managed --policy`: a managed policy that the user cannot change, with `allow_user_policy: false` to drop the user's files. |
| 8 | B | Read or change the database or the keys from a shell | P through a mediated shell command, O otherwise | P: the service account owns the store and the keys, mode 0700 and 0600, and the service serves an account its own partition only. | The decision service. |
| 9 | B | Answer an approval request from an agent, with `gryph policy approve resolve` | O | P for the developer's own request: the account that asked cannot answer. P for an answer from under a known agent process. D for an agent that allocates a terminal under an admin account that runs no known agent. The `local-auth` channel asks for the password. | The admin group, the login identity check, polkit, and the built-in rule that blocks the command from a shell. |
| 10 | B | Take the service out of reach for the own account: a flood of connections, a stopped socket, a socket of the user's own | O | P: a blocking hook blocks, a socket that is not the system's is refused in every profile, and the spool records the `degraded` and `server_identity` tamper events. | The decision service and the fail modes of the hook. |
| 11 | C | Root stops the service, edits managed files, or replaces the binary | O | O | In `managed`: a report off the host that stops. |

## Gaps and hardened deployment

A user install cannot close the same-user gaps. A managed host with the
decision service closes most of them: the hook entry, the policy, the
configuration, the binary path, the store and the keys are out of the
user's reach. Two surfaces stay with the user:

- The hooks of an agent without a lock, which are the enforcement point.
  The entry is a root-owned system file, but the agent also reads its own
  user settings, so a same-user process can start the agent without the
  hook. Gryph detects the missing hook calls.
- The hook process itself. A same-user process can trace it or stop it
  until the agent times out. A blocking hook then blocks, so the agent
  stops instead of going through, but a prompt hook or a post-action hook
  allows by default.

A hardened deployment adds a lower-level control, for example an EDR agent
or a kernel mechanism, to protect both. On Linux, Gryph ships one for the
first surface: the [kernel watcher](./supervisor.md#the-kernel-watcher)
refuses a write to the managed files and the binary by a non-privileged
account, whatever the mode of the file says, and records a rename or an
unlink it cannot stop. On a user install, the
[Landlock launcher](./self-protection.md#the-landlock-launcher) keeps the
policy, the configuration and the keys out of the write reach of an agent
started through it, with no root. Until the second surface has a control,
Gryph on a managed host makes tampering by a same-user adversary a
visible event, and on a user install makes it harder, but does not stop a
determined one.

On a Linux fleet, set two kernel settings through the configuration management
of the fleet. `gryph doctor` reports both under "Host posture".

- `kernel.apparmor_restrict_unprivileged_userns = 1`, or on a kernel without
  that setting `user.max_user_namespaces = 0`. An unprivileged user namespace
  lets a user bind-mount a file of their own over a managed settings file or
  over the binary, and the agent then runs without Gryph.
- `kernel.yama.ptrace_scope = 1` or more. With scope 0, a process of the user
  can attach to the hook process and change its answer.

The receipt design lists further controls against audit-trail tampering. See the
receipt sections of [security-policy.md](./security-policy.md).

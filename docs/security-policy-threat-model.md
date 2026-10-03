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
| B | An agent or malware that runs any process as the same user: a shell, `python -c`, a script | Detects a removed or changed hook entry when `gryph doctor` runs. A root install with `gryph install --managed` writes the hook entry into the agent's managed settings and the policy into the system directory, out of the user's reach. | Stop a change outside the hook path: a direct write, a network call, a second agent binary, or an agent with a private config directory. |
| C | A user with root or admin rights, or an agent that runs as root | Nothing on the host. Off-host evidence (planned) reports a stopped service, a changed asset, or a receipt chain that goes back. | Prevent anything. Root can stop a service, edit any file, and replace the binary. |
| D | A compromised kernel or firmware | Nothing on the host. | Give any local guarantee. |

In one sentence: Gryph prevents tier A, contains part of tier B, and detects
tier C only with evidence off the host.

The largest tier B gap: an agent with a shell can work outside the hook path.
The pre-execution hook sees the command, and Gryph parses it for the paths it
changes, but a script file, an interpreter, or a path built at run time escapes
that analysis. Only a kernel mechanism can close this gap, and only in part.

## Trust boundary

Gryph is a control on the agent, not on the host. It runs as the same
operating-system user as the agent, and it trusts that user, the host, and the
runtime. It does not defend against tiers C and D, and it does not stop a tier
B adversary that works outside the hook path.

The built-in self-protection rules make tampering harder, but they are a
best-effort, same-user control. They match file actions by path and parse shell
commands for the paths they change. The read rule blocks agent reads of the
database and the receipt signing key in the same way. It stops only the reads
that the agent makes through a mediated tool or a shell command that Gryph can
parse. A human or an agent can bypass the rules in many ways, for example with a
path built at run time, a script file, an interpreter, or a process outside the
hook path. A determined same-user adversary can defeat them. A decision service
that runs outside the agent's user is planned. The
[gaps](#gaps-and-hardened-deployment) section lists the lower-level controls that
close this today.

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
| `key` | A secret key: the receipt signing key and the export key. |
| `supervisor` | The decision service process. No row until a system service exists. |

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
| `locked` | `guard`, plus `prevent_same_user` on `binary`, `policy` and `config`, and at least `repair` on every hook configuration | A root install that uses the agent's managed settings. Planned. |
| `managed` | `locked`, plus `attest` on every asset | Locked with off-host evidence. Planned. |

The default configuration has `policy.enabled: false`. Then the built-in rules
do not run, every asset that only they protect is at `none`, and the profile is
`none`. Each such row names the config key in its detail. Set
`policy.enabled: true` to reach `guard`.

### Claims

| Profile | You can say | You cannot say |
|---|---|---|
| `guard` | Gryph guards against agent mistakes and simple attempts to change Gryph. It detects a change to a hook configuration. | Secure. Tamper-resistant. Cannot be disabled. |
| `locked` | The hook entry and the policy files resist the non-admin user and their agents. | Tamper-proof. Stops root. Tamper-evident against root. |
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
profile: P for prevented, D for detected, O for open. The last column names the
control that changes the status. A control marked planned does not exist yet.

| # | Tier | Bypass | In `guard` | Control |
|---|---|---|---|---|
| 1 | B | Edit the agent's hook configuration and remove the Gryph entry | D, when a reconcile pass runs. Repaired when `policy.self_protection.repair` is on, up to three times an hour. | `gryph install --managed`: the hook entry sits in the agent's managed settings, owned by root. Claude Code and Codex today. |
| 2 | B | Stop or trace the hook process until the agent times out and lets the action through | D, when the agent process runs through the census window with no hook call | The `hook_traffic` row and the `silent_agent` tamper event. Set `kernel.yama.ptrace_scope` to 1 or more. Set the agent to fail closed where the vendor offers it. See [agent-enforcement-coverage.md](./agent-enforcement-coverage.md). |
| 3 | B | Run a second agent binary, or an agent with a private config directory | D for a binary with a known program name, O otherwise | The census matches the processes of the account by program name. An agent under another name stays out of scope. |
| 4 | B | Run `gryph _hook` from a shell and record a forged event or prompt | P through a mediated shell command, O otherwise | Planned: a decision service that identifies the caller with kernel peer credentials. |
| 5 | B | Patch the gryph binary, put another one earlier on `PATH`, or preload a library | O | `gryph install --managed`: the hook entry names an absolute path under a root-owned directory, and refuses any other. |
| 6 | B | Change a protected file outside the shell analysis: a script file, an interpreter, a path built at run time | O | A kernel mechanism, optional and Linux only. Planned. |
| 7 | B | Run `gryph policy install` from an agent shell | O. The self-audit log shows it. A new file can only add rules. | `gryph install --managed --policy`: a managed policy that the user cannot change, with `allow_user_policy: false` to drop the user's files. |
| 8 | B | Read or change the database or the keys from a shell | P through a mediated shell command, O otherwise | Planned: a decision service that owns the store and the keys. |
| 9 | C | Root stops a service, edits managed files, or replaces the binary | O | In `managed`: a report off the host that stops. |

## Gaps and hardened deployment

Gryph alone cannot close the same-user gaps. A hardened deployment adds a
lower-level control, for example an EDR agent or a kernel mechanism, to protect
two surfaces:

- The hooks, which are the enforcement point. The agent's hook configuration sits
  in a user-writable location. A same-user process can edit it to unhook Gryph,
  without touching policy.
- The binary, which is the decision point. The decision runs in the user-side
  `gryph` binary. A same-user adversary can patch it, put another binary earlier
  on `PATH`, or preload code. Install the binary under root ownership on a
  protected path.

The same lower-level control protects both surfaces, and the config-directory
policy files gain the same protection. Until then, Gryph makes tampering harder
for a same-user adversary, but does not stop a determined one.

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

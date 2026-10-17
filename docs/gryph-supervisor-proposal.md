# Gryph Supervisor Proposal

Status: Proposal. Not implemented. Owner decisions recorded on 2026-10-03.
Date: 2026-10-02.
Scope: privilege separation, self-protection, and a control plane for Gryph.

This proposal comes from a design debate between four roles: a security expert, a
software architect, a principal engineer, and a product manager. Each role wrote a
position paper, then a critique of the other three, then reviewed this document. The
[debate record](#appendix-a-debate-record) lists the main disagreements and how the
panel resolved them.

## 1. Summary

Gryph has two planes:

- The **data plane** is the hook system. Each agent hook runs `gryph _hook <agent>
  <type>`. Gryph parses the event, records it, evaluates policy, and returns a
  decision that the agent applies.
- The **control plane** owns configuration, policy, keys, hook installation, and in
  the future SafeDep Cloud: remote config and policy, and the sync of events and
  receipts.

Today both planes run as the agent's operating-system user, in a short-lived process
for each hook. The threat model in
[security-policy-threat-model.md](./security-policy-threat-model.md) says this
plainly: Gryph trusts the same user, and its self-protection is best effort.

This proposal adds a **supervisor**: a long-lived control-plane process. In system
mode it runs as root (later as root plus a dedicated service account) and owns the
decision service, the database, the receipt signing key, policy, and config. The hook
becomes a thin, unprivileged client that talks to the supervisor over a local socket.
Self-protection becomes a capability model with pluggable providers, so Gryph can
start with cheap providers and add kernel providers later without design change.

The panel agreed on five main decisions:

1. **Agent managed settings come first.** Some agents have their own root-owned,
   admin-enforced hook configuration (Claude Code `managed-settings.json`, Codex
   `requirements.toml`). This lever has the highest value for each week of work. It
   ships before the supervisor, and it needs no daemon.
2. **The supervisor adds to the code. It does not rewrite it.** It is a second
   implementation of the existing `decision.Service` boundary
   (`decision/service.go`), reached over IPC. The aarm Mediator, PDP, receipts,
   storage, and adapters keep their shape. Section 7.6 lists the small changes.
3. **One vocabulary for self-protection.** Each protected asset gets a capability
   level. Public profiles (Guard, Locked, Managed) are labels computed from these
   levels. A provider declares what it delivers, and `gryph doctor` reports the
   effective level for each asset and each agent.
4. **Honest claims.** Gryph prevents a misaligned agent that acts through its
   mediated tools. With a supervisor and managed settings, it resists the non-admin
   user and their agents. It only **detects** root, and only when off-host evidence
   exists. No profile claims "tamper-proof".
5. **Abstract on the second implementation.** New interfaces exist only where two
   implementations ship now. The other control-plane concerns use the seams that
   already exist (`loader.Source`, `stream.Target`, `receipt.Signer`,
   `approval.Service`). The cloud trust model is written down now, but it gets no code
   until the cloud API exists.

### 1.1 Owner decisions

The owner answered the open questions of the first draft on 2026-10-03:

| Question | Decision | Where |
|---|---|---|
| Is the supervisor open source? | Yes. It lives in this repository only. It can be a separate binary or a command of the `gryph` binary. Both are acceptable. | Section 7.7 |
| Default fail mode on a managed host when the supervisor is down | Block (fail closed) for pre-phase blocking hooks. | Section 8.1 |
| Apple Endpoint Security entitlement | SafeDep applies for it as a separate business task. It is not engineering work in this plan. | Section 16 |
| Who approves an escalation in managed mode | Both: the SafeDep Cloud queue and a local admin. | Section 9 |
| MDM deployment | Yes. The MDM scripts are out of scope. The design assumes that MDM scripts run as root and deploy Gryph, the supervisor, and the hooks. | Section 11.1 |
| What the cloud collects | The cloud becomes the policy control plane. It collects events, receipts, and the data that it needs to create, test, and evaluate policies. It does not collect all session data by default. A team can turn on full collection at its own risk. | Section 10.4 |

## 2. Current state

The facts below come from the code at commit `708b335`.

| Area | Today | Files |
|---|---|---|
| Hook entry | `gryph _hook <agent> <type>` runs as the agent user. It opens SQLite, loads config and policy, evaluates, and renders the response. | `cli/hook.go` |
| Decision boundary | `decision.Service.Handle(HookRequest) HookResponse`. Data-only. Fields are untrusted claims. An empty or unknown verdict blocks. Known gap: the hook side still opens the store for the hook-error self-audit row. | `decision/service.go`, `decision/local.go` |
| Policy engine | AARM Mediator: normalize, identity, context snapshot, PDP (YAML and CEL), escalate, defer, PEP, hash-chained receipt with optional Ed25519 signature. | `aarm/` |
| Self-protection | Built-in policy rules: `gryph-builtin-protected-files`, `gryph-builtin-protected-reads`, `gryph-builtin-hook-command`. Same user, best effort. | `aarm/loader/builtin.go` |
| Hook install | Each adapter implements `Detect`, `Install`, `Uninstall`, `Status`, `HookConfigPaths`, `Hooks`. The hook command is a bare `gryph`, which the shell finds through `PATH`. | `agent/adapter.go`, `agent/utils/command.go` |
| Managed config | `/etc/safedep/gryph/config.yml` (and the macOS and Windows paths). It applies only when root owns it and group and other cannot write it. When it exists, it is authoritative. Slots for `policy.yaml`, `policies/`, and `keys/` are reserved. | `config/system.go` |
| State | SQLite DB, signing key, and export key under the user's data and config directories. | `config/paths.go`, `internal/securefile` |
| Approvals | `escalate` opens `/dev/tty` in the hook process. `defer` uses a queue that `gryph policy approve` resolves. | `aarm/approval/cliprompt.go` |
| Sync | `gryph stream sync` pushes events and audits to targets, with export profiles. There is no daemon. | `stream/`, `cli/stream.go` |
| Enforcement | Only pre-phase blocking hooks prevent an action. Post hooks only detect. | `docs/agent-enforcement-coverage.md` |

Three facts shape the whole design:

- **Most agents fail open on hook failure.** The OpenCode plugin allows on any exit
  other than 2 and has a 5 s timeout. The Pi plugin allows on a spawn error or a
  timeout. Claude Code treats a non-zero exit other than 2 (for example 127, binary
  missing) as a non-blocking error. Cursor fails open unless `failClosed: true` is
  set. Codex does not block on a `PreToolUse` error or timeout. So "fail closed" exists
  only while a live client returns an explicit deny before the agent's timeout.
- **The hook command uses `PATH`.** A same-user process can shadow `gryph`.
- **The decision boundary already exists.** `decision/service.go` was written so that
  "a later privileged supervisor can serve the same Service over IPC".

## 3. Personas and use cases

| Persona | Need | Trust model | Profile |
|---|---|---|---|
| P1 Individual developer | Visibility, guardrails against agent mistakes, no friction | The developer owns the machine. The agent is the risk. | Guard |
| P2 Security-conscious developer | P1, plus signed receipts, strict policy, hook drift repair | Same as P1. Expects resistance to an injected agent that tries to unhook Gryph. | Guard, with repair on |
| P3 Team lead | Shared policy baseline, team view of cost and risk | Developers cooperate. Drift is an accident. | Guard or Locked |
| P4 Enterprise security or IT admin | MDM rollout (Jamf, Intune, Kandji), central policy, compliance evidence, no bypass by developers or agents | The developer and the agent are outside the trust boundary. | Locked |
| P5 SafeDep Cloud customer | P3 or P4, plus a hosted control plane: policy rollout, sync, fleet health, alerts | As P4, plus trust in SafeDep as the remote. | Managed |

Current use cases, by plane:

| Use case | Commands | Plane | Change in this proposal |
|---|---|---|---|
| Install and remove hooks | `install`, `uninstall`, `status`, `doctor` | Control | Add managed scope, absolute command, reconcile |
| Audit agent actions | `_hook`, `logs`, `query`, `sessions`, `diff`, `cat`, `stats`, `cost` | Data, then read | Hook becomes a client. Reads go through the supervisor in system mode. |
| Policy authoring and install | `policy init/validate/test/install`, authoring skill | Control | Managed policy in the system directory |
| Mediation | `_hook` with policy enabled | Data | Decision runs in the supervisor |
| Approvals and deferrals | escalate on `/dev/tty`, `policy approve` | Data and control | Assurance level on every approval |
| Evidence | receipts, signing, `verify`, `verify-log` | Control | Key moves to the supervisor |
| Export and sync | `export`, `stream sync` | Control | Same code, later run by the supervisor |
| Self-audit | install, uninstall, config changes, hook errors | Control | Tamper events join the receipt chain |

Two gaps matter for P4 today. Approvals assume that the developer is the approver.
`doctor` is a local, pull-based check, but a fleet needs pushed health.

## 4. Threat model and claims

### 4.1 Adversary tiers

| Tier | Adversary | What Gryph can achieve | What Gryph cannot achieve |
|---|---|---|---|
| A | A misaligned or injected agent that acts only through its mediated tools | Full mediation on pre-phase blocking hooks. Policy, keys, and hook config out of the agent's write reach. | Actions on hooks that are post-only or missing. |
| B | An agent or malware that runs any process as the same user (shell, `python -c`, a script) | With the supervisor: DB, key, policy, config, and binary out of the user's write reach. With managed settings: hook config out of reach too. Kernel peer credentials identify the hook caller. Removal of user-scope hooks is detected and repaired. | Actions outside the hook path (direct writes, network). A second, unhooked agent binary or an agent with a private config directory. Forged hook requests from the same user (section 7.3). |
| C | A local user with sudo, or an agent that runs as root | Tampering is loud when off-host evidence exists: a heartbeat gap, a changed asset digest, a receipt chain head that goes back. Kernel providers and immutable flags raise the cost. | Prevention. Root can stop services, unload BPF programs, clear immutable flags, edit managed settings, and replace the binary. |
| D | A kernel or firmware compromise | Only off-host signals: a heartbeat that stops, or a failed attestation. | All local guarantees. |

The honest statement: **Gryph prevents tier A, contains most of tier B, and detects
tier C only with off-host evidence.** The supervisor moves the trust line from "trusts
the same user" to "trusts root and the kernel".

The most important tier B gap that privilege separation does **not** close: an agent
with a shell can do work outside the hook path. The pre-hook sees the command, and
`aarm/shellcmd` analyzes it, but a script file or a built-up path escapes that
analysis. Only kernel-level providers (section 5.4) address this, and only in part.

### 4.2 Public profiles and claims

Customers see three profiles. Each profile is a label that `gryph doctor` computes
from the capability levels in section 5.

| Profile | What it is | Allowed claim | Forbidden claim |
|---|---|---|---|
| **Guard** | OSS default. No root. Built-in policy rules, drift detection, opt-in repair. | "Guards against agent mistakes and simple attempts to change Gryph. Detects and repairs hook drift." | "Secure", "tamper-resistant", "cannot be disabled" |
| **Locked** | Root install. Agent managed settings, root-owned binary and policy. Later the system supervisor. | Before the supervisor: "The hook entry and the policy files resist the non-admin user and their agents, for class A agents." With the supervisor: "The decision and the audit trail also resist the non-admin user". | "Tamper-proof", "stops root". "Tamper-evident against root" without an off-host sink. |
| **Managed** | Locked, plus SafeDep Cloud (or the admin's own sink) for off-host evidence. | "Tamper-evident: a change to hooks, policy, or receipts, or a stopped supervisor, is detected off the host." | "Tamper-proof", "prevents root" |

Every profile page carries one sentence on the root limit: "A user with root or admin
rights can disable any local control. Gryph reports a missing heartbeat when this
occurs." The last part applies only to Managed.

The Locked claim holds **per agent**, not per host. A host can be Locked for Claude
Code and only Guard-grade for OpenCode. Section 6 gives the coverage classes.

An optional, Linux-only "kernel-assisted" flag (section 5.4) adds prevention outside
the hook path for non-root processes. It is a flag on a profile, not a fourth profile.

## 5. Self-protection abstraction

### 5.1 Vocabulary

Self-protection is described per **asset** with an ordered **level**:

- Assets: `HookConfig(agent)`, `Binary`, `Policy`, `Config`, `Store`, `Key`,
  `Supervisor`.
- Levels: `None < Mediated < Detect < Repair < PreventSameUser <
  PreventRootBestEffort`.
  - `Mediated`: today's built-in rules. They block a change only through a hooked
    tool call.
  - `Detect`: Gryph notices a change and records a tamper event.
  - `Repair`: Gryph restores the asset and records the repair.
  - `PreventSameUser`: the OS stops a non-admin process of the agent's user.
  - `PreventRootBestEffort`: a kernel mechanism raises the cost for root. It never
    stops root.
- `Attest` is a separate flag. It says that off-host evidence for this asset exists.

Profile labels:

| Profile | Minimum levels |
|---|---|
| Guard | `Mediated` on all assets. `Detect` on `HookConfig`. `Repair` when the user turns it on. |
| Locked | `PreventSameUser` on `Binary`, `Policy`, `Config`. `PreventSameUser` on `HookConfig` for class A agents (section 6). With the supervisor, also on `Store`, `Key`, `Supervisor`. Other agents stay at `Repair`, and `doctor` says so per agent. |
| Managed | Locked, plus `Attest` on all assets. |

### 5.2 Interface

The provider interface is small. It has two implementations at the start: a user-scope
provider and a managed-scope provider. It has no `Watch` and no capability probe until
a kernel provider exists. A watch is a loop around `Assess`.

```go
package selfprotect

type Asset string

type Level int

const (
	LevelNone Level = iota
	LevelMediated
	LevelDetect
	LevelRepair
	LevelPreventSameUser
	LevelPreventRootBestEffort
)

type AssetStatus struct {
	Asset    Asset
	Agent    string // set for HookConfig
	Level    Level
	Attest   bool
	Provider string
	Drift    string // empty when the asset matches the desired state
	Detail   string
}

type Provider interface {
	Name() string
	// Assess is read only and cheap. doctor, reconcile, and receipts use it.
	Assess(ctx context.Context) []AssetStatus
	// Repair restores drift that this provider owns.
	Repair(ctx context.Context, opts RepairOptions) ([]AssetStatus, error)
}
```

The same `AssetStatus` struct feeds `gryph doctor`, the tamper events in the receipt
chain, and the future heartbeat.

Receipt chains are per session (`aarm/receipt`). Reconcile and server identity checks
run outside an agent session, so tamper events go to a **system session**: one per
user, with the deterministic ID `uuid.NewSHA1(uuid.NameSpaceOID, "gryph-system:<uid>")`,
the same scheme as agent sessions. The existing receipt generator and verifier then
cover tamper events with no new chain code. Machine-level events of the supervisor
use the system session of uid 0. A tamper event that the client finds (for example a
server identity mismatch) goes to the spool, and the supervisor appends it to the
system session of the file owner when it ingests the spool. A `Composite` that picks the strongest provider for
each asset comes only when a third provider exists.

### 5.3 Hook reconcile

Reconcile reuses the adapter contract. It needs no new adapter interface, only new
data on the install options:

```go
type InstallScope string

const (
	ScopeUser    InstallScope = "user"
	ScopeManaged InstallScope = "managed"
)

type InstallOptions struct {
	// existing fields ...
	Scope   InstallScope
	Command string // absolute path to the gryph binary
	Home    string // target home for a repair on behalf of a user
}

// ScopedAdapter is optional. An adapter without it supports the user scope only.
type ScopedAdapter interface {
	Scopes() []InstallScope
	ManagedConfigPaths() []string
}
```

`gryph supervisor reconcile --once` runs one pass. It is the same function as
`gryph doctor --repair`. An optional OS timer runs it: a systemd user timer, a launchd
agent with `StartInterval`, or a Windows scheduled task. There is **no user-mode
daemon** as a phase. A user daemon runs as the same user, so it adds no trust. It
becomes a near-free by-product of the IPC path, and Gryph turns it on only if the
latency baseline (section 13, phase 1a) shows a need for a warm process.

Rules for a repair of a file in a user's home. All four roles agreed on these rules.

1. A repair always runs as the user who owns the home. In phase 1a the user runs it.
   From phase 1b a system-wide user unit runs it as each user (section 11.1). Root
   never repairs a file in a user's home. The only root process that touches homes is
   the uninstall cleanup (section 11), through a helper that drops to the target uid
   and gid before it opens a file. A symlink then reaches only files that the user
   can already write.
2. Open with no-follow semantics: `openat2` with `RESOLVE_NO_SYMLINKS` on Linux,
   `O_NOFOLLOW` on each component elsewhere.
3. Write a temp file in the same directory, then rename. Keep unknown keys, as the
   adapters do today.
4. Touch only Gryph entries. Compare a hash of the Gryph entries, not of the file.
5. Detect is on by default. Repair is a config flag.
6. Rate limit. After N repairs in a window, stop, record a tamper event, and alert. A
   reconcile fight with the agent must not loop. Before a sink exists, the alert is
   the `doctor` output and a tamper receipt.
7. Report an unreachable home (NFS with `root_squash`, an encrypted home) as a state,
   not as an error loop.
8. For class A agents in managed mode, use managed settings. Do not write into user
   homes.

Repair in the hook call itself is out of scope. It adds latency, races the agent's own
settings writes, and cannot run when the removed hook is the one that would call it.
A `SessionStart` hook can run a cheap `Assess` for its own adapter and print one
notice.

### 5.4 Providers by platform

| Provider | Level | Needs | Bypass | Plan |
|---|---|---|---|---|
| Built-in policy rules | `Mediated` | Nothing | Any path outside a mediated tool | Exists |
| Reconcile pass on a timer | `Detect`, `Repair` for user-scope hook config | Nothing. It runs as each user (section 11.1). | Race window: unhook, start the agent, wait for repair. | Phase 1a |
| Agent managed settings plus root-owned binary and policy | `PreventSameUser` for `HookConfig`, `Binary`, `Policy`, `Config` | Root install. Agent support (section 6). | Root. Agents without a managed tier. A private agent config directory. | Phase 1b |
| Root-owned state under the supervisor | `PreventSameUser` for `Store`, `Key`, `Supervisor` | System service | Root | Phase 2 |
| systemd `Restart=always` and hardening, launchd `KeepAlive`, Windows service recovery | `Repair` for `Supervisor` | Unit files | Root disables the unit | Phase 2 |
| Linux fanotify permission events | `PreventSameUser` on open and write of protected files, outside hooks | `CAP_SYS_ADMIN` | Root kills the daemon. Some rename and unlink paths on older kernels. | Phase 4, optional |
| Linux Landlock launcher for agents | `PreventSameUser` for protected paths, outside hooks, no root | Kernel 5.13 or later, a wrapper that starts the agent | An agent started without the wrapper | Phase 4, optional |
| Linux BPF LSM (`file_open`, `inode_unlink`, `task_kill`, `bpf`) | `PreventSameUser`, `PreventRootBestEffort` | `CONFIG_BPF_LSM`, `bpf` in the active LSM list (often not the default), BTF | Root boots without `bpf` in `lsm=` or loads a module | Backlog, probe per distro |
| macOS Endpoint Security system extension | `PreventSameUser`, `PreventRootBestEffort` | Apple entitlement, notarized system extension, user or MDM approval, Full Disk Access | Root with SIP off, MDM removal | Only with a paying fleet customer and a dedicated owner |
| `chattr +i`, `uchg` | Slows a same-user change | Root | Root clears the flag. Breaks upgrades. | Not planned. Root ownership gives the same result. |
| Windows minifilter and PPL | `PreventSameUser`, `PreventRootBestEffort` | EV certificate, Microsoft attestation signing, ELAM for PPL | Admin with a vulnerable signed driver | Not planned. Kernel bugs crash customer machines. PPL does not stop an admin. |

**Root is handled by evidence, not by prevention.** When an off-host sink exists, the
supervisor sends a signed status report with: the `AssetStatus` list, a digest of each
managed asset, hook status for each agent, the receipt chain head for each active
session, and counters of hook calls for each agent. The receiver alerts on a gap, a
digest change that no signed push explains, a chain head that goes back, a provider
that drops a level, and a counter that stays at zero while the agent runs. On a
graceful stop the supervisor sends a stop event. A silent stop is the alert.

## 6. Agent managed settings

This is the only lever that makes a tier B unhook need root on all three OSes with no
kernel work. The security role checked the vendor documentation on 2026-10-02. Each
row must be checked again for each agent release before Gryph relies on it.

| Agent | Managed location | Lock against the user | Status | Class |
|---|---|---|---|---|
| Claude Code | `/etc/claude-code/managed-settings.json` (and `managed-settings.d/`), `/Library/Application Support/ClaudeCode/`, `C:\Program Files\ClaudeCode\`, macOS profile, `HKLM\SOFTWARE\Policies\ClaudeCode` | `allowManagedHooksOnly: true`. A user or project `disableAllHooks` does not turn off managed hooks. | Verified from docs. HKCU and the WSL `/etc/claude-code` are user-writable. Do not use them. | A |
| Codex | `/etc/codex/requirements.toml` (Linux and macOS), `%ProgramData%\OpenAI\Codex\requirements.toml` (Windows), macOS MDM key `com.openai.codex:requirements_toml_base64` | `allow_managed_hooks_only = true`, pin `[features].hooks = true` | Verified from docs on 2026-10-03, for the paths and the keys. A `PreToolUse` error or timeout does not block, so the client must deny on its own. | A |
| Cursor | `/etc/cursor/hooks.json`, `/Library/Application Support/Cursor/hooks.json`, `C:\ProgramData\Cursor\hooks.json` | Enterprise priority over user. "Any deny wins." Set `failClosed: true`. | Paths verified. "The user cannot turn it off" is not stated. | B |
| Gemini CLI | System overrides `settings.json` (for example `/etc/gemini-cli/settings.json`) | System overrides win on conflict | Merge of hook lists is not verified | B |
| Windsurf and Devin | `/etc/devin/hooks.json` (legacy `/etc/windsurf/hooks.json`) | System hooks run first. No documented lock. | Linux path verified. Lock not verified. | B |
| OpenCode, Pi, Command Code | User plugin or user config | No managed tier known | Not verified | C |

Coverage classes for the public matrix:

- **A, admin-locked.** Locked profile applies to `HookConfig`.
- **B, system path, lock not verified.** Gryph installs at the system path, and
  reconcile still checks the user scope. The level is `Repair`.
- **C, user plugin.** `Detect` and `Repair` only. The fail-open logic lives in the
  Gryph plugin source, so the plugin must also return an explicit block on a client
  error.

Each row in `docs/agent-enforcement-coverage.md` gains the class, pre-blocking versus
post-only, and "verified on agent version X, date Y". A row drops to class B when
Gryph cannot check it again.

Product risk: `allowManagedHooksOnly` and `allow_managed_hooks_only` also turn off the
developer's own hooks. `gryph install --managed` keeps the lock as a separate admin
switch, off by default. The lock only stops extra user hooks. It is not needed for the
Locked claim.

Vendors can rename managed keys, and Locked then degrades without a signal. Each
class A row has a named owner and a CI check that runs against new agent releases.

A managed install counts as Locked only when three parts ship together:

1. The managed hook entry. For Codex, `[features].hooks = true` is always pinned.
   `allowManagedHooksOnly` and `allow_managed_hooks_only` stay an optional admin
   switch. Claude Code managed hooks hold without that switch, because a user
   `disableAllHooks` cannot turn them off.
2. An absolute hook command to a root-owned binary in a root-owned directory.
3. Managed policy from `systemConfigDir()` (the reserved `policy.yaml` and
   `policies/` slots in `config/system.go` become active), so the user and the agent
   cannot weaken policy.

Until the supervisor ships, the DB and the key stay user-owned, and the decision still
runs in a same-user process that is a child of the agent. A same-user process can
stop it (for example with `kill -STOP`) until the agent times out and allows the
action, or trace it where the OS allows. The claim for this step is: "The hook entry
and the policy files resist the non-admin user. The decision and the audit trail do
not." The decision process is a tier B gap until phase 2.

## 7. Supervisor architecture

### 7.1 Topology

```
 agent process (user U)
   |  hook: /usr/local/libexec/gryph/gryph _hook claude-code PreToolUse
   v
 thin client (user U): parse event, render response (adapter code)
   |  length-prefixed JSON over a local socket, peer credentials
   v
 supervisor (root, later root broker + _gryph worker)
   |-- decision.Local per user partition (label, redact, aarm Mediator, receipts)
   |-- storage.Store per user partition (single writer)
   |-- key custody (receipt.Signer)
   |-- config and policy (managed files, later signed cloud bundles)
   |-- selfprotect providers, reconcile
   |-- stream.Syncer (later cloud target)
```

| Concern | Thin client | Supervisor |
|---|---|---|
| Read stdin, select adapter, `ParseEvent` | Yes | Validates again |
| Phase, kind, link to pre event | No | Yes. It already ignores claimed values. |
| Label, redact, logging level | No | Yes |
| Session, events, receipts, signing | No | Yes, single writer |
| PDP, accumulator, approvals decision | No | Yes |
| Approval prompt on `/dev/tty` | Renders on request | Owns the decision |
| `RenderResponse`, exit code | Yes | No |
| Reconcile, self-protection, sync, config, policy | No | Yes |

Rule: the client keeps only agent-format work and has no authority. Anything that
writes state, holds a key, or decides runs in the supervisor.

Per OS:

- Linux: `gryph-supervisor.service` with `Restart=always`, `ProtectSystem=strict`,
  `ReadWritePaths=/var/lib/safedep/gryph`. Socket activation through
  `gryph-supervisor.socket` at `/run/safedep/gryph/hook.sock`.
- macOS: LaunchDaemon with `KeepAlive` and `Sockets`, at
  `/var/run/safedep/gryph/hook.sock`.
- Windows: a service with restart on every failure, named pipe
  `\\.\pipe\safedep-gryph-hook` with an SDDL that lets Authenticated Users connect
  only.

### 7.2 Multi-user and tenancy

One system supervisor serves every OS user. The tenant key is the kernel peer
credential of the socket, never a request field. Each user gets a partition with a
store, an accumulator, a receipt chain, and a deferral queue:

```
/var/lib/safedep/gryph/users/<uid>/                      Linux
/Library/Application Support/safedep/gryph/users/<uid>/  macOS
%PROGRAMDATA%\safedep\gryph\users\<SID>\                 Windows
```

Policy and keys have machine scope.

**Import of the old DB.** Root never opens a user's DB. On the first login after the
supervisor starts, the per-user unit (section 11.1) runs `gryph supervisor import` as
the user. It reads the user's old DB and sends the rows over the socket. The
supervisor stores them in the partition of the peer uid, marked `imported`, because
the user could change them before the import. The receipts keep their old signatures,
so the trust store (below) keeps the old public key to verify them.

**Reads.** A developer reads only their own partition. CLI reads in system mode use a
small read interface over the socket, scoped to the peer uid. A read-only DB file is
not an option, because SQLite WAL readers must write the `-shm` file. `storage.Store`
has about 60 methods, so phase 2 does not serve all of them. It defines a
`storage.ReadStore` with only the methods that the read commands use (`logs`,
`query`, `sessions`, `cat`, `diff`, `stats`, `cost`, `policy receipts`, `policy
approve`, `policy deferrals`), and a remote implementation of it. This also answers
the privacy question: a developer keeps access to their own log on a managed host.

**Keys.**

| Key | Linux path | Owner and mode |
|---|---|---|
| Receipt signing key | `/var/lib/safedep/gryph/keys/receipt.key` | root, `_gryph` after the split (section 7.5), mode 0600 |
| Export key | `/var/lib/safedep/gryph/keys/export.key` | same |
| Trust store (public keys) | `/etc/safedep/gryph/keys/receipt-pub.json` | root, mode 0644 |
| Cloud bundle root keys (phase 3) | `/etc/safedep/gryph/keys/` | root, mode 0644 |

macOS and Windows use the same layout under the paths above. The supervisor creates a
new machine key on first start. It does not import a user key, because same-user
malware may have read it. The hook client never reads a key. In managed mode,
`policy.receipts.key_path` and `policy.receipts.trust_store` come from the managed
file only, because `config.Load` then reads no user config and no environment.

In phase 1b, before the supervisor, the hook process signs in process, so the signing
key must stay user-readable in the user's config directory. Only the trust store can
move to `/etc/safedep/gryph/keys/receipt-pub.json` in that phase, set by the managed
config.

### 7.3 IPC trust boundary

- **Transport.** Unix socket on Linux and macOS, named pipe on Windows. No TCP.
- **Server identifies the client** from the kernel: `SO_PEERCRED` on Linux,
  `LOCAL_PEERCRED` and `LOCAL_PEERTOKEN` on macOS (the audit token, because pid reuse
  is real), `GetNamedPipeClientProcessId` and the impersonation token SID on Windows.
  `Event.AgentName`, cwd, and session ID stay untrusted claims.
- **Client identifies the server.** The client takes the socket path only from the
  managed config. It checks that the socket peer is uid 0 or `_gryph`, or on Windows
  that the pipe server runs as the service SID. Without this check, a same-user
  process can bind a fake socket and allow every request.
- **Framing.** A 4-byte big-endian length, then JSON. Max frame 8 MiB.
  `HookRequest` and `HookResponse` already have JSON tags.
- **Handshake.** `Hello{proto, client_version, caps}` and `Welcome{proto,
  server_version, mode}`. Unknown fields are ignored. An unknown frame type gets
  `Error{unsupported}`. An unknown verdict blocks, as `renderDecision` does today.
- **Frames.** `Handle`, `Prompt` and `PromptReply`, `ReportHookError` (closes the
  known gap in `decision/service.go`), `Query` (reads for the CLI).
- **Limits.** For each peer uid: a token bucket, a bound on concurrent connections,
  a bound on request size, and a deadline for each request. The decoder gets the same
  fuzzing as `aarm/shellcmd` (commit `bf628ea`).
- **Windows pipe squatting.** The server creates the pipe with
  `FILE_FLAG_FIRST_PIPE_INSTANCE`. The client checks the server with
  `GetNamedPipeServerProcessId` and the token SID.
- **Concurrency.** One writer for each partition, and a mutex for each session around
  snapshot, PDP, append, and receipt. This keeps the accumulator and the hash chain in
  order, and it replaces SQLite lock contention between hook processes.

What a same-user process can still forge: anything that it can run, it can run as
the hook. It can send a synthetic prompt event to reset
`context.actions_since_intent`, send fake post results, flood events, or never call
the hook. Mitigations:

1. Bind each session to the peer uid and to the first agent ancestor process seen.
   Flag a mismatch. The ancestor check is a signal, not proof.
2. A prompt event from a peer that fails the ancestor check is low trust. It cannot
   lower a decision.
3. Keep `gryph-builtin-hook-command` as defense in depth. Do not call it a control.
4. A forger can poison its own new audit rows. It cannot erase or rewrite earlier
   receipts.

### 7.4 Confused deputy

The supervisor never opens a client-claimed path with its own privilege. Today
`loadSession` calls `projectdetection.DetectProject(event.WorkingDirectory)` and the
session-end path reads `TranscriptPath` for cost. Under root these become arbitrary
file reads. In system mode, the client sends bounded bytes and project data as
untrusted claims. The only privileged write into a home is the uninstall cleanup
through the uid-drop helper (section 11).
The spool ingest follows the same rule (section 8.1): no-follow opens, regular
files with one link only, and the file owner uid decides the partition. This is a
requirement for system mode.

### 7.5 Privilege split

The worker parses untrusted JSON, runs the shell analyzer and CEL, and holds the key.
That is the attack surface, and it must not run as root in a general release.

- Beta: one root process behind a flag, with fuzzing of the IPC decoder.
- General availability of system mode on an OS: `gryph supervisor broker` runs as
  root (socket creation, the uid-drop helper for uninstall, providers, worker start). `gryph supervisor
  worker` runs as `_gryph` (or a Windows virtual service account) and owns the
  partitions, keys, PDP, and sync. The broker passes the listening socket to the
  worker.

The interfaces do not change at the split. On Windows the worker can own the pipe
directly, with no handle passing.

### 7.6 Code reuse and changes

Code that does not change: `decision.Service`, receipts, `storage.Store`, adapter
`ParseEvent` and `RenderResponse`. The aarm Mediator and `decision.Local` keep their
shape, with the small changes below.

Code that changes:

- `cli/root.go`: move `NewApp`, `DecisionService`, `lazyPolicyCheck`, and
  `registerAdapters` into a `runtime` package that the CLI and the supervisor both
  import. This is a prerequisite.
- `decision`: add `ReportHookError`. Add `decision/ipc` with a client and a server.
- `cli/hook.go`: restructure `NewHookCmd`, not only add `resolveService` (section
  8.2). Today it opens the store before it reads stdin, and `runHook` returns exit 1
  on an unknown agent, a `ParseEvent` failure, and a `Handle` error. Most agents treat
  exit 1 as allow. In client mode, the command reads stdin first, opens no store, and
  maps every failure (unknown agent, parse error, deadline, rate limit, decode, server
  error) to the managed verdict for the hook through `renderDecision`. It never
  returns a non-zero exit that does not block.
- `decision.Local`: `loadSession` stops calling `projectdetection.DetectProject`. It
  takes the project claim from the request through an option.
- `decision.HookRequest`: gains a project claim and, on a session end, cost totals.
  Cost needs every usage record of the transcript, so a bounded excerpt does not work.
  The client runs `collectSessionCost` (`cli/cost.go`) as the user and sends the
  totals as untrusted claims. The supervisor stores them marked as client-reported.
- `aarm`: a `degraded` option on the Mediator, set only in `local-ephemeral`. When it
  is set, a rule that reads `context.*` resolves to block. The PDP already finds the
  context references of each rule (`collectContextRefs` in `aarm/pdp/pdp.go`). The
  gate is the explicit flag, not the accumulator type, because `Nop` is the default
  accumulator of `NewMediator` and `gryph policy test` uses it.
- `aarm/loader/builtin.go`: `gryph-builtin-hook-command` also blocks an agent command
  that runs `gryph policy approve resolve` or `gryph policy deferrals resolve`. Today
  its condition is `action.gryph_hook == true` only.
- `cli/policy_approve.go`: new `show`, `watch`, and `resolve` commands, and `--scope`.
  In managed mode, `resolve` (and `policy deferrals resolve`) ask for confirmation on
  `/dev/tty` and refuse `--yes`.
- `storage`: a `ReadStore` interface for the read commands, with a remote
  implementation over the socket.
- `agent.InstallOptions`: `Scope`, `Command`, `Home`. Optional `ScopedAdapter`.
- `events.HookSpec`: `Timeout`, set for each adapter from the agent's real hook
  timeout.
- `config`: a `supervisor` section that only the managed file can set. When a
  managed config file is active, `config.Load` binds no `GRYPH_*` environment
  variable, on every command including `_hook`. Done on 2026-10-03: before the fix,
  `AutomaticEnv` also applied over the managed file, so a user could turn off
  managed policy with `GRYPH_POLICY_ENABLED=false`.
- Grants: new storage keyed by the canonical action digest, the session, and the
  scope. Today a deferral resolution writes only a follow-up receipt and creates no
  grant.
- `aarm/loader/builtin.go`: system paths join the protected globs.
- `approval.Outcome`: an `Assurance` field.

### 7.7 Binary and commands

The supervisor is open source and lives in this repository. The owner accepts one
binary or two. The recommendation:

- Start with **one binary**. The supervisor runs as `gryph supervisor run` (and
  later `gryph supervisor broker` and `gryph supervisor worker`). One artifact to
  sign and notarize. The hook client and the supervisor always have the same
  version, so skew comes only from an upgrade in progress.
- Keep the `supervisor` package free of cobra and of `cli`. It imports the
  `runtime` package (section 7.6). A separate `cmd/gryph-supervisor` then needs only
  a small `main.go`.
- Revisit at the broker and worker split (section 7.5). A small root broker binary
  that does not link the PDP, CEL, and the shell analyzer has less attack surface
  as root. Build it as a second binary from this repository if the size and
  dependency audit shows a gain.

The hook command always names the binary at its root-owned absolute path. It never
names a separate client binary.

## 8. Availability and fail modes

### 8.1 Matrix

Only the root-owned managed config sets these values, with the key
`supervisor.unavailable.{blocking,prompt,other}`. A user config cannot set them. The
owner confirmed `enforce` (block for blocking hooks) as the default on a managed
host. `pilot` stays an opt-in profile for a first rollout.

The column comes from the adapter's `HookSpec`, not from the phase alone. A hook with
the `Prompt` flag uses the prompt column. Any other hook with `Blocking` set uses the
blocking column. Every other hook (post hooks, and pre-phase hooks that cannot block,
such as `SessionStart` or `Notification`) uses the other column.

| Deployment | Blocking hook | Prompt hook | Other hooks |
|---|---|---|---|
| Standalone (today) | `local` | `local` | `local` |
| System supervisor, default (`enforce`) | `block`, with one stderr line | `allow`, spooled | spooled |
| System supervisor, admin opt-in (`pilot`) | `local-ephemeral` | `allow`, spooled | spooled |
| Container, WSL, or devcontainer with no visible managed config | standalone `local` | `local` | `local` |
| Container, WSL, or devcontainer with a visible managed config but no socket | admin choice: `block` or `local-ephemeral` | `allow`, spooled | spooled |

Definitions:

- `local`: today's in-process `decision.Local` with the user's DB.
- `local-ephemeral`: the client evaluates the root-owned managed policy plus the
  built-in rules with an in-memory store. It has no context state and does not sign.
  A rule that reads `context.*` resolves to block. The decision goes to the spool
  marked `degraded`. This mode needs world-readable (0644) managed policy files.
- Spool: a root-owned drop directory with mode 1733. A user cannot list or read the
  spool files of other users. A user's own rows stay `unverified`. The supervisor
  opens each spool file with no-follow, accepts only a regular file with one link,
  and assigns its rows to the partition of the file owner uid, never to a uid in the
  file content. A per-uid quota bounds the spool. Rows never change the accumulator.
- `local-ephemeral` uses an in-memory SQLite `storage.Store` and a `Nop` accumulator,
  with the Mediator `degraded` flag set (section 7.6).
- After an outage, context counters can be stale. Spooled rows never change the
  accumulator, so a prompt from the outage does not reset
  `context.actions_since_intent`. Under `enforce` this is the safe direction: the
  counter is higher than the truth, so rules on it trigger earlier. Under `pilot`,
  actions that ran in `local-ephemeral` are not counted, so the counter can be lower.
  The supervisor marks each session that spans an outage as `degraded` in its
  receipts, so a review can find them.

Why `block` is the default in system mode: the socket is root-owned, so a same-user
process cannot remove it. But a same-user process can flood its own uid's rate limit
and so force deadlines. Therefore:

- The fallback triggers **only** when the client does not complete the handshake:
  no socket, a refused connect, or no `Welcome` frame within the handshake budget.
  With socket activation (systemd `.socket`, launchd `Sockets`) the kernel accepts a
  connect even while the supervisor is down or restarting. So a successful connect
  alone proves nothing. The `Welcome` frame is the proof that the supervisor runs.
- After `Welcome`, a deadline, a rate limit, or a server error gives `block` for
  blocking hooks, and `allow`, spooled, for prompt and other hooks.
- A failed server identity check gives `block` in every mode and records a tamper
  event. It never triggers the fallback.
- The client never falls back to a user socket or a user DB when a managed config
  exists.
- In `pilot`, a same-user flood can fill the listen backlog, cause a refused connect,
  and so force `local-ephemeral`. This is acceptable: context rules still block, and
  the user gains no signing that they can exploit.

MDM templates offer both profiles. `pilot` suits a first rollout. `enforce` suits a
stable fleet.

### 8.2 Client rules

- The client never crashes and never hangs. It returns an explicit verdict before the
  agent's own timeout. Budget: the `HookSpec.Timeout` minus a margin. Handshake budget
  (connect plus `Welcome`) 300 ms, which covers a supervisor restart under socket
  activation. Non-escalate decision deadline 2 s.
- On a block caused by an outage, stderr shows one line: "Gryph supervisor is not
  running. Run `gryph doctor`." In managed mode it appends the org support text.
- On a policy block, stderr shows one line with the rule ID and the receipt ID, plus
  the command that shows the receipt. In managed mode it appends: "Managed by <org>.
  Request an exception: `gryph exception request <receipt-id>`." The agent gets the
  rule message as guidance.
- Socket activation on Linux and macOS keeps the socket open across a restart or
  upgrade. The client waits for `Welcome` within the handshake budget. On Windows the
  client retries the pipe within the same budget.
- With socket activation, the peer credential of the listening socket is the
  process that created it (systemd or launchd, uid 0). The server identity check
  accepts uid 0 for an activated socket.
- The protocol handshake and the "unknown verdict blocks" rule cover version skew
  during an upgrade.

```go
func resolveService(ctx context.Context, rt *runtime.Core) (decision.Service, func(), error) {
	sup := rt.Config.Supervisor
	if !sup.Enabled() {
		return rt.LocalService(ctx)
	}
	c, err := ipc.Dial(ctx, sup.Socket, ipc.ExpectServer(sup.ServerIdentity))
	switch {
	case err == nil:
		return c, c.Close, nil
	case errors.Is(err, ipc.ErrConnect):
		return rt.FallbackService(sup.Unavailable), func() {}, nil
	default:
		rt.RecordTamper(ctx, err)
		return rt.BlockService(), func() {}, nil
	}
}
```

`ipc.Dial` connects and waits for `Welcome`. It returns `ErrConnect` only for a
missing socket, a refused connect, a missing pipe, or no `Welcome` within the
handshake budget. Any other error, including a server
identity mismatch, gives the block service and a tamper event. It never calls
`FallbackService`. The client has no DB write access, so it writes the tamper event
to the spool.

### 8.3 Mode selection

The client does not switch to supervisor mode only because a managed config exists.
The managed config must turn it on:

```yaml
# /etc/safedep/gryph/config.yml
supervisor:
  enabled: true
  socket: /run/safedep/gryph/hook.sock
  server_identity: _gryph        # root before the broker and worker split
  profile: enforce               # enforce | pilot
```

| Managed config | `supervisor.enabled` | Hook client behavior |
|---|---|---|
| Absent | n/a | Standalone. In process, user DB, user key. A user config or a `GRYPH_*` variable cannot turn on supervisor mode. |
| Present | unset or `false` | Locked without a daemon (phase 1b). In process, managed policy, user DB, user key. |
| Present | `true` | Client mode, on every hook. When the socket is not reachable, the fail-mode matrix (section 8.1) applies. The client never falls back to the user DB. |

Reasons for an explicit switch:

- Phase 1b hosts have a managed config but no supervisor. They must keep working.
- The presence of a socket must never change the trust decision. The socket path
  and the expected server identity come only from the root-owned config.
- Once on, the switch is sticky. An outage gives the configured fail mode, not a
  silent return to the user DB.

`gryph install --managed` controls the order, so a rollout cannot block agents by
mistake. It installs and starts the supervisor units, waits for a successful health
check over the socket, and only then writes `supervisor.enabled: true`. `gryph
uninstall --managed` does the reverse: it clears the switch first, then stops the
units.

## 9. Approvals

### 9.1 Channels and assurance

| Channel | Assurance | Plan |
|---|---|---|
| Client-side `/dev/tty` prompt. The supervisor sends `Prompt{nonce, action_digest, deadline}` on the open connection. The client renders it with `CLIPrompt` and returns `PromptReply`. The supervisor accepts each nonce once, on the same connection, before the deadline. | `same-user-tty` | Phase 2 |
| Local admin. A member of the admin group answers with `gryph policy approve` over the supervisor socket. | `local-admin` | Phase 2 |
| Local admin who is the same person as the developer, through `sudo` | `self-elevated` | Phase 2, off by default |
| SafeDep Cloud queue, with an SSO identity | `out-of-band` | Phase 3 |
| OS-native prompt (polkit, macOS LocalAuthentication, Windows credential UI) | `local-auth` | On request |

Every approval receipt records its channel, its assurance, and the approver identity.
The nonce stops replay. It does not stop a same-user forgery, and the docs say so. A
rule can set `min_assurance`. When no channel meets it, the action becomes a
deferral. It never falls back to a weaker channel.

Unmanaged hosts keep today's behavior: the `/dev/tty` prompt and the deferral queue.

### 9.2 Escalation on a managed host

On a managed host, the owner decided that both the cloud queue and a local admin can
approve. The developer cannot approve their own escalation, unless a rule sets
`min_assurance: same-user-tty` for a low-risk action.

Configuration, in the managed config or a signed cloud bundle:

```yaml
approval:
  channels: [cloud, local-admin]   # both by default on a managed host
  inline_wait: 15s                 # the client bounds it by the hook timeout
  request_ttl: 30m                 # an unanswered request expires as a deny
  grant_ttl: 15m                   # how long an approval stays usable
  max_grant_scope: once            # once | session | window
  local_admin:
    group: gryph-admins            # an explicit group, not every sudoer
    allow_self_elevated: false
```

A policy rule can narrow the channels for its own escalations, for example
`approvers: [cloud]` for a production deploy command.

On a single-developer laptop, the local admin channel is mainly for IT staff over
SSH. The cloud queue is the practical approver for a fleet. The local admin channel
matters most on shared hosts and build machines.

### 9.3 Flow

```
agent  -> pre hook -> client -> supervisor: rule decides escalate
supervisor: create request R (action digest, rule, user, host, session, summary)
supervisor: send R to every channel at the same time
   cloud queue (when online)       local admin queue (always)
supervisor: record the request receipt, release the session lock
client: wait inline up to the wait budget, show a status line on /dev/tty
   answer in time  -> allow or block now, receipt records approver and assurance
   no answer       -> block with a pending message, R stays open as a deferral
later: an approver answers R
   supervisor stores a grant bound to the action digest, scope, and grant_ttl
   next hook in the session returns guidance: "R was approved. You can retry."
agent retries the same action -> supervisor matches the grant -> allow, grant used
```

The supervisor is the single arbiter. The first valid answer wins, by the
supervisor's clock. It records a later answer as `superseded` and does not apply it.
A deny is final for that request.

The supervisor does not know the agent's hook timeout, so the client sets the wait
budget: `min(inline_wait, HookSpec.Timeout minus margin)`. When the budget is 2 s or
less (for example OpenCode, which kills the hook after 5 s), the client skips the
inline wait and returns the pending block at once. A wait that outlives the agent
timeout fails open, because the agent then allows the action.

The supervisor records the request receipt and releases the session lock (section
7.3) before the inline wait. It takes the lock again to append the outcome receipt.
So parallel tool calls in the same session do not stall behind an approval. During
the wait, the context counters of those parallel calls see the request entry only,
not its outcome.

Cloud answers come through polling, so they rarely arrive within the inline wait. The
grant-and-retry path is the main path for cloud answers. The supervisor uses a
long-poll while a request is open.

### 9.4 What each person sees

**Developer, in the agent terminal.** During the inline wait, the client writes one
status line to `/dev/tty` when a terminal exists:

```
gryph: approval needed for rule prod-deploy (request R-7F3K). Waiting up to 15s.
```

When the wait ends without an answer, stderr shows:

```
gryph: blocked, approval pending. Request R-7F3K was sent to <org> admins.
       Check status: gryph policy approve show R-7F3K
```

The agent gets this guidance: "This action needs approval. Request R-7F3K is
pending. Tell the user. Do not retry until the user confirms that it is approved."
This stops an agent that loops on retries.

When the request resolves, the developer sees it in three places: `gryph policy
approve show`, a notice on the next hook in the same session, and (with Cloud) the
notification channel of the org, for example Slack or email to the requester. A deny
carries the approver's note.

**Local admin.** The admin uses the CLI on the host, locally or over SSH:

```
$ gryph policy approve list
ID       USER   AGENT        RULE         ACTION                         AGE
R-7F3K   dev1   claude-code  prod-deploy  command_exec: make deploy-prod  40s

$ gryph policy approve show R-7F3K
$ gryph policy approve resolve --id R-7F3K --decision allow --scope once --note "release window"
$ gryph policy approve resolve --id R-7F3K --decision deny --note "use the staging target"
```

`gryph policy approve watch` follows new requests in a terminal. The commands need no
`sudo`. The supervisor reads the peer credential and checks the admin group.

These commands extend the ones that exist. Today `gryph policy approve list` always
reports zero pending, because the `/dev/tty` prompt has no cross-process queue. The
supervisor gives it one. An escalation that is still pending after the inline wait is
a deferral row, so `gryph policy deferrals` also shows it, and
`gryph policy approve resolve` uses the same flags as `gryph policy deferrals
resolve`, plus `--scope`.

**Cloud approver.** The request shows in the dashboard queue and in the configured
channel (Slack, email, webhook) with a redacted summary: user, host, agent, rule,
the normalized action, and the recent context entries of the session. The summary
uses the `policy` export profile (section 10.4) at every collection level. The
approver sees the action, not what it runs: for `make deploy-prod` the approver sees
the command, not the Makefile. The docs state this limit. The approver
picks a scope (once, this session, or a time window up to `max_grant_scope`) and can
add a note. The cloud sends the answer to the host as a signed message, bound to the
request ID, the device, the action digest, and the scope, with a single-use ID
(section 10.2). The supervisor polls
the cloud. The host makes only outbound connections.

### 9.5 Rules

- **Identity.** A local admin is a member of `approval.local_admin.group`, checked
  from the peer credential. The approver must not be the developer. The supervisor
  rejects an answer when the approver peer uid equals the requester uid, whatever
  the group. It also compares the audit login identity: `loginuid` on Linux, the
  audit user ID in the macOS audit token, and the linked token user SID on Windows.
  An unset audit identity on either side (for example `loginuid` 4294967295 in a
  container) counts as a match. A match through `sudo` gives `self-elevated`, which
  the admin can allow. Root can fake the audit identity. That is tier C, which only
  off-host evidence detects.
- **Groups.** `SO_PEERCRED` gives only the primary gid. The supervisor resolves the
  supplementary groups of the peer uid from the OS group database for the admin
  group check.
- **An admin's own agent.** The uid check does not stop an agent that runs as an
  admin from answering another user's request, because the uids differ. So in
  managed mode, `resolve` asks for confirmation on `/dev/tty` and refuses `--yes`. An
  agent has no way to type on the terminal of the admin. The supervisor also applies
  the agent ancestor signal (section 7.3) to the approver connection, and refuses an
  answer from a process under a known agent. The built-in rule
  `gryph-builtin-hook-command` also blocks an agent shell command that runs
  `gryph policy approve resolve` or `gryph policy deferrals resolve`. That rule is
  defense in depth only.
- **Grants.** A grant is bound to the canonical normalized action, the session, and
  the scope. `once` matches the same action digest one time. `session` matches the
  same action digest, or a narrower pattern that the approver sees and confirms, for
  the rest of the session. `window` does the same for this user and project for a
  set time. A grant never matches the whole rule. `max_grant_scope` defaults to
  `once`. A changed action needs a new request. A persistent change is a policy
  change through the exception flow (section 10.3), not an approval.
- **Offline.** When the cloud is not reachable, the local admin queue still works.
  The supervisor syncs the request and its answer to the cloud later. When no
  channel is available, the request waits until `request_ttl`, then expires as a
  deny. The next hook in the session shows "Request R-7F3K expired without an
  answer." `gryph doctor --managed` reports "no approver channel" when the host is
  not enrolled and the local admin group is empty.
- **Post hooks.** An `escalate` on a post or unknown phase hook cannot stop the
  action. It creates a review item in the same queues and does not wait.
- **Audit.** The receipt of the request, the answer, and each use of a grant are all
  in the receipt chain, with the approver identity and the assurance.

## 10. Control plane

### 10.1 Seams

| Concern | Seam | Today | Later |
|---|---|---|---|
| Config | `config.Load`, managed file wins as a whole | user file, managed file | signed cloud bundle writes the managed layer |
| Policy | `loader.Source`, layers merge | `policy.yaml`, `policies/`, built-in | managed `policies/`, then a cloud bundle `Source`. A managed `allow_user_policy: false` drops user sources. |
| Event sync | `core/stream.Target`, `stream.Syncer` with cursors | `gryph stream sync` | supervisor runs the syncer. Cloud is one more target. `StreamItem` gains receipts. |
| Key custody | `receipt.Signer` | user file | supervisor file under `_gryph`. TPM or Secure Enclave later. |
| Approval | `approval.Service` | `CLIPrompt`, `Nop` | channels in section 9 |
| Health | `AssetStatus` | `doctor` | signed status report to a sink |

No new interface is built for these until a second implementation ships. The policy
merge rules already give the right safety: a new file can only add rules, `disabled:`
acts only on its own file, and the built-in rules load last.

### 10.2 Cloud trust model

This section has no code until the cloud API exists. It is written now because a first
remote fetch without rollback protection becomes the attack, and a later change would
alter the wire format.

- **Bundles** for config, policy, and remote commands are signed and carry `tenant`,
  `device_id`, a monotonic `version`, `issued_at`, `expires_at`, and a content digest.
- The supervisor rejects a lower version (rollback), an expired bundle (freeze), and a
  bundle for another device. It keeps the highest version seen in its own store.
- **Keys** follow the TUF model: an offline root key and an online signing key, with
  rotation signed by the root. Use `go-tuf` or Sigstore. Do not write a new
  implementation. Pinned keys live in `systemConfigDir()/keys/`. The root of trust
  never travels on the same channel.
- **Offline**: keep the last good bundle until `expires_at`, then apply the managed
  fallback (a baseline policy).
- **Remote commands** such as uninstall, break-glass, or turning off self-protection
  are signed bundles too, never plain API calls.
- **Event sync** batches carry sequence numbers and chain heads. This is the external
  anchor that [security-policy.md](./security-policy.md) lists as future hardening.
  Delivery is at least once, with idempotent receipt IDs.
- **Device identity**: a short-lived, single-use enrollment token gives a device
  certificate, bound to a hardware key where possible. mTLS to the cloud.
  Re-enrollment is an audited event.
- **Verification failure**: a bundle that fails verification does not apply. The
  supervisor keeps the last good bundle and records a tamper event. It keeps one
  version counter for each bundle type. Each remote command carries a single-use ID.

Key custody note: the Secure Enclave supports P-256 only. Receipt signing is Ed25519
today. Hardware custody on macOS needs an ECDSA option or a wrapped key, so it waits
for a receipt format decision.

### 10.3 Break-glass and exceptions

- `policy.self_protection.enabled` is ignored when a managed config exists.
- Break-glass is a signed, time-boxed override for one host. A local admin key or the
  cloud issues it. It is recorded as a receipt.
- `gryph exception request <receipt-id> --reason` prints a policy snippet in OSS. In
  Managed it opens a request in the admin queue, on top of the deferral model.

### 10.4 Cloud data collection

The cloud becomes the policy control plane for Gryph. It needs events, receipts, and
the data that it uses to create, test, and evaluate policies. It does not collect all
session data by default. A team can turn on full collection at its own risk.

Collection has three levels. The level comes from the managed config or a signed
cloud bundle, never from the user config.

| Level | Default | What leaves the host | Purpose |
|---|---|---|---|
| `evidence` | Always on in Managed | Signed status reports, tamper events, receipts (chain head, decision, rule IDs, action type, phase, agent, assurance), approval records, hook-call counters. No event content. Each escalation also sends a redacted summary of the action and its recent context to the approval queue (section 9.4). | Fleet health, tamper detection, compliance evidence |
| `policy` | Default in Managed | `evidence`, plus each event through a new built-in export profile `policy`. The profile includes the fields that policy rules match on: tool name, normalized path, command, URL and host, shell analysis targets, MCP server and tool, classes, injection score, origin. It digests prompts. It digests or drops file content and tool output. It drops every value with class `secret`, `pii`, or `unknown_sensitive`. | Policy authoring from observed actions, replay of candidate policies, shadow evaluation |
| `full` | Off. Opt-in, chosen by the team, at its own risk. | `evidence`, plus each event through the built-in `full` export profile: prompts, tool input, tool output, and content. | Content rules, investigations |

Rules:

- Collection reuses the export path that exists today: `Event.ForExport(profile)`,
  the keyed digests, and the rule that a digest is never a plain sha256
  ([content-labels.md](./content-labels.md#export)). The cloud is one more stream
  target with a fixed profile for each level.
- The redactor runs at store time for every level. `full` never sends a value that
  the local store redacted.
- Commands and URLs can carry secrets, such as a bearer header or a token in a
  query. The `policy` profile removes URL userinfo and query values, and runs the
  redactor again on command, path, and URL fields at export.
- The `policy` profile needs one extension: export rules today match on class and
  origin only. The profile must also match on the field (for example `command` or
  `path` versus `content` or `output`). This change lands in `core/privacy` before
  phase 3.
- A change of level is a self-audit event. The developer sees one notice on the
  first session after enrollment and after each change of level: "Your organization
  collects Gryph data at level <level>. This includes the commands and file paths
  that agents use. Run `gryph doctor` to see what leaves this device." For `full`,
  the notice says "prompts, commands, file paths, and file content". `gryph doctor` always shows the current level and the cloud target.
- The local log stays readable by the developer at every level (section 7.2).
- A content rule (`content_patterns`) evaluates on the host at every level. Only
  its replay in the cloud needs `full`.

### 10.5 Policy control plane

With the `policy` level, the cloud can manage the policy life cycle. The engine on the
host and in the cloud is the same `aarm/pdp` code.

1. **Author.** An admin drafts a rule from observed actions in the cloud, or writes
   YAML with the authoring skill.
2. **Test.** The cloud replays the candidate policy against the collected events of
   the tenant, with the same engine as `gryph policy test`, and shows the decisions
   that would change.
3. **Shadow.** A signed bundle carries the candidate in `observe` mode. The supervisor
   evaluates it next to the active policy and records what it would decide as a
   receipt marked `shadow`. A shadow decision never changes the verdict that the
   agent gets. The cloud reports the difference.
4. **Roll out.** The admin promotes the candidate to `enforce`, first for a host group,
   then for the fleet. Each step is a signed bundle with a higher version.
5. **Roll back.** A new bundle with a higher version that holds the earlier policy.
   The supervisor never accepts a lower version (section 10.2).

Shadow evaluation is new work in the Mediator. It runs a second PDP and records the
result. It must not write to the accumulator, so that the shadow policy cannot change
the context of the active policy. A shadow evaluation does not call the approval,
deferral, or identity audit steps. A shadow `escalate` or `defer` creates no request
and no deferral.

## 11. Packaging and distribution

| Mode | Channel | Contents | Plan |
|---|---|---|---|
| User | brew, npm, `install.sh`, `go install` | Binary only. No daemon. Guard profile. | Unchanged |
| Locked, Linux | deb and rpm (goreleaser `nfpms`), GPG signed | Root-owned binary under `/usr/libexec/gryph`, units, managed config, managed agent settings | Phase 1b (no daemon), phase 2 (supervisor) |
| Locked, macOS | Signed and notarized pkg, brew cask that wraps it | LaunchDaemon, managed files, configuration profile | Phase 2. Needs an Apple Developer ID and notarization in CI. The plain binary also gets signed. |
| Locked, Windows | MSI (WiX), Authenticode | Service with recovery actions, managed files | After Linux and macOS are stable |

In managed scope, `gryph doctor` refuses a binary that root does not own, for example
a brew or npm install.

Uninstall removes hooks. With managed settings this is one file. Class C agents have
user-scope hooks, and uninstall cannot wait for each user to log in, because a hook
that calls a removed binary fails on every agent call. So `gryph uninstall --managed`
walks homes, and only for this job. It uses a helper that drops to the target uid and
gid before it opens a file, with the rules in section 5.3. This is the only root
process that touches a user's home. Root can always uninstall. The docs say so.

### 11.1 MDM deployment contract

MDM deployment is in scope as a design input. The MDM scripts themselves are out of
scope. The design assumes this about every MDM script:

- It runs as root (or SYSTEM on Windows), with no interactive user.
- It can run before any user logs in, and before any agent is installed.
- It can run again at any time, for example on each check-in or upgrade.
- It cannot know which users will use which agents.

Gryph gives the scripts a small, stable contract:

| Step | Command | Behavior |
|---|---|---|
| Place the binary | The package (deb, rpm, pkg, MSI) or a copy by script | Root-owned binary in a root-owned directory |
| Configure and install | `gryph install --managed --config <file> [--enroll-token-file <file>] --json` | Validates the config, then writes it atomically to `systemConfigDir()`. Writes managed policy. Writes the managed hook entries for every class A and class B agent in the config allowlist, also when the agent is not installed yet. Installs and starts the supervisor units (phase 2 and later). Enrolls the device (phase 3). |
| Report compliance | `gryph doctor --managed --json` | Profile, per-agent class and level, supervisor state, policy version, collection level, last sync. MDM tools read it as a compliance attribute (for example a Jamf extension attribute or an Intune custom compliance script). |
| Remove | `gryph uninstall --managed [--purge]` | Sends a signed stop event when the host is enrolled. Removes the managed files, the units, and the hooks. Keeps the partitions and the receipt chain unless `--purge` is set. |

Rules for these commands:

- **Idempotent.** A second run with the same input changes nothing and exits 0. A
  run with a new config applies only the difference.
- **Exit codes.** 0 success, 1 failure, 3 invalid config (nothing changed), 10
  partial success (some agents degraded, the JSON lists them).
- **No environment trust.** Under `--managed`, Gryph ignores `HOME`, `XDG_*`, and
  `GRYPH_*`. All paths come from `systemConfigDir()` and the platform defaults. Config
  keys already ignore `GRYPH_*` when a managed file is active (commit `426480f`). The
  path variables (`GRYPH_CONFIG_DIR`, `GRYPH_DATA_DIR`, `GRYPH_CACHE_DIR`, `XDG_*`)
  are still read in `config/paths.go`. An MDM root run has no `SUDO_USER`, so the
  sudo guard does not apply, and a stray variable can move where root writes its
  self-audit rows. Phase 1b closes this.
- **No secrets on the command line.** The enrollment token comes from a file that
  only root can read. Gryph deletes the file after a successful enrollment.
- **No writes into user homes.** Class A and class B agents use machine-wide managed
  files. Class C agents need user-scope hooks. The package installs a system-wide
  user unit: a systemd user timer under `/etc/systemd/user`, a LaunchAgent under
  `/Library/LaunchAgents`, or a Windows logon task. It runs `gryph supervisor
  reconcile --once` as each user, at login and on a timer. It runs as that user, so
  no uid drop is needed. This applies from phase 1b. Install never walks homes.
  Only uninstall does (section 11). The same unit runs `gryph supervisor import` once
  after the supervisor starts (section 7.2).
- **Safe input files.** Gryph opens `--config` and `--enroll-token-file` with
  no-follow, and only when root owns the file and its parent directory and neither
  is group- or other-writable. It deletes the token with `unlinkat` on the opened
  directory. When the host is already enrolled, a missing token file is a no-op.
- **No program execution through `PATH`.** Today agent detection runs the agent
  binary (for example `claude -v`) through `PATH`. As root, that runs a binary that
  a user can write, for example in `/opt/homebrew/bin`. Under `--managed`, Gryph
  never runs an agent binary or any program found through `PATH`. Detection uses
  file checks only.
- **Allowlist changes.** Removing an agent from the allowlist removes its managed
  entries on the next run.
- **New agents later.** When a developer installs a class A agent after deployment,
  the managed hook entry is already in place. Reconcile reports the new agent in the
  next `doctor` run.
- **Upgrade.** A package upgrade replaces the binary. Socket activation keeps the
  socket open, and the protocol handshake covers skew. A new run of
  `gryph install --managed` is safe after an upgrade.

The managed config that the script passes holds the org settings: the fail-mode
profile (`enforce` or `pilot`), the approval routing (section 9.2), the collection
level (section 10.4), the agent allowlist, and the lock switch for each class A
agent.

## 12. Testing

- **Same binary, real contracts.** testscript starts `gryph supervisor run
  --foreground --socket $WORK/gryph.sock` in the background, then kills it. Most
  contracts need no root.
- **Test seams.** The acceptance binary cannot set `globalConfigDirOverride`. Add a
  build tag that turns on an env override for the system directory, the socket path,
  and the server identity check. A CI check asserts that release builds do not have
  it.
- **Determinism.** `gryph supervisor reconcile --once`. Scripts never sleep and poll.
- **Chaos scripts**, each a txtar with a `catalog.yaml` row: kill the supervisor during
  a request and assert the fail mode, delete hook entries and assert detect then
  repair, a symlinked `settings.json` that points outside the home and assert no
  write, invalid JSON and assert no clobber, an old client against a new server and
  the reverse, a fake socket owned by the user and assert that the client refuses it,
  a full spool and assert the bound.
- **Privileged job.** A root container with two users covers peer credentials, the
  uid drop, partitions, and file ownership after repair. A macOS runner covers a
  launchd smoke test. A Windows acceptance leg covers the named pipe and the service.
- **MDM contract.** Scripts run `gryph install --managed` twice and assert no change
  and exit 0, run it with an invalid config and assert exit 3 and no change, and
  assert that a set `HOME` or `GRYPH_*` variable has no effect under `--managed`.
- **Inline wait bound.** An `inline_wait` greater than the adapter timeout gives a
  pending block before the agent timeout.
- **Socket activation.** With the socket held by systemd and the supervisor stopped,
  a hook gets the fallback after the handshake budget, not a post-connect block. With
  `enforce` that is a block, with `pilot` it is `local-ephemeral`.
- **System session.** A reconcile tamper event and an ingested client tamper event
  land in the system session of the right uid, and `verify` passes on that chain.
- **Approvals.** The privileged job covers a local admin in the admin group, a
  non-member who is refused, an admin answer from the requester uid that is refused, a `self-elevated` answer when it is off, two answers to
  one request (first wins, second is `superseded`), a grant used once, and a request
  that expires as a deny.
- **Latency guard.** A benchmark for hook p99 through the client, in CI.
- **Kernel providers** get a separate privileged nightly job.

## 13. Phased plan

Each phase ships alone and has exit criteria.

### Phase 1a: Foundations (OSS, no root)

- Measure hook latency per phase today. This sets the latency targets for later
  phases.
- Install hooks with an absolute binary path in the user scope.
- Move the hook-error self-audit write behind `decision.Service`.
- Extract the `runtime` package from `cli/root.go`.
- `selfprotect.Provider` with the user-scope provider. `gryph doctor` reports
  `AssetStatus` per asset and agent. `gryph supervisor reconcile --once` and an
  optional timer. Detect on by default, repair opt-in.
- Define the `AssetStatus` and tamper event schema. Record tamper events in the
  per-user system session (section 5.2).
- Add `HookSpec.Timeout` for each adapter.

Exit: every adapter that supports the user scope has a catalog row for detect and
repair, and each row passes. No latency regression against the phase 1a baseline.

### Phase 1b: Locked without a daemon (OSS, root install)

- `sudo gryph install --managed` for Claude Code and Codex (class A). Cursor with
  `failClosed: true`. Gemini and Windsurf at the system path (class B).
- Lock as a separate admin switch, off by default.
- Managed policy in `systemConfigDir()`.
- deb and rpm with a root-owned binary.
- Sign and notarize the plain macOS binary. The MDM contract of section 11.1:
  `gryph install --managed`, `gryph doctor --managed --json`, and
  `gryph uninstall --managed`. MDM scripts deploy the binary and run these commands.
  The pkg and MSI follow in phase 2.
- System-wide user units that run `gryph supervisor reconcile --once` as each user
  for class C agents (section 11.1).
- Ignore the path variables (`GRYPH_*_DIR`, `XDG_*`, `HOME`) under `--managed`
  (section 11.1). The config-key part is done (commit `426480f`).
- Coverage matrix with classes and verified versions.
- `gryph doctor` prints "Locked (hook entry and policy files, decision and audit
  trail not protected)" until phase 2 ships.

Exit: acceptance rows pin the managed key names. A same-user removal of a class A hook
fails. Claim in docs: "the hook entry and the policy files resist the non-admin user,
the decision and the audit trail do not". Customer metrics: MDM install success rate, and percent of detected agents
in class A on managed hosts.

### Phase 2: System supervisor (OSS)

- `decision/ipc` client and server. Server and client identity checks. Limits.
- Linux first: systemd socket activation with the `Welcome` handshake rule,
  partitions, DB import as the user, `storage.ReadStore` over the socket, key custody
  in the supervisor, confused-deputy rules, fail-mode matrix, spool, client-mode
  restructure of `NewHookCmd`, Mediator `degraded` flag.
- Approvals with assurance levels. Deferral queue in the supervisor. Local admin
  channel with `gryph policy approve list|show|watch|resolve`, inline wait, and grants
  (section 9).
- Beta as one root process behind a flag. GA after the broker and worker split.
- Then macOS (signed pkg, LaunchDaemon), then Windows (MSI, service).

Exit, for each OS separately: hook p95 and p99 within the budget set from the phase
1a baseline. Kill and upgrade tests pass with the expected fail mode. Receipts are out
of the user's write reach. The privileged two-user CI job gates merges. Linux GA does
not wait for macOS or Windows.

### Phase 3: SafeDep Cloud (commercial)

- Signed bundles for config and policy per section 10.2. Cloud `stream.Target` with
  receipts. Signed status report. Fleet dashboard and tamper alerts.
- Cloud approval queue next to the local admin channel (section 9). Exception queue.
- Collection levels `evidence`, `policy`, `full`, and the field match in export
  profiles (section 10.4).
- Policy control plane: replay, shadow evaluation in the Mediator, staged rollout
  (section 10.5).
- Reword the README line "No cloud, no telemetry" to "Gryph sends nothing by default.
  SafeDep Cloud is opt-in." before this phase ships. The same applies to
  `website/src/protect/milestones.ts`.

Exit: time to detect a tamper event is the heartbeat interval plus one minute, under
5 minutes. Percent of fleet that reports within 24 hours.

### Phase 4: Kernel-assisted (Linux, optional)

- fanotify permission provider for protected files and the binary.
- Opt-in Landlock launcher for agents.
- BPF LSM only behind a capability probe.
- macOS Endpoint Security only with a paying fleet customer, a dedicated owner, and
  the Apple entitlement (section 16).

Exit: a red-team suite measures the bypass rate of protected paths outside the hook
path. Overhead and stability are measured.

### OSS and commercial line

Local enforcement is free. Guard, Locked, the system supervisor, the provider
interface, the managed config, and a generic sink stay OSS. A self-hosting enterprise
can run Locked with its own MDM and SIEM. SafeDep Cloud sells the multi-host work:
fleet dashboard, central policy rollout, tamper alerts, out-of-band approvals,
exceptions, compliance reports, SSO, and any signed kernel component. The fanotify
provider and the Landlock launcher are OSS.

## 14. Non-goals

- Prevention against root or a kernel compromise.
- Mediation of agents that run without hooks, or in a container that cannot reach the
  socket, beyond the fail mode in section 8.
- A generic plugin system for providers. Providers are compiled in.
- A user-mode daemon as a security feature.
- A Windows kernel driver.

## 15. Open questions for the owner

The owner answered the first set of questions on 2026-10-03 (section 1.1). These
questions remain:

1. Publish that class B and C agents get only `Repair` under Locked?
2. Is repair on by default for P2, through a single `gryph install --repair` flag?
3. Does the cloud approval queue need its own SSO roles for each team or host group
   in phase 3, or is one tenant-wide `approver` role enough at first?
4. Can a team choose `full` collection for a subset of hosts or agents, or only for
   the whole tenant?

## 16. SafeDep action items

These items are business or account tasks for SafeDep. They are not engineering work
in this plan, but some phases depend on them.

| Item | Needed for | Lead time |
|---|---|---|
| Apply for the Apple Endpoint Security entitlement (`com.apple.developer.endpoint-security.client`) | Phase 4, macOS kernel-assisted provider | Unknown. Apple grants it on request. Apply early so that a customer request does not wait on Apple. |
| Apple Developer ID and notarization access for CI | Phase 1b (signed binary), phase 2 (pkg) | Weeks |
| Authenticode code signing certificate | Phase 2, Windows MSI and service | Weeks. SmartScreen reputation builds over time. |
| GPG key for deb and rpm signing. Packages ship as signed GitHub release assets first. A hosted apt or yum repository is ongoing operations work, so it waits for customer demand. | Phase 1b | Days |

## Appendix A: Debate record

| Topic | Positions in round 1 | Resolution |
|---|---|---|
| Fail mode when the supervisor is down | Security: fail closed in managed mode. Principal: `local` fallback. Architect: `supervisor.required` flag. Product: admin choice. | In system mode the user has no DB or key, so `local` cannot exist there. Default `block` for pre hooks. `local-ephemeral` as the `pilot` opt-in, with context rules that resolve to block. Fallback only on connect failure, because a same-user process can force deadlines. `local` stays the default for standalone. |
| Phase order | Product: user self-heal first. Security: system service first. Principal: managed settings before the supervisor. | Phase 1a foundations and reconcile, phase 1b managed settings without a daemon, phase 2 supervisor. Security accepted this on the condition that phase 1b ships managed policy and a root-owned binary together. |
| Abstraction scope | Architect: seven interfaces. Principal: one interface for each concern, at most two implementations. | One new interface (`selfprotect.Provider`) and one new implementation of an existing one (`decision.Service` over IPC). The rest uses existing seams. |
| Vocabulary | Security: capabilities. Product: T0 to T3. Architect: four mechanism tiers. | Asset levels are the source of truth. Guard, Locked, Managed are computed labels. Kernel-assisted is a flag. Attest is a flag. |
| User-mode daemon | Architect and product: phase 2 or T1. Principal: a timer is enough. | Timer that runs `reconcile --once`. No user daemon as a phase. |
| Kernel providers | Product: T3 on all three OSes. Principal: no Windows driver, Endpoint Security only when paid for. | Linux fanotify and Landlock first. BPF LSM behind a probe. Endpoint Security on request. Windows driver not planned. |
| Root broker and worker split | Architect: phase 3b. Principal: defer. Security: required. | Beta may run one root process behind a flag. GA of system mode on an OS needs the split. |
| Confused deputy | Architect raised it. | All agree. The client sends bounded bytes and cost totals. Repairs and the DB import run as the user. The uid-drop helper is used only by uninstall. |
| Cloud trust model | Security: design now. Principal: defer. | The trust model is written now (section 10.2). No code until the cloud API exists. Use `go-tuf` or Sigstore. |
| Heartbeat | Security and product: in phase 1. Architect: later. | Phase 1a defines the `AssetStatus` and tamper event schema and records events locally. Transport waits for phase 3. |
| `allowManagedHooksOnly` | Not raised in round 1. | Architect raised it in round 2: it also turns off the developer's own hooks. Separate admin switch, off by default. |
| Server authentication | Not raised in round 1. | Security raised it in round 2: the client checks that the socket peer is root or `_gryph`. Required. |

### Round 3 review

All four roles reviewed the draft. Each role raised blocking items, and the panel
fixed all of them before sign-off:

- Security and architect: a failed server identity check must never trigger the
  fallback. Only `ErrConnect` falls back.
- Architect: `runHook` returns exit 1 on a `Handle` error today, which most agents
  treat as allow. Client mode maps every error to the managed verdict.
- Architect: `decision.Local`, `HookRequest`, and `aarm/pdp` change for the
  confused-deputy and `local-ephemeral` rules.
- Security: the spool ingest is a confused deputy too. Codex always pins
  `[features].hooks = true`.
- Principal: an error after connect applies the phase rule, so prompt hooks do not
  freeze. Container rows, test-based exit criteria, and MDM recipes that match the
  packaging phase.
- Product: the Locked claim does not depend on the optional lock. The policy-block
  message is specified.

Final status after round 3: all four roles signed off.

### Round 4 review: owner decisions

The owner answered the open questions (section 1.1) and asked for an escalation
design with both the cloud queue and a local admin. The panel reviewed the new
sections. The blocking items and their fixes:

- Security: a `session` or `window` grant must not match the whole rule. The default
  scope is `once`.
- Security: an approver with the requester uid is refused, also in the admin group.
- Security: the MDM input files open with no-follow and need root ownership. Under
  `--managed`, Gryph runs no program through `PATH`.
- Security: the `policy` profile strips URL userinfo and query values and runs the
  redactor on command, path, and URL fields. Approval summaries use it at every
  level.
- Architect: `config.Load` bound `GRYPH_*` over the managed file. Fixed in commit
  `426480f`.
- Architect: the supervisor releases the session lock during the inline wait.
- Principal: the client bounds the inline wait by the agent hook timeout, because a
  longer wait fails open.
- Principal: class C hooks use system-wide user units that run reconcile as each
  user, from phase 1b.
- Product: the `evidence` level says that approval summaries leave the host. The
  developer gets a collection notice on the first session after enrollment.

Final status after round 4: all four roles signed off.

### Independent review

After round 4, an independent reviewer (a fresh model with no part in the debate)
checked the proposal against the code. It confirmed nearly every code claim. The
blocking items and their fixes:

- Socket activation accepts a connect while the supervisor is down, so a successful
  connect proved nothing. The fallback now keys on the `Welcome` frame within a
  handshake budget (section 8.1).
- Receipt chains are per session, so tamper events had no chain. They now go to a
  per-user system session with a deterministic ID (section 5.2).
- Blocking context rules when the accumulator is `Nop` would break `gryph policy
  test`, because `Nop` is the default. An explicit Mediator `degraded` flag gates it
  (section 7.6).

The should-fix items are also applied: a narrower phase 1b claim, the approver
confirmation on `/dev/tty` against an admin's own agent, the DB import as the user,
one rule for root writes into homes (uninstall only), the fail matrix keyed on
`HookSpec`, the client-mode restructure of `NewHookCmd`, a `storage.ReadStore` in
place of the full store over IPC, and cost totals in place of a transcript excerpt.
The key paths are now in section 7.2.

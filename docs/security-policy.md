# Security Policy

Gryph enforces a YAML policy over every agent action. A rule can **block**, **warn**, **guide**, or **allow**. Blocked actions never reach the agent's tool, guidance is delivered back to the agent as stderr text, and every decision is persisted to the event log.

Policies are evaluated by an AARM-shaped Policy Decision Point. This document provides guidance for
policy authoring and usage.

## Quick start

```bash
gryph policy init          # write the example policy to the global config dir
gryph policy edit          # open it in $EDITOR to author your rules
gryph policy validate
gryph policy test --action file_write --path ./secrets/db.env
```

Edit the global policy between steps using `gryph policy edit`. Every feature has inline documentation in the generated file.

Turn enforcement in configuration. See `gryph config show` for details. You can also run `gryph
policy set policy.enabled=true` to enable policy enforcement or edit the `config.yml` file directly.

```yaml
policy:
  enabled: true
```

Once enabled, every supported agent hook runs through the engine.

## Where policy files live

Gryph loads policy from exactly two sources, in this order:

1. **Global policy file** (`${ConfigDir}/policy.yaml`, optional). The single operator-owned file. On macOS this is `~/Library/Application Support/gryph/policy.yaml`; on Linux `~/.config/gryph/policy.yaml`. A missing file is not an error: with `policy.enabled: true` and no `policy.yaml` on disk, the merged policy contains built-in self-protection rules only.
2. **Built-in self-protection rules** (always appended, never filtered). These protect the config directory, the database, and agent hook configs from agent self-modification.

Write the global file with `gryph policy init` (non-interactive, honors `--force` to overwrite) or open it in your editor with `gryph policy edit` (scaffolds from the built-in example when the file is missing, then runs validation after save). Per-project policy overlays are a planned future iteration; today, one host-global policy governs every project and agent on the host.

Use `disabled:` in the global file to suppress a built-in rule by ID. Rule IDs must be unique within the file; user rules may not use the `gryph-builtin-` prefix.

```yaml
disabled:
  - block-rm-rf-root
```

## Authoring a rule

A rule is a YAML object with at minimum an `id` and an `action`. Everything else is optional.

```yaml
version: "1"
rules:
  - id: block-prod-writes
    action: block
    severity: high
    match:
      action_types: [file_write]
      file_patterns:
        - "**/prod/**"
    message: |
      Refusing write to {{.Action.Params.Path}}. Production changes go
      through the release pipeline.
```

Run `gryph policy test --action file_write --path /repo/prod/config.yaml` to see the rule fire.

### Match criteria

| Field | Type | Notes |
|---|---|---|
| `action_types` | list | `file_read`, `file_write`, `file_delete`, `command_exec`, `network_request`, `tool_use`, `session_start`, `session_end`, `notification`, `subagent_start`, `subagent_stop` |
| `file_patterns` | list | Doublestar globs (`**`) over the action path |
| `command_patterns` | list | Go regexps over the shell command |
| `tool_names` | list | Exact tool names like `Bash`, `Write`, `WebFetch` |
| `content_patterns` | list | Go regexps over the captured content preview |
| `working_directory_patterns` | list | Doublestar globs over the agent's cwd |

An empty `match` block matches every action. Combine with `scope` to narrow further.

### Scope

```yaml
scope:
  agents:   [claude-code, cursor]
  projects: [payments]
  tools:    [Bash]
```

`scope` is AND-combined with `match`. Omit a field to mean "any". Useful for shipping one policy file across teams and having rules opt in only where they apply.

### Conditions (CEL)

`condition` is a CEL expression that must return bool. It runs after `match` succeeds.

```yaml
- id: warn-large-edit
  action: warn
  match: { action_types: [file_write] }
  condition: >
    action.params.lines_added > 200 &&
    !action.params.path.contains("generated")
  message: |
    Large edit ({{.Action.Params.LinesAdded}} lines) to {{.Action.Params.Path}}.
    Consider splitting into smaller commits.
```

Available variables:

```
action.type / tool / operation / agent / working_dir / project
action.params.{path, command, args, url, size_bytes, lines_added, lines_removed, content}
action.data_classifications        list, set by the heuristic classifier
action.injection_score             float 0..1, set for tool_use actions only
action.human_principal             captured identity, see Identity capture
action.service_identity            CI / service identity, see Identity capture
action.role_scope                  OS uid/gid + asserted scopes
context.{total_actions, files_read, files_written, commands_executed,
         network_requests, errors, tools_used, session_duration_ms,
         classifications_seen, entities_seen, semantic_drift}
```

`action.data_classifications` carries labels like `secret`, `pii`, `source_code`, `config`, `git_internal`, `external_url`. `context.classifications_seen` is the running union across the session. `semantic_drift` is reserved and reads as `0.0` today.

`action.human_principal`, `action.service_identity`, and `action.role_scope` carry the AARM R6 identity fields. They are empty strings when capture is disabled or the resolver could not derive a value. See [Identity capture](#identity-capture).

CEL evaluation runs sandboxed with a 100 ms timeout.

### Messages

`message` is a Go `text/template` rendered when the rule matches. Available references:

```
{{.Action.Type}}            {{.Action.Tool}}        {{.Action.Agent}}
{{.Action.Params.Path}}     {{.Action.Params.Command}}
{{.Context.TotalActions}}   {{.Context.FilesWritten}}
{{.Rule.ID}}                {{.Rule.Severity}}
```

The rendered message is delivered to the agent on stderr for block and guidance decisions.

## Decisions

| Decision | What happens | Exit code |
|---|---|---|
| `allow` | Action proceeds | 0 |
| `warn` | Action proceeds, message recorded | 0 |
| `guidance` | Action proceeds, message delivered to agent | 0 |
| `block` | Action refused, message delivered to agent | 2 |
| `escalate` | Action pauses; operator approves or denies via CLI prompt. Approved -> allow. Denied or timed out -> block. See [Approval](#approval-workflow). | 0 or 2 |
| `defer` | Action is blocked at the hook, recorded as a deferral, and queued for operator resolution. Resolution writes a follow-up receipt. Timeout flips the queue row to deny and writes a deny follow-up receipt. Requires a non-empty `reason`. See [Deferrals](#deferrals). | 2 |

When multiple rules match, the most restrictive wins:

```
block > escalate > defer > guidance > warn > allow
```

## Commands

### Authoring and validation

| Command | Purpose |
|---|---|
| `gryph policy init` | Write the fully documented example policy to `${ConfigDir}/policy.yaml`. Use `--force` to overwrite an existing file. |
| `gryph policy edit` | Open the global policy in `$EDITOR`. Scaffolds from the built-in example when the file is missing. Runs validation after save. |
| `gryph policy schema` | Print the JSON Schema. Pipe into editor tooling or an AI agent. |
| `gryph policy validate` | Parse and compile the global policy. Reports rule count and the resolved file path. |
| `gryph policy test ...` | Dry-run a synthetic action through the merged policy. See `--help` for flags. |

`gryph policy test` accepts `--format json` for machine-readable output.

### Runtime inspection

| Command | Purpose |
|---|---|
| `gryph policy context` | List per-session counters (action counts, tools used, classifications seen). `--session <id|prefix>` drills into one session and shows recent actions. |
| `gryph policy receipts` | List receipt rows for mediated actions. `--session`, `--decision`, `--since`, `--until` filter. Pass `--show-hash` to include the per-row hash. |
| `gryph policy receipts --verify` | Recompute the hash chain and verify any signatures. `--session ID` verifies one chain in full; `--all-sessions` verifies every chain. Exits non-zero on break or invalid signature. |
| `gryph policy receipts export` | Stream receipts as JSONL or CSV. `--include-signatures` adds the Ed25519 signature columns. |
| `gryph policy receipts verify-log --input FILE` | Verify an exported chain stand-alone. No database access needed. Verifies signatures when `--trust-store` resolves to a populated store. |
| `gryph policy approve list` | List pending approval requests. CLI prompts run in-process, so this is always empty in the CLI frontend. |
| `gryph policy approve history` | Show receipts whose decision was `escalate`, `approved`, `denied`, or `approval_timeout`. |
| `gryph policy deferrals` | List the pending-deferral queue. `--status` filters to `pending`, `resolved_allow`, `resolved_deny`, `resolved_timeout`, or `all`. `--session ID` scopes to one session. |
| `gryph policy deferrals resolve --id ID --decision allow|deny [--note TEXT]` | Resolve a queued deferral by id (or id-prefix). Writes a follow-up receipt with `deferral_of_sequence` set, emits a `deferral_resolved` self-audit row. |
| `gryph policy deferrals sweep [--dry-run]` | Flip every expired pending deferral to `resolved_timeout`, write a deny follow-up receipt for each, emit `deferral_timeout` per row and a `deferral_sweep` summary. |

### Signing keys

| Command | Purpose |
|---|---|
| `gryph policy keys generate` | Create an Ed25519 keypair, write the private key to `<config dir>/keys/receipt.key` (0600), append the pubkey to the trust store. `--force` rotates the key and records a `receipt_key_rotated` self-audit row. |
| `gryph policy keys list` | List trusted public keys. |
| `gryph policy keys trust --pub FILE` | Add an external public key from a JSON file. Rejects entries whose `key_id` does not match `sha256(pub)[:8]`. |
| `gryph policy keys revoke --key-id ID` | Remove a key from the trust store. The private key file is left in place. |

## Receipts

Every mediated action produces a receipt row in the event store. The default `policy.log_all_evaluations: true` records receipts for `allow` decisions too, which keeps Gryph aligned with AARM's "receipt for every action" requirement. Operators who want the prior behavior (only `block` / `guidance` / `warn` / `escalate` rows) set `policy.log_all_evaluations: false` explicitly. Note that the new default raises per-event storage and signing cost on allow-heavy workloads.

Receipts form a per-session hash chain (`hash`, `prev_hash`). The hash now also covers the SHA-256 of the active policy document (`policy_hash`), so an after-the-fact rule edit is visible at verify time. The chain detects tampering and lets you verify the audit trail off-host.

Receipt rows carry the three identity fields (`human_principal`, `service_identity`, `role_scope`) captured at the mediation boundary. They surface in the `gryph policy receipts --format json` view and in the JSONL and CSV exports. Pre-Phase-6 rows have NULL identity columns and continue to verify cleanly: the hash recipe treats the empty string as the same length-prefixed zero bytes as the insert path.

Signing defaults to `sign_mode: auto`: receipts carry an Ed25519 signature when a key file is present at the configured `key_path`, and skip the signature when no key is on disk. Pick the explicit mode that matches your operational policy:

```yaml
policy:
  receipts:
    sign_mode: auto      # default: sign when a key exists, otherwise unsigned
    # sign_mode: always  # hard-fail at startup if the key is missing
    # sign_mode: never   # skip signing unconditionally
```

The legacy `sign: true` / `sign: false` bool is still accepted as a deprecated alias for `sign_mode: always` / `sign_mode: never`. Each receipt then carries an Ed25519 signature and `signer_key_id`. `gryph policy receipts --verify` walks the chain, recomputes every hash, and verifies signatures against the trust store. `gryph policy receipts export ... | gryph policy receipts verify-log --input -` round-trips the same checks without database access.

### Context chain

The Context Accumulator log (`aarm_context_actions`) carries a per-session hash chain of the same shape as the receipt chain: each row stores `sequence`, `prev_hash`, and `hash`. The chain attests to the as-mediated action (identity + counter-feeding fields), not the post-hook result, so a result_status update never invalidates the chain. Verify it with:

```
gryph policy context --verify --session <id|prefix>     # one session, full chain
gryph policy context --verify                           # sessions touched by the most recent --limit rows
gryph policy context --verify --all-sessions            # every session in the log
```

`--verify` exits non-zero on any chain break and records a `context_chain_broken` self-audit row. Rows written before the chain was added show up as `unchained` in the summary and do not fail verification. `--format json` emits a machine-readable verdict (`actions`, `chain_breaks`, `summary`).

The chain is not signed today. The receipt chain remains the authenticated audit log. The context chain is for the policy engine to read and for tamper-evidence within the same database.

## Deferrals

A `defer` decision pauses the agent's tool call by blocking it at the hook,
records a `defer` receipt with `result_status=deferred`, and queues a
pending-deferral row that an operator resolves out-of-band (or the timeout
sweep flips to deny). Gryph mediates synchronously on hooks and cannot resume
a previously-blocked tool call, so the agent has to re-issue the action after
resolution if it still wants to perform it.

A defer rule looks like:

```yaml
- id: defer-on-missing-classification
  action: defer
  reason: wait_for_classification
  match:
    action_types: [file_write]
  condition: "size(action.data_classifications) == 0"
```

The `reason` field is required on defer rules and surfaces on the receipt's
`defer_reason` column and in the operator-facing block message returned to
the agent.

### Auto-defer triggers

Two trigger types produce a synthetic defer decision even without an explicit
`action: defer` rule, when the PDP detects insufficient or conflicting input:

- `fresh_session_insufficient_context` fires when a rule's CEL condition
  references context fields that are still zero or empty AND the session is
  younger than `policy.defer.fresh_session_seconds` (default 60). The action
  defers rather than evaluating against an unfilled snapshot.
- `conflicting_policies` fires when multiple rules match at the winning
  severity tier with materially different rendered messages. Each decision
  lives at its own tier under the precedence scheme, so the practical case
  this catches is two or more rules of the same decision (e.g. two `block`
  rules) firing with different justifications. The PDP normally surfaces the
  first match's message; this trigger surfaces the ambiguity instead. Gated
  by `policy.defer.conflict_triggers_defer` (default true).

Both triggers are gated by `policy.defer.enabled` (default true).

### Resolution and timeout

Operators inspect the queue with `gryph policy deferrals` and resolve with
`gryph policy deferrals resolve --id PREFIX --decision allow|deny`. Each
resolution:

1. Updates the deferral row to `resolved_allow` / `resolved_deny`.
2. Inserts a follow-up receipt in the same session whose
   `deferral_of_sequence` points at the original defer receipt's sequence.
3. Emits a `deferral_resolved` self-audit row.

Timeouts run via `gryph policy deferrals sweep` (also folded into
`gryph retention cleanup`). Any pending deferral whose `expires_at` is past
flips to `resolved_timeout` and gets a deny follow-up receipt. AARM R4
forbids implicit allow on timeout, so the only valid value for
`policy.defer.auto_resolve_on_timeout` is `deny`.

```yaml
policy:
  defer:
    enabled: true
    fresh_session_seconds: 60
    conflict_triggers_defer: true
    timeout_seconds: 600
    auto_resolve_on_timeout: deny
```

The receipt-hash chain stays valid across the deferral lifecycle: the
follow-up receipt is appended at the next sequence with `prev_hash` pointing
at the original defer receipt's hash. `gryph policy receipts --verify`
exercises the entire chain in one pass.

## Approval workflow

A rule with `action: escalate` pauses the agent's tool call and prompts the operator on `/dev/tty` for approve or deny.

```yaml
policy:
  approval:
    mode: cli            # or nop to deny all escalations
    timeout_seconds: 60
    require_note: false
```

The receipt row records the final outcome (`approved`, `denied`, or `approval_timeout`) and the approver identity. Review past decisions with `gryph policy approve history`. If no controlling terminal is available, the request denies; the safe default applies for unattended runs.

## Risk signals

Two heuristic signals populate the action record. Disable either if a custom analyzer fits better.

```yaml
policy:
  classify:
    enabled: true
    fail_open: false
    extra_patterns:
      pii: ["**/customer-list*"]
  injection_score:
    enabled: true
```

`classify` labels paths and URLs. `injection_score` scans tool-use content for prompt-injection markers and returns a float between 0 and 1. Use them in conditions:

Defer fires automatically on insufficient context (fresh sessions whose
counters have not filled in yet) and on conflicting policies (multiple rules
at the same severity returning different decisions), unless disabled via
`policy.defer.enabled: false`. See [Deferrals](#deferrals).


```yaml
- id: block-secret-network-write
  action: block
  match: { action_types: [network_request] }
  condition: "'secret' in context.classifications_seen"

- id: warn-suspicious-tool-input
  action: warn
  match: { action_types: [tool_use] }
  condition: "action.injection_score > 0.5"
```

### Identity capture

Gryph captures three identity-level fields at the mediation boundary and writes them onto every action and receipt:

- `human_principal`: operator-asserted via `GRYPH_HUMAN_PRINCIPAL` (SSO claim, email, etc.). Falls back to `uid:<N>:<username>` derived from the OS process credentials. On Unix the format is `uid:<N>:<username>`. On Windows (no uid available) it is `user:<username>`.
- `service_identity`: operator-asserted via `GRYPH_SERVICE_IDENTITY`. Otherwise auto-detected for GitHub Actions, Buildkite, GitLab, and CircleCI, with a generic `ci:unknown` fallback when `CI=true` is set.
- `role_scope`: operator-asserted via `GRYPH_ROLE_SCOPE`. Otherwise derived from OS uid/euid/gid plus up to eight supplementary groups.

Capture runs once at process start and is cached. Disable the layer with `policy.identity.enabled: false` to leave the three fields empty.

```yaml
policy:
  identity:
    enabled: true                  # off disables capture entirely
    require_human_principal: false # if true, missing principal denies the action
```

When `require_human_principal: true`, the Mediator blocks any action whose `human_principal` is empty before consulting the PDP. The block message is `Action denied: no verifiable human principal`, a receipt with `decision=block` is still written, and an `identity_missing` self-audit row is emitted. The switch is a silent no-op when `enabled: false` (we cannot enforce what we did not capture).

The OS-derived human principal is a weak proxy. On a developer workstation `uid:501:alice` is the most we know. Real SSO identity requires the operator to set `GRYPH_HUMAN_PRINCIPAL` from the SSO session. Receipts record what was captured. Downstream tooling decides what to trust.

Policies can gate on identity directly:

```yaml
- id: block-prod-without-sso
  action: block
  match: { action_types: [file_write], file_patterns: ["**/prod/**"] }
  condition: "!action.human_principal.startsWith('sso:')"
  message: "Prod writes require SSO identity, got {{.Action.HumanPrincipal}}"
```

### Safe-by-default classification

AARM R2 requires the engine to default to the highest sensitivity level when no classification mechanism produces a result. Gryph honours this by appending the `unknown_sensitive` label to any action the classifier left unlabeled. The label fires in three cases: the classifier is disabled (`classify.enabled: false`), the classifier ran and matched nothing, or the action has no classifiable surface (no path, URL, or content). Policies that gate on classification now fail safe instead of waving the action through.

Rules that match on explicit labels (`'secret' in context.classifications_seen`) are unaffected. Rules can also opt into a paranoid mode by gating on `'unknown_sensitive' in action.data_classifications`.

Operators who explicitly want classification off and do not want the fail-safe label flip `classify.fail_open: true`:

```yaml
policy:
  classify:
    enabled: false
    fail_open: true   # opt out of the AARM safety-net label
```

The default (`fail_open: false`) keeps AARM conformance.

## Worked examples

### Block destructive shell

```yaml
- id: block-rm-rf-root
  action: block
  severity: critical
  match:
    action_types: [command_exec]
    command_patterns:
      - '(?i)\brm\s+-[rf]+\s+(/|~|\$HOME)(\s|$)'
  message: Refusing destructive command {{.Action.Params.Command}}
```

### Refuse writes that leak credentials

```yaml
- id: block-aws-key-in-write
  action: block
  severity: critical
  match:
    action_types: [file_write]
    content_patterns:
      - 'AKIA[0-9A-Z]{16}'
  message: |
    Content for {{.Action.Params.Path}} contains what looks like an AWS
    access key. If this is a fixture, redact it first.
```

### Cap session volume

```yaml
- id: warn-session-write-volume
  action: warn
  severity: low
  match:
    action_types: [file_write]
  condition: "context.files_written >= 25"
  message: |
    This session has written {{.Context.FilesWritten}} files. Consider
    stopping to review the diff before continuing.
```

### Allow docs edits explicitly

```yaml
- id: allow-docs-edits
  action: allow
  tags: [docs]
  match:
    action_types: [file_write]
    file_patterns:
      - "**/*.md"
      - "**/docs/**"
```

## Troubleshooting

- **Rule never matches.** Confirm the global policy file is present and valid with `gryph policy validate`, which also prints the resolved file path. Then `gryph policy test --action <type> --path <path>` and inspect the matched-rule output.
- **Policy fails to load.** `gryph policy validate` reports the first compile error with the rule ID and line.
- **Policy is enabled but nothing is blocked.** Confirm `policy.enabled: true` and that the agent in question is registered. Check `gryph query --status blocked` to see what was caught.
- **I want to override a rule for one project.** Per-project policy overlays are a planned future iteration. Today, edit the global policy (`gryph policy edit`) and use `disabled: [rule-id]` or tighten the rule's `scope` field to exclude the project you want to exempt.

<details>
<summary><strong>Threat model: receipt keys and trust</strong></summary>

Read this before relying on receipt signatures as evidence outside your own host.

### What signing protects

- **Tamper detection on exported receipts.** A third party with the pubkey can verify a JSONL export came from your host and was not modified in transit.
- **Per-host attribution.** Aggregating receipts from many hosts at one SIEM, each host's signature ties its rows back to that host's key.
- **In-DB tamper by something that does not have key access.** Rare on a single-user host since the key sits in `~/.config/gryph/keys/` at the operator's UID.

### What signing does not protect

- **The operator deciding to lie.** They hold the key. They can regenerate it and re-sign a forged chain. Self-attestation is unsolvable without an external anchor.
- **Same-UID malware.** Anything running as the operator can read the 0600 key file. We enforce owner check and `O_NOFOLLOW` on read, which blocks symlink swaps and cross-user reads. It does not block code the operator already trusts.
- **Tail truncation.** Drop the last N rows and the prefix still verifies. No external head commitment.
- **Backdating.** `recorded_at` is set by the signer.
- **A compromised agent shaping events before mediation.** Gryph signs what the Mediator saw. The hook runs in-process with the agent.

### Trust roots

1. The private key file at `<config dir>/keys/receipt.key`. Mode `0600`, owner-checked.
2. The trust store at `<config dir>/keys/receipt-pub.json`. World-readable so SOC tools can inspect it. Writable only at filesystem perms; `keys trust` rejects entries whose `key_id` does not derive from their pubkey, but nothing stops you from adding your own freshly generated key.
3. The mediation path itself. The chain attests to what Gryph computed, not to what the agent did downstream.

### When to enable signing

- You export receipts off-host for audit by a separate team. Signing earns its keep.
- You ship receipts into a SIEM that pre-shares a per-host pubkey. Signing earns its keep.
- You run a single-user workstation and only ever read receipts locally. Signing is decoration; the hash chain alone covers tamper-evidence within the DB.

### Hardening beyond this design

- Anchor each session's head (`session_id, last_sequence, last_hash`) to an external append-only log (Rekor, Git, S3 Object Lock) so tail truncation becomes detectable.
- Move the key behind macOS Keychain, Linux Secret Service, or a YubiKey so same-UID malware cannot extract the seed.
- Pin pubkeys at the verifier from an out-of-band channel rather than trusting whatever lands in `receipt-pub.json` on the producing host.

</details>

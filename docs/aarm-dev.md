# AARM / Policy Layer Developer Guide

This guide covers the `aarm/` packages and their CLI wiring in `cli/policy.go`.
It targets contributors and AI agents that change policy evaluation, receipts,
context, approvals, or deferrals.

AARM is Gryph's agent-action security layer. It normalizes an agent hook event
into a canonical action, evaluates it against policy, returns an allow / block /
guidance decision, and records a tamper-evident receipt. The requirement set
(R1..R9) lives in `docs/security-spec.md`. The conformance suite in
`test/conformance/aarm` maps tests back to those requirements.

## Entry point

The layer implements the `core/security.Check` interface. `cli/root.go`
registers `lazyPolicyCheck` (in `cli/policy.go`) with the security `Evaluator`.
Hook processing calls `app.Security.Evaluate` in `cli/hook.go`, which calls the
check.

- `lazyPolicyCheck` defers policy load until the first hook event. A broken
  policy file must not lock the user out of `gryph policy validate` and `test`.
- `lazyPolicyCheck.load` builds the `aarm.Mediator` through
  `loadPolicyMediator`. That function is the single wiring site: it reads
  `config.PolicyConfig`, opens sources, and installs every optional component
  through `MediatorOption` values.
- The `Mediator` is the AARM implementation of `security.Check`.

## Request flow

```
hook event
  -> mediation.Adapter.Normalize   (event -> model.Action, enrich)
  -> Mediator.enforceIdentity      (pre-PDP block if no human principal)
  -> accumulator.Append + Snapshot (per-session context memory)
  -> pdp.PDP.Evaluate              (match rules -> EvaluationResult)
  -> Mediator branch on decision:
       escalate -> approval.Service -> outcome
       defer    -> deferral queue + block message
       other    -> pep.Apply (model decision -> core security decision)
  -> receipt.Generator.Record      (hash-chained receipt row)
  -> core/security.CheckResult
```

Post-hook, `cli/hook.go` calls `Mediator.RecordResult` on the allow path to
write the execution outcome to the accumulator row and the receipt row.

## Package map

| Package | Role |
| --- | --- |
| `aarm` | `Mediator`: the `security.Check`. Orchestrates every step. Re-exports model types. |
| `aarm/model` | Shared data model: `Action`, `Parameters`, `Decision`, `EvaluationResult`, `ContextSnapshot`, `Result`, `Severity`. No dependencies on other aarm packages. |
| `aarm/mediation` | `Adapter` interface plus `HookAdapter` and `MCPAdapter`. Normalizes agent events into `model.Action` and enriches with classify / injectscore / identity. |
| `aarm/pdp` | Policy Decision Point. `Policy` / `Rule` schema, YAML parse, rule compile, `Evaluate`, CEL conditions, message templates, policy hash. |
| `aarm/pep` | Policy Enforcement boundary. Maps `model.EvaluationResult` to `core/security.CheckResult`. |
| `aarm/loader` | `Loader` merges policy `Source` values. `FileSource`, `DirSource` (the policies directory), and `BuiltinSource` (self-protection rules). |
| `aarm/accumulator` | Context Accumulator interface. Per-session action memory feeding `context.*` CEL variables. `Nop` and SQLite implementations. |
| `aarm/accumulator/contextchain` | Per-session hash chain over context-action rows. |
| `aarm/receipt` | Append-only, hash-chained receipt log. Hashing, Ed25519 signing, chain verify, JSONL export, log verify. |
| `aarm/approval` | Approval Service for `escalate`. `Nop` (deny) and `CLIPrompt`. |
| `aarm/identity` | Captures human principal, service identity, role scope at the mediation boundary. |
| `aarm/classify` | Heuristic data classifier (secret, pii, source_code, ...). Fail-safe wrapper defaults to `unknown_sensitive`. |
| `aarm/injectscore` | Heuristic prompt-injection score for tool-use actions. |
| `aarm/canonical` | Deterministic JSON with recursively sorted keys. Shared by every hash. |
| `aarm/testchain` | Property-test scaffolding shared by receipt and context chain tests. Not production code. |
| `aarm/conformance` | Test-only helpers that attribute conformance tests to AARM requirements. |

`aarm/check` and `aarm/contextchain` are empty placeholder directories.

## Data model

`model.Action` is the canonical action. Adapters build it; the PDP, receipt,
and accumulator read it. Key fields:

- `Type` (`ActionType`): `file_read`, `file_write`, `file_delete`,
  `command_exec`, `network_request`, `tool_use`, session and subagent types.
- `Parameters`: normalized `Path`, `Command`, `Args`, `URL`, `Content`.
  `ContentFull` holds content for `content_patterns` matching only. It is
  capped at 1 MiB. For content over the cap, the PDP matches the first 1 MiB
  and sets `ContentTruncated`. A policy that needs full inspection must handle
  `content_truncated`. `ContentFull` is never persisted and is cleared after
  evaluation.
- Identity: `HumanPrincipal`, `ServiceIdentity`, `RoleScope`.
- Risk signals: `DataClassifications`, `InjectionScore`.
- `Phase` (`pre` / `post` / `unknown`): pre-execution hooks are enforceable;
  post hooks are detection only.

`model.EvaluationResult` is the PDP output: `Decision`, `MatchedRuleIDs`,
`Message`, `Severity`, `Tags`, `DeferReason`.

## Policy schema and evaluation

A policy is a YAML document (`pdp.Policy`) with a `version`, a list of `rules`,
and an optional `disabled` ID list. Print the JSON Schema with
`gryph policy schema`. Print the commented example with `gryph policy init`.

A `pdp.Rule` has:

- `id` (unique, required), `description`, `action` (the decision), `severity`,
  `enabled`, `tags`, `message`.
- `match`: `action_types`, `file_patterns` (doublestar globs),
  `command_patterns` (regexp), `tool_names`, `content_patterns` (regexp),
  `working_directory_patterns` (globs).
- `scope`: `agents`, `projects`, `tools`. Narrows which actions the rule sees.
- `condition`: a CEL expression that must return `bool`.
- `reason`: required when `action: defer`.

Evaluation order per rule: enabled check, scope, match criteria, then CEL
condition. All present match fields must pass (AND). Within a field list, any
pattern may match (OR).

### Decisions and precedence

`Evaluate` collects every matching rule, then the highest-precedence decision
wins:

```
block (5) > escalate (4) > defer (3) > guidance (2) > warn (1) > allow (0)
```

The winning rule supplies the severity, tags, and rendered message. `pep.Apply`
maps the AARM decision onto the three core decisions:

- `block` -> core `block`.
- `guidance`, `warn` -> core `guidance`.
- `escalate` -> routed to the Approval Service before the PEP. If it reaches
  the PEP unhandled, it degrades to guidance with a warning log.
- `defer` -> routed to the deferral queue; the agent sees a `block`.

### CEL variables

Conditions read two maps. `action.*` fields come from `actionActivation`:
`type`, `tool`, `operation`, `agent`, `working_dir`, `project`,
`injection_score`, `data_classifications`, `phase`, `content_truncated`,
`human_principal`, `service_identity`, `role_scope`, and `params.*`
(`path`, `command`, `args`, `url`, `size_bytes`, `lines_added`,
`lines_removed`, `content`).

`context.*` fields come from `contextActivation`: `total_actions`,
`files_read`, `files_written`, `commands_executed`, `network_requests`,
`errors`, `tools_used`, `session_duration_ms`, `classifications_seen`,
`entities_seen`, `semantic_drift`.

Conditions run under a 100 ms timeout and a CEL cost limit. `message` is a Go
`text/template` with `missingkey=error`. The template data is `.Action`,
`.Context`, and `.Rule`.

## Loader and self-protection

`buildPolicyLoader` in `cli/policy.go` resolves three sources in order: the
global file `${ConfigDir}/policy.yaml` (`FileSource`), the directory
`${ConfigDir}/policies/*.yaml` (`DirSource`, one document per file, sorted by
name), and the built-in source. `policyLoaderSources` is the single definition
of this order. Both user sources sit inside `${ConfigDir}`, so both are covered
by the `${ConfigDir}/**` self-protection glob. No other location is resolved.

`Loader.Load` merges sources in order. Rules:

- Duplicate rule IDs across sources are a load error.
- A document's `disabled` list removes only rules defined in the same document.
  The scope is uniform for every source. A file cannot disable a rule from
  another file. A `disabled` entry that names no rule in the same file logs a
  warning and has no effect. A disabled rule does not reserve its ID, so another
  file may define it.
- `BuiltinSource` rules (self-protection) load last, use the reserved
  `gryph-builtin-` ID prefix, and are never removed by a `disabled` list. A user
  rule may not use the reserved prefix.

A block always beats an allow. `disabled` acts only on one file. So the policies
directory is additive. A file merged there can add rules. It cannot remove the
built-in rules or another file's rules. `gryph policy install`
relies on this. See
[security-policy-threat-model.md](./security-policy-threat-model.md).

Self-protection blocks agent writes to Gryph's own control surfaces (policy,
config, database, signing keys, agent hook configs). The operator toggles it
only through `policy.self_protection.enabled`. Inspect the rules with
`gryph policy builtin`. `selfProtectionGlobs` in `cli/policy.go` builds the
globs.

`gryph policy list` enumerates the sources with rule counts. `gryph policy
install` promotes a reviewed candidate file into the policies directory. It
validates the candidate alone, then validates the merged result, excluding the
file it replaces. `gryph policy validate --file` and `edit <path>` validate one
off-tree file in isolation. `gryph policy test --file` dry-runs one off-tree file
plus the built-in rules, so an author can check a draft before install.

## Receipts

Every non-skip decision produces a receipt row. `policy.log_all_evaluations`
also records `allow` rows. The receipt log is append-only and hash-chained per
session. The hash canonicalization and field order are documented at the top of
`aarm/receipt/hash.go`. Change that order only with a matching change to the
verifier, or every existing chain fails verification.

- The generator can sign each row with Ed25519. `WithSigner` enables it. Keys
  live under the Gryph config directory. Manage them with `gryph policy keys`.
- `UpdateDecision` rewrites the decision and result status for an approval
  outcome but does not recompute the hash. The hash input collapses the outcome
  back to `escalate` via `DeriveInsertDecision` so the chain stays verifiable.
- The row stores `error_message`, but the hash excludes it.
- Export with `gryph policy receipts export`. Verify a chain with
  `gryph policy receipts verify-log`.

## Context accumulator

The accumulator records each action and returns the point-in-time
`ContextSnapshot` the PDP reads through `context.*`. The `Nop` implementation
returns an empty snapshot. The SQLite implementation persists to
`aarm_context_*` tables and hash-chains rows via `contextchain`. `Append` runs
before evaluation. `RecordResult` runs post-hook and updates the result-derived
counters.

## Special decision paths

- Identity enforcement: when `policy.identity.require_human_principal` is true
  and `Action.HumanPrincipal` is empty, `Mediator.enforceIdentity` blocks before
  the PDP and before the accumulator append. A denied action does not count
  toward `context.total_actions`.
- Escalate: `handleEscalate` calls the Approval Service. A nil outcome fails
  closed (treated as deny). The four `approval_*` audit actions fire through
  the `ApprovalAuditHook`.
- Defer: `handleDefer` writes a defer receipt, then the `DeferralHook` persists
  the pending row and returns an operator hint spliced into the block message.
  Auto-defer triggers (fresh session, conflicting policies) live in the PDP.
  `DeferConfig` gates them. Resolve with `gryph policy deferrals`.

## Extension points

- Add a Mediator dependency: define a `MediatorOption` in `aarm/check.go` and
  wire it in `loadPolicyMediator`.
- Add an agent adapter: implement `mediation.Adapter`. See
  `docs/agent-adapter.md`. Reuse `Common` for classify / injectscore / identity
  enrichment and `populateWellKnownParams` for argument promotion.
- Cross-cutting audit or storage: the Mediator stays decoupled from `storage`
  and `cli`. Hooks (`DeferralHook`, `ApprovalAuditHook`, `IdentityAuditHook`)
  carry the CLI-shaped side effects out of `aarm`. Keep it that way.

## CLI surface

`NewPolicyCmd` in `cli/policy.go` assembles the `gryph policy` tree: `init`,
`edit`, `list` (alias `ls`), `install`, `schema`, `builtin`, `validate`,
`test`, `context`, `receipts` (`export`, `verify-log`), `approve`, `keys`,
`deferrals`. `init` and `edit` take an optional name-or-path argument
(`cli/policy.go`). `list` and `install` live in `cli/policy_authoring.go`. Use
`gryph policy test` to dry-run a synthetic action through the merged policy
without an agent event.

## Testing and invariants

- Run `make test` and `make lint` before submitting. Run `make generate-schema`
  after any change to the policy schema or payload types.
- Receipt and context hashes are consensus formats. A field-order or
  canonicalization change breaks every existing chain. Update the verifier and
  the property tests together.
- `Policy.Hash` caches on first call. Never mutate `Rules` or `Disabled` after
  the policy is loaded.
- Fail-safe defaults: an unknown classification maps to `unknown_sensitive`; a
  nil approval outcome denies; identity capture failure does not silently allow.
- Conformance tests attribute to requirements through `aarm/conformance`. Add a
  test under `test/conformance/aarm` when you satisfy or change a requirement.

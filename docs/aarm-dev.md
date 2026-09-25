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
The hook side in `cli/hook.go` parses the agent payload and calls
`decision.Service.Handle`. The in-process `decision.Local` redacts, applies
the logging level, upserts the session, and calls `app.Security.Evaluate`,
which calls the check. The session is an explicit argument to `Evaluate` and
`Check`, not a context value. `cli/hook.go` then renders the response.

- `lazyPolicyCheck` defers policy load until the first hook event. A broken
  policy file must not lock the user out of `gryph policy validate` and `test`.
- `lazyPolicyCheck.load` builds the `aarm.Mediator` through
  `loadPolicyMediator`. That function is the single wiring site: it reads
  `config.PolicyConfig`, opens sources, and installs every optional component
  through `MediatorOption` values.
- The `Mediator` is the AARM implementation of `security.Check`.
- `decision.Local` takes the agent name from `Event.AgentName` only. The
  request has no second agent field, so a caller cannot select a logging level
  for an agent other than the event agent.
- Known gap: `decision.Local` applies the logging level before it calls
  `Evaluate`. At `logging.level: minimal` the level strips the tool input and
  the write content, so policy rules do not see them. For a sensitive event
  the level strips the payload content and `FullContent` at every level,
  `full` included. PR #68 moves the strip after the evaluation.

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

Post-hook, `decision.Local` calls `Mediator.RecordResult` on the allow path to
write the execution outcome to the accumulator row and the receipt row.

## Package map

| Package | Role |
| --- | --- |
| `aarm` | `Mediator`: the `security.Check`. Orchestrates every step. Re-exports model types. |
| `aarm/model` | Shared data model: `Action`, `Parameters`, `Decision`, `EvaluationResult`, `ContextSnapshot`, `Result`, `Severity`. Its only aarm dependency is `aarm/shellcmd`, for `Action.Shell`. `aarm/shellcmd` imports no Gryph package, and a test enforces it. |
| `aarm/mediation` | `Adapter` interface plus `HookAdapter` and `MCPAdapter`. Normalizes agent events into `model.Action` and enriches with classify / injectscore / identity. |
| `aarm/pdp` | Policy Decision Point. `Policy` / `Rule` schema, YAML parse, rule compile, `Evaluate`, CEL conditions, message templates, policy hash. |
| `aarm/shellcmd` | Parses a shell command with `mvdan.cc/sh`. `Analyze` returns the paths the command reads, writes, or removes, and the hosts it contacts. The mediator stores the result on `model.Action.Shell`. The PDP matches `file_patterns` against the write and remove targets for `command_exec` actions. |
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
- `Phase` (`pre` / `post` / `unknown`): pre-execution hooks are enforceable.
  Post hooks are detection only. The decision service sets the phase on the
  event from the adapter's `Hooks()` table, and mediation copies it.

`model.EvaluationResult` is the PDP output: `Decision`, `MatchedRuleIDs`,
`Message`, `Severity`, `Tags`, `DeferReason`.

## Policy schema and evaluation

A policy is a YAML document (`pdp.Policy`) with a `version`, a list of `rules`,
and an optional `disabled` ID list. Print the JSON Schema with
`gryph policy schema`. Print the commented example with `gryph policy init`.

A `pdp.Rule` has:

- `id` (unique, required), `description`, `action` (the decision), `severity`,
  `enabled`, `tags`, `message`.
- `match`: `action_types`, `file_patterns` (doublestar globs; for
  `command_exec`, also the paths from `aarm/shellcmd`),
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

Self-protection blocks agent changes to Gryph's own control surfaces (policy,
config, database, signing keys, agent hook configs). It is one rule,
`gryph-builtin-protected-files`, over `file_write`, `file_delete`, and
`command_exec`. For a command, the PDP matches the paths that `aarm/shellcmd`
parses from the command line. A delete or move of a directory also matches when
the directory contains a protected path, for a shell command or a
`file_delete`. The rule has no agent names and no command regexes.

`aarm/shellcmd` walks the parsed command tree. It tracks the set of working
directories a command can run in: a `cd` in a subshell, a pipe, a
substitution, or a background job does not carry over, and a `cd` that may not
run (after `&&` or `||`, or in an `if` or loop body) adds a directory to the
set. For a wrapper such as `sudo`, it tries each word after the wrapper as the
start of the command, because it does not know every wrapper option that takes
a value. The walk over-approximates. It prefers a false block to a missed
change. When the parser rejects a command, or a script nested in it such as
the script of `bash -c`, `Analysis.Parsed` is false, every word of the
rejected script is a read and a removal target, and the host list has `?`.

The mediator parses a command once, in `mediation.HookAdapter`, and stores the
result on `model.Action.Shell`. The PDP and later context work read that
result. The PDP parses the command itself only when an action has no
analysis, for example in `gryph policy test`. The receipt does not store the
analysis.

A read target comes from an input redirect, the source of a copy or a move,
the file operands of a fixed list of read commands (`cat`, `head`, `grep`,
`sed` without `-i`, `sort`, `tar`, `sqlite3`, and others), and the files that
`curl` and `wget` upload. A host comes from a URL anywhere in a word, from
the operands of `curl`, `wget`, `ssh`, `sftp`, `nc`, and similar tools, from an
scp-style `host:path` in `scp`, `rsync`, and the remote of a `git` command,
from `openssl -connect`, and from a `/dev/tcp/host/port` redirect. Hosts are
lower case, without the port.

A write or remove target also comes from the file operands of an editor
(`vim`, `vi`, `nvim`, `ex`, `nano`), `sort -o`, the archive of `zip` (also
`zip -O` and the `zip -lf` log) and of a `tar` create, append, update,
concatenate, or delete, the archive of `7z a`, `u`, `d`, and `rn`, and the
output files of `curl` and `wget`. `curl` also writes the files of `--hsts`,
`--etag-save`, `--libcurl`, `--alt-svc`, and `-w '%output{FILE}'`. The value
of a `curl` option that the table does not know is a guessed write target.

A command that can write any path in a directory, at any depth, is a tree
write (`AccessWriteTree`) of that directory: a recursive copy (`cp -r`,
`rsync -a`, `scp -r`) of a source that can be a directory, a copy of
directory contents (a source that ends in `/` or `/.`), a copy or link with
`-T` or `ln -n`, a `tar`, `7z x`, or `unzip` extract (the target directory or
the working directory), and a recursive `wget`. An extract that keeps
absolute member names (`tar -P`, `7z -spf`, `unzip -:`), `xz --files`, `7z`
with an unknown command, and `curl -w @FILE` are a tree write of `/`. A
recursive copy of a source with an extension, such as `notes.txt`, is a
write of the destination and of the source name in it. A download that takes
a name from the server (`curl -J`, `wget --content-disposition`) and `7z e`,
`unzip -j`, and `gunzip -N` remove the target directory. A plain download
writes the URL file name in the `--output-dir` or `-P` directory. `cp
--parents` and `rsync -R` write the whole source path under the destination.
An `ln` with one operand writes the base name in the working directory.
`gzip`, `bzip2`, and `xz` remove each operand and write the compressed or
decompressed file, unless `-c` or `-k` is set. `zip -m`, `7z -sdel`, `tar
--remove-files`, and `rsync --remove-source-files` remove the sources.

The PDP matches a removal or a tree write against the parent directories of
each pattern, and the root for an absolute pattern. A tree write into the
action working directory, the home directory, or a parent of either (also
`/`) matches every pattern that starts with `**/`. An agent loads its
settings from the project root and from home, so a tree write elsewhere
cannot plant a file that the agent loads. The cost is that an extract or a
recursive copy into the project root, such as `tar xzf release.tgz`, blocks
under the built-in rule. An extract into a subdirectory, such as `tar xzf
release.tgz -C build`, does not.

`parseArgs` in `aarm/shellcmd/tools.go` splits options the way getopt does,
with one `options` table for each tool. For a tool that parses with
getopt_long, such as `tar`, `curl`, `wget`, `sort`, `gzip`, and `cp`, a long
option also matches by a unique prefix, so `--cr` is `--create`. A prefix
match is only correct when the table lists every real option that is a
prefix of a listed value option, such as `curl --head` for `--header`. The
lists are best effort. A plain `mv` or `ln` onto a directory that does not
exist yet, and a `wget` or `curl` config file, are not seen. `tar` gives each
letter of an old-style first word that takes a value the next word, in
order, so `tar xfC a.tar dir` reads `a.tar` into `dir`.
Self-protection matches write and remove targets only.

The operator toggles self-protection only through
`policy.self_protection.enabled`. Inspect it with `gryph policy builtin`.

`selfProtectionGlobs` in `cli/policy.go` builds the globs. The Gryph paths come
from the config. The hook config paths come from each adapter's
`HookConfigPaths()`, collected by `Registry.HookConfigGlobs()` over the adapters
that `registerAdapters` in `cli/root.go` registers. To protect a new agent,
implement `HookConfigPaths()` in its adapter. Do not edit the loader.

Self-protection is best effort. The shell parse cannot resolve unknown
variables, command substitutions, encoded payloads, script files, or
interpreters, and a process outside the hook path is never seen. Kernel-based
self-protection is on the roadmap. See
[security-policy-threat-model.md](./security-policy-threat-model.md).

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
`ContextSnapshot` the PDP reads through `context.*`. The Mediator sets
`SessionStartedAt` on its own copy from the session argument. CEL cannot read
it. The `Nop` implementation
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
  The fresh-session trigger reads `ContextSnapshot.SessionStartedAt`, which the
  Mediator sets from the session argument.
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

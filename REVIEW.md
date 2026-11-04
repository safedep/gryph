# Review instructions

Gryph is a security tool. It hooks AI coding agents, decides on each agent action, and records a
signed receipt chain. A review finds the changes that weaken those controls. The reviewer also reads
`AGENTS.md`. This file names what matters most and the severity of each finding.

## What Important means here

A finding is Important when the change does one of these things:

- It lets an agent action pass that the policy or the fail mode must block.
- It weakens self-protection, the receipt chain, the signing keys, or the supervisor partition.
- It stores, logs, or exports agent content that the label step must redact or strip.
- It breaks data that exists on a user's disk: the audit database, receipts, or keys.
- It crashes or hangs the hook. A hung hook stops the agent.

Style, names, comments, and refactors are Nit at most. Report at most five Nits per review.

## Always check

1. Fail open. An error, a timeout, a payload that does not parse, or an unknown verdict must give
   the verdict that `policy.fail_mode` or the hook fail column names. A blocking hook blocks. Read
   the failure table in `docs/supervisor-dev.md` (section "The client"). The fail-safe defaults
   hold: an unknown classification maps to `unknown_sensitive`, a nil approval outcome denies, and
   a failed identity capture does not allow. A failed CEL condition can only make a decision
   stricter. A change that turns any of these into an allow is Important.
2. Policy merge and self-protection. A `disabled` list acts only on rules of its own file. No file
   removes a built-in rule. A user rule cannot use the `gryph-builtin-` prefix. A block always
   beats an allow. A change to `Loader.Load`, `aarm/loader/builtin.go`, or the precedence order
   that breaks one of these is Important. See `docs/security-policy-threat-model.md`.
3. Receipt and context hashes. They are consensus formats. A change to field order, field set, or
   canonicalization without a new `hash_version`, a verifier update, and property tests is
   Important. So is a change that lets a receipt keep content that the stored event loses. The
   supervisor signs only with the machine key, and root never reads a user's private key.
4. Supervisor trust. The server trusts the kernel peer credentials, never a frame. No request field
   names a partition or a path that the server opens. A read or a write in one uid partition must
   not reach another partition. The forbidigo rule catches a raw `os.Open` in `supervisor/`. It
   does not catch a file access through a helper in another package, or `os.Remove`, `os.Rename`,
   `os.Chmod`, or a directory walk. Report such an access outside `platform/nofollow` as Important.
5. Content labels. Agent content goes in a `privacy.Text` field with `json:",omitzero"`. An adapter
   does not redact, label, or strip content. `decision.Local` does it. A log line through
   `dry/log`, an error message, or an export that carries a raw prompt, command, tool input, or
   file content past the label step is Important. Read `docs/content-labels.md`.
6. Layer rules. `aarm` imports neither `storage` nor `cli`. An adapter keeps agent paths, file
   names, and command rules in its own package, never in `aarm/loader` or `aarm/pdp`. Shell
   commands go through `aarm/shellcmd`, never a regex. Adapters register only in
   `RegisterAdapters` in `engine/engine.go`. A break of these rules is Important. A new adapter
   follows every step of `docs/agent-adapter.md`.
7. Ent schema. A change in `storage/ent/schema/` needs the regenerated code from `make generate` in
   the same change. CI does not check this. Missing code is Important. A schema change that drops
   or retypes a column of existing user data is Important.
8. Acceptance. A new user-facing guarantee needs a script under `test/acceptance/scripts/` and a
   row in `test/acceptance/catalog.yaml`. A missing script for a block, a fail
   mode, self-protection, or the supervisor is Important. For other guarantees it is a Nit.
9. Docs that the change makes wrong. A change to the policy surface (commands, flags, rule fields,
   config keys under `policy:`, defaults) updates `docs/security-policy.md`. A change to AARM
   internals updates `docs/aarm-dev.md`. A wire format change updates `docs/supervisor-dev.md`.
   A missing update is a Nit, or Important when the doc then tells a user a wrong security
   behavior.
10. Tests use `testify` `assert` and `require`. A direct `t.Fatal` or `t.Error` is a Nit. A bug fix
    without a test that fails on the old code is a Nit.

## Writing

Docs, comments, and user-facing messages use ASD-STE100: short sentences, active voice, present
tense. They use ASCII only, with no emoji and no em-dash. A semicolon does not join two
sentences. A break of these rules is a Nit.

## Evidence

A claim about behavior needs a `file:line` citation in the source, not an inference from a name.
For a fail-open claim, cite the line that returns the allow and the path that reaches it.

## Do not report

CI and the hooks already enforce these. Do not report them:

- golangci-lint findings, gofmt, and the forbidigo rule on raw `os` file calls in `supervisor/`.
- Secrets that TruffleHog finds in CI. CI does not run gitleaks, so a secret that only gitleaks
  would find stays in scope.
- JSON Schema drift in `schema/*.json`. The `verify-schema` job runs `make generate-schema`.
- A catalog row without a script, or a script without a row. `TestCatalogIntegrity` fails.
- A stale `docs/agent-enforcement-coverage.md`. `TestEnforcementCoverageDoc` fails.
- A hook config file that an adapter writes but does not declare in `HookConfigPaths()`.
  `TestAdapters_InstallWritesOnlyDeclaredHookConfig` fails.
- A forbidden import in `supervisor/`. `supervisor/architecture_test.go` fails.

Do not review generated or vendored files: `storage/ent/` outside `storage/ent/schema/`,
`schema/*.json`, `pricing/models.json`, `go.sum`, and `website/package-lock.json`.

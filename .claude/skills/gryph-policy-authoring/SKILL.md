---
name: gryph-policy-authoring
description: Use when a user wants to author, edit, install, or test a Gryph security policy that governs what an AI coding agent may do. Trigger whenever the user wants Gryph to block, allow, warn on, or require approval for an agent action, wants to write or change a rule in a policy file, asks how to match a tool, command, file path, or URL, works with CEL conditions or context counters in a rule, wants to split policy into many files, or wants to dry-run and verify a policy with `gryph policy test`. Also trigger on phrases like "write a gryph policy", "add a policy rule", "make gryph block X", "test my policy", "install a policy", or "policy for my agent", even when the user does not name a file. This skill is for authoring and testing policy content, not for changing the policy engine code (use aarm-policy-layer for engine work under aarm/ or cli/policy.go).
---

# Authoring and Testing Gryph Policies

Gryph mediates what an AI coding agent reads, writes, and runs. A policy is an ordered
list of rules. Each rule matches an action and returns a decision. This skill helps a
user author those rules and test them before they go live.

Gryph resolves user policy from two places, both in Gryph's config directory: the global
`policy.yaml`, and every `*.yaml` file in the `policies/` directory. Gryph merges its own
built-in self-protection rules last. So the effective policy is your files plus the
built-ins. Keep one global file, or split rules into many small files in `policies/`, one
concern per file. Rule IDs must be unique across every file. Per-project policy is not
wired today.

Two sources of truth back this work. Use both. Do not author rules from memory.

- `docs/security-policy.md` in this repo is the full author guide. It explains match
  criteria, scope, CEL conditions, decisions, messages, receipts, deferrals, approval,
  risk signals, and has worked examples. Read the section you need before you write a
  rule.
- The installed `gryph` binary is the authoritative, version-matched surface. Drive it
  for the exact schema and for testing. The doc explains the concepts, the binary
  confirms the fields.

## The authoring loop

Follow this loop. Each step feeds the next.

1. Open a policy file. Run `gryph policy list` first to see every active source and its
   rule count. To edit the global file, run `gryph policy edit`. To author a separate
   file for one concern, run `gryph policy edit <name>`, which opens
   `policies/<name>.yaml` and scaffolds it from the commented example when it is missing.
   Run `gryph policy init` (or `gryph policy init <name>`) to scaffold the example
   without opening an editor. Never run `gryph policy init --force` on an existing file
   without explicit confirmation, because it overwrites the user's rules with the
   example. `gryph policy validate` prints the resolved paths.

2. Learn the surface from the binary, not from guesswork. Run `gryph policy schema` for
   the authoritative rule fields, match criteria, and the decision set. Run
   `gryph policy builtin` to read Gryph's own built-in rules as working, real-world
   examples to imitate. If you are unsure a field exists or a decision is spelled right,
   check `schema` before you write it.

3. Author the rule. Match on `action_types`, tools, file patterns, commands, or URLs,
   then narrow with a CEL `condition` when the match criteria are not enough. Read the
   "Authoring a rule", "Match criteria", and "Conditions (CEL)" sections of
   `docs/security-policy.md` for the field details.

4. Validate. Run `gryph policy validate`. It compiles the merged policy and reports the
   rule count. Fix every error before you test. `gryph policy edit` runs this for you on
   save when the target is the global file or a named file.

5. Test with synthetic actions. This is the core of getting a rule right. See the
   testing section below.

6. Iterate. Adjust the rule and repeat from step 4 until the decisions match intent.

## Authoring as an agent: the review gate

An agent cannot write into the config directory. The built-in self-protection rules
block an agent `file_write` into `policy.yaml` and `policies/`. So an agent does not edit
the live policy. It writes a candidate, then a human installs it.

1. Write a candidate at a normal, unprotected path: `gryph policy init ./candidate.yml`.
   This path is a candidate, not live policy. Gryph never resolves it.
2. Edit `./candidate.yml` and add the rules.
3. Validate the candidate alone: `gryph policy validate --file ./candidate.yml`.
4. Hand it to a human. The human reviews the file and runs
   `gryph policy install ./candidate.yml`, which validates it, checks it against the
   merged policy, and copies it into `policies/`.

`install` refuses a candidate that breaks the merge, for example a duplicate rule ID
across files. Do not try to get an agent to write into `policies/` directly. The block is
by design, and it is the human review gate.

## Testing a rule

`gryph policy test` dry-runs a synthetic action through the merged policy. It does not
touch the database and does not run an agent, so it is safe to run as often as you like.

Build the action from flags that describe what an agent would do:

```bash
gryph policy test --action command_exec --command "rm -rf /"
gryph policy test --action file_write --path /app/prod/config.yaml
gryph policy test --action tool_use --tool WebFetch --url https://example.com
```

Use `--format json` when you want to read the decision and the matched rule
programmatically. Set context counters with flags like `--context-files-written 25` to
exercise rules that read `context.*` variables, without a live session. Run
`gryph policy test --help` for the full flag list.

For every rule, test three cases. This catches both false negatives and false positives:

1. An action that must match the rule.
2. An action that must not match, so the rule is not too broad.
3. An action near the boundary, to confirm where the rule stops.

## Decisions and precedence

A rule returns one decision. When several rules match one action, the most restrictive
wins. The order, strongest first, is:

`block > escalate > defer > guidance > warn > allow`

Confirm the current decision names and semantics from `gryph policy schema` and the
"Decisions" section of `docs/security-policy.md`. Precedence, not file order, decides. An
`allow` can never override a `block`. Read `gryph policy builtin` to see what already
fires, so a broad block does not swallow a case you meant to warn on.

## Author safely

These habits come from the user guide and prevent lockouts and silent misfires.

- Start a new rule with `enabled: false`, or with `action: warn`. Watch the receipts with
  `gryph policy receipts`, confirm it fires on the right actions, then promote it to
  `block`.
- Keep `policy.fail_mode: closed` during testing so a broken policy blocks actions
  instead of allowing them silently. If a broken policy locks you out, set
  `fail_mode: open`, fix the policy, and set it back. Do not ship `fail_mode: open`.
- `disabled:` is a top-level list of rule IDs. It turns off your own rules. It is scoped
  to the file that declares the rule, so it cannot reach a rule in another file. It cannot
  turn off a built-in rule. To turn the built-ins off, set
  `policy.self_protection.enabled: false` in the config file. This turns off all of them
  at once and is not recommended.
- Namespace rule IDs by the file's purpose. IDs must be unique across every policy file,
  and `install` rejects a candidate that collides.
- Match criteria are cheap and readable. Reach for a CEL `condition` only when the match
  criteria cannot express the rule.

## Where things live

- User policy: `policy.yaml` and the `policies/` directory, both in Gryph's config
  directory. `gryph policy validate` prints the resolved paths, and `gryph policy list`
  shows every active source.
- Config keys under `policy:` (fail_mode, receipts, approval, self_protection) live in
  the Gryph config file, not in a policy file.

## Keep the guide honest

`docs/security-policy.md` is the source of truth for authors. If you find the binary
behaves differently from the doc, tell the user and prefer the binary, because it is the
installed version. Do not copy large parts of the doc into this skill. Point at the doc
and drive the binary.

---
name: gryph-policy-authoring
description: Use when a user wants to author, edit, or test a Gryph security policy that governs what an AI coding agent may do. Trigger whenever the user wants Gryph to block, allow, warn on, or require approval for an agent action, wants to write or change a rule in policy.yaml, asks how to match a tool, command, file path, or URL, works with CEL conditions or context counters in a rule, or wants to dry-run and verify a policy with `gryph policy test`. Also trigger on phrases like "write a gryph policy", "add a policy rule", "make gryph block X", "test my policy", or "policy for my agent", even when the user does not name a file. This skill is for authoring and testing policy content, not for changing the policy engine code (use aarm-policy-layer for engine work under aarm/ or cli/policy.go).
---

# Authoring and Testing Gryph Policies

Gryph mediates what an AI coding agent reads, writes, and runs. A policy is an ordered
list of rules. Each rule matches an action and returns a decision. This skill helps a
user author those rules and test them before they go live.

There is one user-authored policy file: the global `policy.yaml` in Gryph's config
directory. Authoring means adding or changing entries in its ordered `rules:` list.
Gryph also merges its own built-in self-protection rules at load time, so the effective
policy is your file plus the built-ins. Project-local policy files are not wired today.

Two sources of truth back this work. Use both. Do not author rules from memory.

- `docs/security-policy.md` in this repo is the full author guide. It explains match
  criteria, scope, CEL conditions, decisions, messages, receipts, deferrals, approval,
  risk signals, and has four worked examples. Read the section you need before you
  write a rule.
- The installed `gryph` binary is the authoritative, version-matched surface. Drive it
  for the exact schema and for testing. The doc explains the concepts, the binary
  confirms the fields.

## The authoring loop

Follow this loop. Each step feeds the next.

1. Open the policy. Almost always the user already has a `policy.yaml`, so run
   `gryph policy edit` to open the single global file and add your rule to the `rules:`
   list. Only when no policy exists yet, run `gryph policy init` to scaffold the fully
   commented example first, because every field there carries an inline comment. Never
   run `gryph policy init --force` on an existing policy without explicit confirmation,
   because it overwrites the user's rules with the example. `gryph policy validate`
   prints the resolved file path if you need to confirm which file you are editing.

2. Learn the surface from the binary, not from guesswork. Run `gryph policy schema` for
   the authoritative rule fields, match criteria, and the decision set. Run
   `gryph policy builtin` to read Gryph's own built-in rules as working, real-world
   examples to imitate. If you are unsure a field exists or a decision is spelled right,
   check `schema` before you write it.

3. Author the rule. Match on `action_types`, tools, file patterns, commands, or URLs,
   then narrow with a CEL `condition` when the match criteria are not enough. Read the
   "Authoring a rule", "Match criteria", and "Conditions (CEL)" sections of
   `docs/security-policy.md` for the field details.

4. Validate. Run `gryph policy validate`. It parses and compiles the policy and reports
   the rule count. Fix every error before you test. `gryph policy edit` runs this for
   you on save.

5. Test with synthetic actions. This is the core of getting a rule right. See the
   testing section below.

6. Iterate. Adjust the rule and repeat from step 4 until the decisions match intent.

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
exercise rules that read `context.*` variables, without needing a live session. Run
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
"Decisions" section of `docs/security-policy.md`. Precedence matters when you add a new
rule on top of the built-ins. Read `gryph policy builtin` to see what already fires so a
narrow allow does not get overridden, or a broad block does not swallow a case you meant
to warn on.

## Author safely

These habits come from the user guide and prevent lockouts and silent misfires.

- Start a new rule as `action: warn` or with `disabled: true`. Watch the receipts with
  `gryph policy receipts list`, confirm it fires on the right actions, then promote it
  to `block`.
- Keep `policy.fail_mode: closed` during testing so a broken policy blocks actions
  instead of allowing them silently. If a broken policy locks you out, set
  `fail_mode: open`, fix the policy, and set it back. Do not ship `fail_mode: open`.
- To neutralize a built-in rule without forking it, add its rule ID to the top-level
  `disabled:` list in your policy. A project-local policy can turn off a global rule
  this way.
- Match criteria are cheap and readable. Reach for a CEL `condition` only when the match
  criteria cannot express the rule.

## Where things live

- Global policy: `policy.yaml` in Gryph's config directory. `gryph policy validate`
  prints the resolved path.
- Config keys under `policy:` (fail_mode, receipts, approval) live in the Gryph config
  file, not in `policy.yaml`.

## Keep the guide honest

`docs/security-policy.md` is the source of truth for authors. If you find the binary
behaves differently from the doc, tell the user and prefer the binary, because it is the
installed version. Do not copy large parts of the doc into this skill. Point at the doc
and drive the binary.

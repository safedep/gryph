---
name: gryph-policy-authoring
description: Use when a human wants help to author or change a Gryph security policy that governs what an AI coding agent may do, often with an agent's help. Trigger whenever the user wants Gryph to block, allow, warn on, or require approval for an agent action, wants to write or change a policy rule, asks how to match a tool, command, file path, or URL, works with CEL conditions or context counters, wants to split policy into many files, or wants to dry-run a rule. Also trigger on phrases like "write a gryph policy", "add a policy rule", "make gryph block X", "help me write a policy", or "policy for my agent", even when the user does not name a file. This skill drafts a policy in a workspace directory and hands the user an install command. It never installs the policy and never writes into Gryph's config directory. It is not for changing the policy engine code.
---

# Authoring Gryph Policies

Gryph mediates what an AI coding agent reads, writes, and runs. A policy is an ordered
list of rules. Each rule matches an action and returns a decision. This skill helps a
human draft those rules, with an agent's help, then hands the human a command to install
them.

You draft in a workspace directory. You never install. You never write into Gryph's
config directory. The human reviews the draft and runs the install command you give at
the end. This keeps the human in control of what becomes active policy.

## The two hard rules

1. Draft only in a workspace directory you create, for example `./gryph-policy/`. Never
   write a policy file into Gryph's config directory or its `policies/` directory. Gryph
   blocks an agent write there by design, and a direct write bypasses the human review.
2. Never install. Do not run `gryph policy install`, `gryph policy edit`, or
   `gryph policy init <name>` (a bare name writes into `policies/`). These change live
   policy. Your job ends at a validated draft plus an install command for the human.

## The gryph binary is the source of truth

The installed `gryph` binary is authoritative and version-matched. Read the schema and
the existing policy from it. Do not author fields from memory.

- `gryph policy schema` prints the exact rule fields, match criteria, and decision set.
  This is the single source of truth for the schema. Check it before you write a field.
- `gryph policy builtin` prints Gryph's own built-in rules. Read them as working examples
  to imitate, and to see what already fires so your rule does not collide with them.
- `gryph policy list` shows every active source and its rule count.

The security policy guide at
https://github.com/safedep/gryph/blob/main/docs/security-policy.md is a concept guide.
It explains match criteria, scope, CEL conditions, decisions, messages, receipts, and has
worked examples. Read the section you need for concepts. When the doc and the binary
disagree, trust the binary and tell the user.

You may read the active policy for context: read the resolved `policy.yaml` (its path is
printed by `gryph policy validate`) and the files in `policies/`. Read only. Never write
there.

## The authoring workflow

1. Make a workspace directory and work inside it.

   ```bash
   mkdir -p gryph-policy
   ```

   This directory is a draft area. Gryph never resolves it. Name each draft file for its
   purpose, for example `gryph-policy/no-prod-writes.yaml`. Namespace the rule IDs the
   same way, because IDs must be unique across every file once installed.

2. Learn the surface. Run `gryph policy schema` for the fields and `gryph policy builtin`
   for real examples. Run `gryph policy list` to see what is already active.

3. Scaffold a draft. Run `gryph policy init ./gryph-policy/<name>.yaml` to write the fully
   commented example to the workspace, then edit it. The path form writes to that exact
   file, so it stays a draft. Do not use a bare name.

4. Author the rule. Match on `action_types`, tools, file patterns, commands, or URLs.
   Narrow with a CEL `condition` only when the match criteria cannot express the rule.

5. Validate the draft in isolation.

   ```bash
   gryph policy validate --file ./gryph-policy/<name>.yaml
   ```

   This compiles the one file without touching the active policy. Fix every error before
   you continue.

6. Dry-run the draft. `gryph policy test --file` evaluates a synthetic action against your
   draft file plus the built-in rules, without resolving the active policy. Test three
   cases per rule: an action that must match, an action that must not match, and an action
   near the boundary.

   ```bash
   gryph policy test --file ./gryph-policy/<name>.yaml --action command_exec --command "rm -rf /"
   gryph policy test --file ./gryph-policy/<name>.yaml --action file_write --path /app/prod/config.yaml
   gryph policy test --file ./gryph-policy/<name>.yaml --action tool_use --tool WebFetch --url https://example.com
   ```

   Iterate: fix the rule, then repeat from step 5 until the decisions match intent.

7. Hand the human an install command. Print the exact command and let the human run it.

   ```bash
   gryph policy install ./gryph-policy/<name>.yaml
   ```

   `install` validates the file, checks it against the merged policy, and copies it into
   `policies/`. It refuses a file that breaks the merge, for example a duplicate rule ID.
   The human runs it. You do not.

## Testing

`gryph policy validate --file <path>` checks that a draft compiles. `gryph policy test
--file <path>` dry-runs a synthetic action against that draft plus the built-in rules. It
does not touch the database and does not run an agent, so run it as often as you like.

`test --file` shows your rules and the built-ins. It does not include other installed
files, because your draft is not yet merged with them. So the decision reflects your file
against the built-in rules. The full test, with every active file, happens after the human
installs the draft and runs plain `gryph policy test` and `gryph policy receipts`.
It is safe for the human to install a draft and try it. A new file can only add rules. It
cannot remove the built-in rules or another file's rules. To roll back, the human
removes the file from `policies/`.

Set context counters with flags like `--context-files-written 25` to exercise rules that
read `context.*` variables. Run `gryph policy test --help` for the full flag list.

## Decisions and precedence

A rule returns one decision. When several rules match one action, the most restrictive
wins. The order, strongest first, is:

`block > escalate > defer > guidance > warn > allow`

Confirm the current names from `gryph policy schema`. Precedence, not file order, decides.
An `allow` can never override a `block`.

## Author safely

- Start a new rule with `enabled: false`, or with `action: warn`. After the human
  installs it, watch `gryph policy receipts`, confirm it fires on the right actions, then
  promote it to `block`.
- `disabled:` is a top-level list of rule IDs. It turns off your own rules. It is scoped
  to the file that declares the rule, so it cannot reach a rule in another file. It cannot
  turn off a built-in rule. To turn the built-ins off, the operator sets
  `policy.self_protection.enabled: false` in the config file, which turns off all of them.
- Namespace rule IDs by the file's purpose. IDs must be unique across every policy file.
- Keep `policy.fail_mode: closed`. Do not advise `fail_mode: open` except as a temporary
  step to recover from a lockout.
- Config keys under `policy:` (fail_mode, receipts, approval, self_protection) live in the
  Gryph config file, not in a policy file.

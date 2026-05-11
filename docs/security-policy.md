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
context.{total_actions, files_read, files_written, commands_executed,
         network_requests, errors, tools_used, session_duration_ms}
```

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
| `escalate` | Reserved for the upcoming approval workflow. Today it falls back to `block`. | 2 |

When multiple rules match, the most restrictive wins:

```
block > escalate > guidance > warn > allow
```

## Commands

| Command | Purpose |
|---|---|
| `gryph policy init <path>` | Write a fully documented example to `<path>`. Use as a template. |
| `gryph policy schema` | Print the JSON Schema. Pipe into editor tooling or an AI agent. |
| `gryph policy sources` | Show the resolved source list without loading any files. |
| `gryph policy validate` | Parse and compile every configured source. Reports rule count and source list. |
| `gryph policy test ...` | Dry-run a synthetic action through the merged policy. See `--help` for flags. |

`gryph policy test` accepts `--format json` for machine-readable output.

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

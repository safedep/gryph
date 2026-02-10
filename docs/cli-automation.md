# CLI Automation

`gryph export` outputs events as [JSON Lines](https://jsonlines.org/) to stdout, making it composable with standard Unix tools like `jq`, `sort`, `uniq`, and `wc` for ad-hoc analysis without writing Go code.

Each line is a complete `Event` object with a `$schema` field for validation.

```bash
gryph export --since 1d | jq .
```

## Quick Counts & Summaries

Count events by action type:

```bash
gryph export --since 1w | jq -r '.action_type' | sort | uniq -c | sort -rn
```

Total actions today:

```bash
gryph export --since 1d | wc -l
```

Activity breakdown per agent:

```bash
gryph export --since 1w | jq -r '.agent_name' | sort | uniq -c | sort -rn
```

Action types per agent:

```bash
gryph export --since 1w | jq -r '[.agent_name, .action_type] | @tsv' | sort | uniq -c | sort -rn
```

## File Analysis

Unique files read:

```bash
gryph export --since 1w | jq -r 'select(.action_type == "file_read") | .payload.path // empty' | sort -u
```

Unique files written:

```bash
gryph export --since 1w | jq -r 'select(.action_type == "file_write") | .payload.path // empty' | sort -u
```

Most-edited files (by write count):

```bash
gryph export --since 1w | jq -r 'select(.action_type == "file_write") | .payload.path // empty' | sort | uniq -c | sort -rn | head -20
```

Code churn — total lines added and removed:

```bash
gryph export --since 1w | jq -r 'select(.action_type == "file_write") | [.payload.lines_added // 0, .payload.lines_removed // 0] | @tsv' | awk -F'\t' '{a+=$1; r+=$2} END {printf "+%d -%d\n", a, r}'
```

File types touched (extension breakdown):

```bash
gryph export --since 1w | jq -r 'select(.action_type == "file_write" or .action_type == "file_read") | .payload.path // empty' | grep -oE '\.[a-zA-Z0-9]+$' | sort | uniq -c | sort -rn
```

## Command Audit

All shell commands executed:

```bash
gryph export --since 1d | jq -r 'select(.action_type == "command_exec") | .payload.command // empty'
```

Failed commands (non-zero exit code):

```bash
gryph export --since 1w | jq 'select(.action_type == "command_exec" and .payload.exit_code != 0) | {command: .payload.command, exit_code: .payload.exit_code, error: .error_message}'
```

Longest-running commands (by duration):

```bash
gryph export --since 1w | jq -r 'select(.action_type == "command_exec") | [.payload.duration_ms // .duration_ms // 0, .payload.command // ""] | @tsv' | sort -rnk1 | head -10
```

## Errors & Security

All errors with messages:

```bash
gryph export --since 1w | jq 'select(.result_status == "error") | {action: .action_type, tool: .tool_name, error: .error_message}'
```

Blocked or rejected actions:

```bash
gryph export --since 1w | jq 'select(.result_status == "blocked" or .result_status == "rejected") | {action: .action_type, tool: .tool_name, status: .result_status, error: .error_message}'
```

Sensitive file access audit (requires `--sensitive` to include these events):

```bash
gryph export --since 1w --sensitive | jq 'select(.is_sensitive) | {action: .action_type, path: .payload.path, tool: .tool_name, timestamp: .timestamp}'
```

## Session Analysis

Events per session:

```bash
gryph export --since 1w | jq -r '.session_id' | sort | uniq -c | sort -rn
```

Session timeline (first and last event per session):

```bash
gryph export --since 1w | jq -r '[.session_id, .timestamp, .action_type] | @tsv' | sort -k1,1 -k2,2 | awk -F'\t' '!seen[$1]++ {first[$1]=$2} {last[$1]=$2; count[$1]++} END {for (s in first) printf "%s\t%s\t%s\t%d events\n", s, first[s], last[s], count[s]}'
```

## Pipelines & Integration

Save a weekly report to file:

```bash
gryph export --since 1w -o weekly-audit.jsonl
```

Filter an agent and save:

```bash
gryph export --since 1w --agent claude-code -o claude-code-audit.jsonl
```


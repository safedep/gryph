# Hook latency baseline

This file holds one reference run of `make bench-hook` and the budget rule
for changes to the hook path. The numbers depend on the machine. Compare two
runs from one machine only, from one `make bench-hook` invocation where that
is possible.

## Method

`test/perf` runs the real `gryph` binary once per hook, as an agent does:
payload on stdin, decision on exit. Each benchmark has its own home,
config and database. One warm run creates the database and the session
before the measured runs. The percentiles use the nearest rank over the
measured runs.

Benchmarks cover the hook kinds of the fail-mode matrix for Claude Code:

| Benchmark      | Hook               | Fixture                   |
| -------------- | ------------------ | ------------------------- |
| `pre-blocking` | `PreToolUse`       | `pre_tool_use_bash.json`  |
| `prompt`       | `UserPromptSubmit` | `user_prompt_submit.json` |
| `post`         | `PostToolUse`      | `post_tool_use_read.json` |
| `lifecycle`    | `SessionStart`     | `session_start.json`      |

`policy-off` records the event only. `policy-on` also runs the built-in
rules and one user rule, the context accumulator, and the receipt chain.

## Budget

A change to the hook path keeps the p99 of every benchmark within the p99
of the same benchmark on the same machine before the change, plus 10
percent. A later client mode that sends the hook to a local service is
measured against `policy-on` in the same run.

## Reference run

Measured through the real `gryph` binary, one process per hook, on linux/amd64 with 4 CPUs, Go go1.25.6, at 2026-10-03T10:50:22Z.

| Benchmark | Runs | p50 | p95 | p99 |
|---|---|---|---|---|
| lifecycle/policy-off | 50 | 26.1ms | 28.7ms | 36.6ms |
| lifecycle/policy-on | 50 | 31.4ms | 40.2ms | 45.1ms |
| post/policy-off | 50 | 27.8ms | 35ms | 38.2ms |
| post/policy-on | 50 | 36.4ms | 44.8ms | 58.3ms |
| pre-blocking/policy-off | 50 | 28.3ms | 32.3ms | 36.3ms |
| pre-blocking/policy-on | 50 | 36.2ms | 46.4ms | 46.9ms |
| prompt/policy-off | 50 | 25.6ms | 30ms | 35.6ms |
| prompt/policy-on | 50 | 35.2ms | 45ms | 47.9ms |

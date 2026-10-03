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
percent. The client mode, which sends the hook to the decision service, is
measured against `policy-on` in the same run.

## Client mode and the guard

`GRYPH_PERF_CLIENT=1 make bench-hook` adds a `client` row per phase. The
benchmark needs root on Linux: it writes the managed configuration to
`/etc/safedep/gryph` for its own duration, starts `gryph supervisor run`
on a socket of its own, sends every hook through it, then stops the
service and removes the directory. It refuses a host that already has a
managed directory. The managed policy carries the same user rule as
`policy-on`, so both modes evaluate the same rules.

After the run, the guard compares each `client` row with the `policy-on`
row of the same phase from the same run. The client p99 must stay within
the local p99 times `GRYPH_PERF_BUDGET` (default `1.10`). A phase over the
budget fails the run and the guard prints both p95 and p99 of every phase.
CI runs the guard in the privileged job, after the privileged acceptance
scripts, with `BENCHTIME=50x`.

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

## Reference run, client mode

Measured on the same kind of machine as the reference run above, as root,
with `GRYPH_PERF_CLIENT=1` and `BENCHTIME=50x`, at 2026-10-03T19:20:48Z.
The hook through the service is faster than the local `policy-on` mode,
because the service holds the open database and the loaded policy, and
the hook process only parses the payload and talks to the socket.

| Benchmark | Runs | p50 | p95 | p99 |
|---|---|---|---|---|
| lifecycle/client | 50 | 15.6ms | 23.2ms | 24.8ms |
| lifecycle/policy-on | 50 | 36.9ms | 46.8ms | 48.7ms |
| post/client | 50 | 15.3ms | 23ms | 27ms |
| post/policy-on | 50 | 38.6ms | 46.1ms | 49.3ms |
| pre-blocking/client | 50 | 19.8ms | 21.9ms | 22.5ms |
| pre-blocking/policy-on | 50 | 38.2ms | 43.5ms | 46.1ms |
| prompt/client | 50 | 16.3ms | 20.5ms | 25.7ms |
| prompt/policy-on | 50 | 36.1ms | 41.1ms | 42.9ms |

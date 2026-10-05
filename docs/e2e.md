# E2E Tests

E2E tests live in `test/cli/` as package `cli_test`, separate from the `cli/` package.

## Running

```bash
go test -v ./test/cli/                  # all E2E tests
go test -v -run "^TestHook" ./test/cli/ # specific group
```

## Test Isolation

Each test gets its own temp directory with a dedicated SQLite DB and config file.
Nothing touches the real system.

```go
env := newTestEnv(t)
stdout, stderr, err := env.run("logs", "--format", "json")
```

`newTestEnv` creates a config with `logging.level: full`, `display.colors: never`,
and `storage.path` pointing to the temp DB. Use `newTestEnvWithConfig(t, yaml)` to
override.

## Running Commands

`env.run(args...)` builds a fresh `cli.NewRootCmd()`, prepends `--config` and
`--no-color` flags, captures stdout/stderr into buffers, and returns them with the
error.

For hook commands that read stdin:

```go
payload, _ := os.ReadFile("../../agent/claudecode/testdata/pre_tool_use_write.json")
stdout, stderr, err := env.runHook("claude-code", "PreToolUse", payload)
```

`runHook` injects payload via `os.Pipe()` into stdin. Tests using `runHook` must
**not** use `t.Parallel()`.

## Seeding Data

Two approaches:

**Direct store seeding** for tests that need controlled data:

```go
env.seedStore(func(ctx context.Context, store storage.Store) {
    sess := session.NewSessionWithID(uuid.New(), "claude-code")
    sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
    sess.WorkingDirectory = "/tmp/project"
    require.NoError(t, store.SaveSession(ctx, sess))

    evt := events.NewEvent(sess.ID, "claude-code", events.ActionFileRead)
    evt.Sequence = 1
    evt.Timestamp = time.Now().UTC()
    evt.ResultStatus = events.ResultSuccess
    evt.ToolName = "Read"
    payload := &events.FileReadPayload{Path: "/tmp/project/main.go"}
    require.NoError(t, evt.SetPayload(payload))
    require.NoError(t, store.SaveEvent(ctx, evt))
})
```

**Via `runHook`** for testing the full hook-to-store pipeline (see hook tests).

Reusable seed functions: `seedNRecentEvents(n)`, `seedMixedAgentEvents`,
`seedTodayAndYesterdayEvents`, `seedWithPaths`, `seedWithCommands`,
`seedWithErrors`, `seedOldEvents`, `seedOldAndRecentEvents`, `seedMixedActions`,
`seed3Sessions`.

## Verifying Store State

Open a read handle to assert on DB contents after a command:

```go
store, cleanup := env.openStore()
defer cleanup()
evts, err := store.QueryEvents(ctx, events.NewEventFilter())
assert.Len(t, evts, 3)
```

## Assertion Helpers

Reusable closures for table-driven test entries:

| Helper                          | Signature              | Checks                        |
| ------------------------------- | ---------------------- | ----------------------------- |
| `assertEventCount(n)`           | `func(t, stdout, err)` | JSON array has n events       |
| `assertAllEventsFromAgent(a)`   | `func(t, stdout, err)` | Every event's agent matches   |
| `assertAllActionsAre(a)`        | `func(t, stdout, err)` | Every event's action matches  |
| `assertActionsIn(a...)`         | `func(t, stdout, err)` | Actions within allowed set    |
| `assertOutputContains(s)`       | `func(t, stdout, err)` | stdout contains substring     |
| `assertSessionCount(n)`         | `func(t, stdout, err)` | JSON array has n sessions     |
| `assertAllSessionsFromAgent(a)` | `func(t, stdout, err)` | Every session's agent matches |
| `assertValidJSONArray`          | `func(t, stdout)`      | stdout is a valid JSON array  |
| `assertValidJSONL`              | `func(t, stdout)`      | stdout is valid JSONL         |
| `assertValidCSV(n)`             | `func(t, stdout)`      | CSV has header + n rows       |

## Writing a New Test

1. Pick the right file or create `e2e_<command>_test.go`.
2. Use table-driven tests with `name`, `args`, optional `setup`, and `assert`.
3. Seed via helpers or inline `seedStore` calls.
4. Assert on stdout string, error, or store state.

```go
func TestMyCommand(t *testing.T) {
    tests := []struct {
        name   string
        args   []string
        setup  func(env *testEnv)
        assert func(t *testing.T, stdout string, err error)
    }{
        {
            name:  "basic_case",
            args:  []string{"mycommand", "--flag", "value"},
            setup: seedNRecentEvents(5),
            assert: func(t *testing.T, stdout string, err error) {
                assert.NoError(t, err)
                assert.Contains(t, stdout, "expected output")
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            env := newTestEnv(t)
            if tt.setup != nil {
                tt.setup(env)
            }
            stdout, _, err := env.run(tt.args...)
            tt.assert(t, stdout, err)
        })
    }
}
```

## Hook Latency Benchmark

`test/perf` measures the hook path through the real `gryph` binary, one
process per hook with the payload on stdin, for each hook kind (blocking pre
hook, prompt hook, post hook, lifecycle hook) with policy off and on. It
reports the mean and the p50, p95 and p99 of each benchmark.

```bash
make bench-hook              # 50 runs per benchmark
make bench-hook BENCHTIME=200x
```

The report lands in `perf-reports/hook-latency.md`, a directory that is not
in the repository. CI runs the benchmark on every push and keeps the report
as an artifact. The numbers depend on the machine, so compare two runs from
one machine only, from one `make bench-hook` invocation where that is
possible.

Each benchmark has its own home, configuration and database. One warm run
creates the database and the session before the measured runs. The
percentiles use the nearest rank over the measured runs. `policy-off`
records the event only. `policy-on` also runs the built-in rules and one
user rule, the context accumulator and the receipt chain.

### Budget

A change to the hook path keeps the p99 of every benchmark within the p99
of the same benchmark on the same machine before the change, plus 10
percent.

### Client mode and the guard

`GRYPH_PERF_CLIENT=1 make bench-hook` adds a `client` row per phase, which
sends every hook to the decision service. The benchmark needs root on
Linux: it writes the managed configuration to `/etc/safedep/gryph` for its
own duration, starts the service on a socket of its own, then stops it and
removes the directory. It refuses a host that already has a managed
directory. The managed policy carries the same user rule as `policy-on`,
so both modes evaluate the same rules.

After the run, the guard compares each `client` row with the `policy-on`
row of the same phase from the same run. The client p99 must stay within
the local p99 times `GRYPH_PERF_BUDGET` (default `1.10`). A phase over the
budget fails the run, and the guard prints the p95 and the p99 of every
phase. CI runs the guard in the privileged job, after the privileged
acceptance scripts, with `BENCHTIME=50x`.

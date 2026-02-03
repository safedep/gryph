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

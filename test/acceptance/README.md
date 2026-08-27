# Gryph Acceptance Suite

The suite runs the real `gryph` binary through user-facing commands and checks
that Gryph keeps its promises. Every script isolates itself with `HOME`,
`XDG_CONFIG_HOME`, and `XDG_DATA_HOME` under its own work directory. Nothing
touches the host system, and no script needs the network. The `status` and
`doctor` commands may call the GitHub API for the update check. That check
fails soft, so no network never fails a script.

The suite is hermetic, so it gates. CI runs it on every pull request and every
push to main.

The other suites stay separate. `test/cli` runs `cli.NewRootCmd()` in process
and owns pipeline correctness. `test/conformance/aarm` owns AARM spec
conformance. This suite owns the real-binary contracts: exit codes, on-disk
file shapes, stdout and stderr formats, and PATH-resolved hook commands.

## Files

```
test/acceptance/
  catalog.yaml          # guarantee inventory: id, category, tier, guarantee, labels
  catalog.go            # catalog schema, loader, id derivation, selector
  integrity_test.go     # TestCatalogIntegrity: guards catalog and script drift (normal CI)
  acceptance_test.go    # TestAcceptance: real-binary harness (//go:build acceptance)
  report/               # joins JUnit results with the catalog into a Markdown report
  scripts/
    <category>/<capability>/<name>.txtar
```

## Feature id

A script's feature id is its path under `scripts/`, without `.txtar`, with `/`
kept:

```
scripts/policy/block/dangerous-command.txtar -> policy/block/dangerous-command
```

The subtest name, `TestCatalogIntegrity`, and the catalog use this rule to
agree on identity. Put every script at least two levels deep:
`<category>/.../<name>.txtar`.

## Add a case

1. Add a script at `scripts/<category>/.../<name>.txtar`.
2. Add a row to `catalog.yaml`:

   ```yaml
   - id: <category>/.../<name>
     title: "short title"
     category: <category>
     tier: P0                 # P0 | P1 | P2
     guarantee: "what must hold"
     labels: [policy, block]  # optional
   ```

That is the whole change: one script, one catalog row. No scaffolding, no
per-case Go code.

`TestCatalogIntegrity` runs under normal `go test ./...`. It fails when a
script has no catalog row, so a typo cannot become an untracked guarantee. A
catalog row with no script is a gap. It never fails the build.

## Write a script

Scripts are [`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)
txtar files. The harness puts `gryph` on `$PATH`, so `exec gryph ...` runs the
real built binary.

Start every script with the sandbox environment:

```
env HOME=$WORK/home
env XDG_CONFIG_HOME=$WORK/home/.config
env XDG_DATA_HOME=$WORK/home/.local/share
```

Embedded txtar files extract under `$WORK`, so a file section named
`home/.config/gryph/config.yaml` becomes the sandbox config with no copy step.
Feed hook payloads with `stdin <file>` before the `exec gryph _hook ...` line.

The harness adds three commands to the testscript builtins:

- `execexit <code> <command> [args...]` runs a command and asserts its exact
  exit code. Use it for every non-zero exit-code contract. The plain `! exec`
  form only distinguishes zero from non-zero.
- `expandenv <file>...` rewrites `${VAR}` references in a file with values
  from the script environment. Use it when a JSON fixture needs a
  sandbox-absolute path.
- `replace <file> <old> <new>` substitutes a literal string in a file. Tamper
  cases use it.

## Run it

```bash
# Catalog integrity only (fast, offline, part of normal CI):
go test ./test/acceptance/ -run TestCatalogIntegrity

# Full suite (builds the binary from ../../cmd/gryph):
go test -tags acceptance ./test/acceptance/

# Reuse a prebuilt binary:
GRYPH_BIN=$PWD/bin/gryph go test -tags acceptance ./test/acceptance/

# Filter by category or labels:
ACCEPTANCE_CATEGORY=policy go test -tags acceptance ./test/acceptance/
ACCEPTANCE_LABELS=block,receipts go test -tags acceptance ./test/acceptance/

# Render the per-guarantee report from a JUnit file (what CI does):
gotestsum --junitfile acceptance.xml -- -tags acceptance ./test/acceptance
go run ./test/acceptance/report -junit acceptance.xml \
  -catalog test/acceptance/catalog.yaml -scripts test/acceptance/scripts
```

The report groups guarantees by category, then tier: pass, fail, skip,
unknown, gap. It shows the guarantee text and, for a failure, the captured
line. It exits zero for normal results. A missing or broken JUnit file is an
error, not zero coverage.

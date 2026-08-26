# Gryph AARM Conformance Suite

This directory contains the executable conformance suite for the AARM
specification. It maps each AARM requirement bullet (R1..R9) to one or more
Go tests that drive the reference mediator built from `fixtures/` and
asserts the spec-mandated behavior.

The suite is shipped as portable artifacts (`fixtures/`, `AARM_SPEC_VERSION`,
`SUITE_VERSION`, `report.schema.json`) plus Gryph-specific Go test code. Other
AARM implementers can consume the fixtures and the schema and write their
own assertions against their implementation.

## Prerequisites

The Go toolchain must be installed and on PATH to run the suite. This is
true even when the gryph CLI ships with a prebuilt `gryph-conformance.test`
binary alongside it: the CLI runs that binary through `go tool test2json`
because stock Go test binaries emit plain `-test.v` text rather than the
JSON event stream the report parser consumes. Without `go`, the bundled
binary path returns an error rather than silently producing an empty
report. Operators who only need to consume an existing report can skip
this requirement and run the gryph CLI's report rendering against a
previously captured JSON stream.

## How to run

The standard entry point is the gryph CLI:

```
make conformance               # text report (default)
make conformance-json          # JSON report (validates against report.schema.json)
make conformance-markdown      # GitHub-pastable markdown report
```

Or invoke the CLI directly once `bin/gryph` is built:

```
./bin/gryph aarm conformance
./bin/gryph aarm conformance --format json
./bin/gryph aarm conformance --requirement R5
./bin/gryph aarm conformance --include-should
```

You can also run the test package directly without going through the CLI:

```
go test -count=1 -short ./test/conformance/aarm/
```

`-short` skips the meta tests in `suite_of_suite_test.go` that spawn the gryph
binary to validate report shape and byte stability.

## How to interpret

- Each test calls `aarm.Requires(t, R<n>, MUST|SHOULD, "<bullet>")` at the top
  so the CLI parser attributes the result to the right requirement, tier and
  spec bullet. The test name itself is opaque to the parser.
- The CLI parser also recognises `aarm.Skip(t, category, reason)`
  (`not_implemented`, `out_of_scope`, `deferred`, `requires_external`) and
  `aarm.Gap(t, reason)`. Skip categories are first-class in the report; gaps
  surface in the `gaps[]` array.
- Exit code: `0` if every MUST passes, `1` if any MUST fails or errors, `2`
  if the test binary cannot run. SHOULD failures are non-blocking by default
  and become blocking when `--include-should` is set.

## How to add a test when a requirement bullet is closed

1. Pick the right file (`r<N>_*_test.go`). Add a new Go test function whose
   first line is the registration call:

   ```go
   func TestR4_ModifyDecision(t *testing.T) {
       aarm.Requires(t, aarm.R4, aarm.MUST, "MODIFY decision rewrites the action before execution")
       // ... real assertions here ...
   }
   ```

2. Replace any prior `aarm.Skip(...)` with the real assertions. Use
   `helpers.NewReferenceMediator(t)` and the fixture loaders so the test
   stays consistent with the rest of the suite.

3. Run `make conformance` locally to verify the report flips from skip to
   pass for that bullet. Bump `SUITE_VERSION` patch and add a `CHANGELOG.md`
   entry.

## How to update AARM_SPEC_VERSION

When the AARM specification changes:

1. Update `AARM_SPEC_VERSION` to the new revision string.
2. Bump `SUITE_VERSION` minor (or major for schema-incompatible changes).
3. Add a `CHANGELOG.md` entry describing what changed and which tests need
   to be revisited.
4. Walk each `r<N>_*_test.go` test and update the bullet strings to match
   the new spec wording. Test names need not change; the registration
   bullet is the canonical label.

## Hash stability caveat

Receipt hashes are stable only when both the clock and UUID generators are
pinned. Tests that need byte-identical receipt bytes across runs opt into
this by driving the receipt generator directly with a pre-computed
`RecordedAt`. Without that opt-in, two runs over the same input produce
chains that verify internally but have different hash bytes.

## Reference hardware (time-ceiling assertions)

R5 "hash chain verifiable offline within a documented time ceiling" is a
SHOULD by default. The reference number in the suite is generous (10k
receipts under 60 seconds on shared CI). Operators who need a hard upper
bound can fork this assertion and pin a stricter ceiling against their
target hardware.

## Files

| Path | Purpose |
|---|---|
| `AARM_SPEC_VERSION` | Plain text revision identifier of the AARM spec being tested |
| `SUITE_VERSION` | Plain text suite semver (independent of Gryph) |
| `CHANGELOG.md` | Suite changes per SUITE_VERSION |
| `report.schema.json` | JSON Schema for `--format json` output |
| `fixtures/policies/` | Reference, empty, and defer-trigger policy fixtures |
| `fixtures/actions/` | Hand-crafted action fixtures, one per AARM action type |
| `r<N>_*_test.go` | Per-requirement tests (R1..R9) |
| `helpers_test.go` | Shared fixture loaders and PDP driver |
| `suite_of_suite_test.go` | Meta tests that exec the gryph binary to validate report shape and byte stability (skipped under `-short` or when `bin/gryph` is absent) |

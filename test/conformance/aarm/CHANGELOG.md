# AARM Conformance Suite Changelog

All notable changes to the AARM conformance suite are recorded here. The
suite tracks its own semver (`SUITE_VERSION`) independent of the AARM
specification version it tests (`AARM_SPEC_VERSION`). Bumping
`AARM_SPEC_VERSION` always bumps the suite minor version.

The format is loosely based on Keep a Changelog. Schema-incompatible
changes to `report.schema.json` bump the suite major version.

## 0.1.0 - 2026-05-16

- Initial release. R1-R6 MUST coverage matches the manual audit baseline.
  R7 and R9 pre-skipped as `out_of_scope`. R8 partial (streaming
  `deferred`, batch + filtering implemented).
- AARM specification version pinned to 2026-05-16.
- JSON report schema_version 1.

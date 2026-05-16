// Package conformance ships test-only helpers shared by the AARM conformance
// suite under test/conformance/aarm. Tests in the suite call Requires to
// attribute themselves to a numbered AARM requirement (R1..R9), Skip to
// declare a categorized skip, and Gap to flag a divergence the CLI parser
// should surface in the gaps[] bucket.
//
// Sentinel format (load-bearing API between tests and the CLI reporter):
//
//	AARM-REQUIRES: requirement=R5 tier=MUST bullet="Receipt for every action"
//	AARM-SKIP: category=not_implemented reason="MODIFY decision pending"
//	AARM-GAP: reason="receipt hash mismatch"
//
// These lines are emitted via t.Log and parsed verbatim by
// cli/aarm.go.
//
// Adding a new requirement: extend the Requirement and Tier constant sets,
// keep the AARM-REQUIRES sentinel format unchanged, then add tests under
// test/conformance/aarm that call Requires with the new constant.
//
// The suite is independent of the AARM spec version: the spec version is
// pinned by test/conformance/aarm/AARM_SPEC_VERSION (plain text), and the
// suite has its own SUITE_VERSION file alongside.
package conformance

import (
	"fmt"
	"testing"
)

// Requirement is the numbered AARM requirement an individual test attributes
// to. The full list lives in the AARM specification: R1 Pre-Execution
// Mediation, R2 Context Awareness, R3 Policy Evaluation, R4 Decision
// Outcomes, R5 Tamper-Evident Receipts, R6 Identity and Attribution, R7
// Semantic Distance, R8 Telemetry, R9 Least Privilege.
type Requirement string

// All Requirement constants used by the suite. Tests reference these by
// symbolic name so AARM renumbering (e.g. R7 becomes R8) is a one-line edit
// here, not a sweep across the test tree.
const (
	R1 Requirement = "R1"
	R2 Requirement = "R2"
	R3 Requirement = "R3"
	R4 Requirement = "R4"
	R5 Requirement = "R5"
	R6 Requirement = "R6"
	R7 Requirement = "R7"
	R8 Requirement = "R8"
	R9 Requirement = "R9"
)

// Tier is the AARM compliance tier: MUST requirements gate conformance, SHOULD
// requirements are recorded but do not fail the suite unless the operator
// opts in via --include-should.
type Tier string

// Tier constants.
const (
	MUST   Tier = "MUST"
	SHOULD Tier = "SHOULD"
)

// SkipCategory is the explicit, normalized reason a conformance test is
// skipped. The CLI buckets skips by category rather than conflating them
// under one "skipped" pile.
type SkipCategory string

// Skip category constants. Their on-disk JSON serialization (snake_case
// strings) is fixed by report.schema.json: not_implemented, out_of_scope,
// deferred, requires_external.
const (
	NotImplemented   SkipCategory = "not_implemented"
	OutOfScope       SkipCategory = "out_of_scope"
	Deferred         SkipCategory = "deferred"
	RequiresExternal SkipCategory = "requires_external"
)

// requirementTitles maps each Requirement to its human-readable AARM section
// title. Used by the CLI reporter to label the requirements[] entries.
var requirementTitles = map[Requirement]string{
	R1: "Pre-Execution Mediation",
	R2: "Context Awareness",
	R3: "Policy Evaluation",
	R4: "Decision Outcomes",
	R5: "Tamper-Evident Receipts",
	R6: "Identity and Attribution",
	R7: "Semantic Distance",
	R8: "Telemetry",
	R9: "Least Privilege",
}

// Title returns the canonical title for a Requirement. Unknown requirements
// return the requirement ID itself so the reporter can render something
// even if the constant set drifts.
func Title(r Requirement) string {
	if v, ok := requirementTitles[r]; ok {
		return v
	}
	return string(r)
}

// Requires attributes the calling test to a single AARM requirement + tier +
// bullet. It must be the first call in every conformance test. The bullet
// argument is a short prose label for the requirement bullet under test (free
// form, parsed verbatim by the reporter).
//
// Sub-tests inherit the parent test's attribution: the CLI reporter
// associates the AARM-REQUIRES sentinel emitted at the parent level with
// every sub-test event in the parent's stream.
func Requires(t *testing.T, req Requirement, tier Tier, bullet string) {
	t.Helper()
	t.Logf("AARM-REQUIRES: requirement=%s tier=%s bullet=%q", req, tier, bullet)
}

// Skip emits the AARM-SKIP sentinel with the given category and reason then
// calls t.SkipNow. The CLI counts the test under the categorized skip bucket
// for its registered requirement.
func Skip(t *testing.T, category SkipCategory, reason string) {
	t.Helper()
	t.Logf("AARM-SKIP: category=%s reason=%q", category, reason)
	t.SkipNow()
}

// Gap records a divergence between the implementation and the spec without
// terminating the test. The caller is expected to follow up with t.Errorf (or
// equivalent) when the divergence should fail the suite. Gap entries are
// surfaced in the JSON report's gaps[] array.
func Gap(t *testing.T, reason string) {
	t.Helper()
	t.Logf("AARM-GAP: reason=%q", reason)
}

// FormatRequiresSentinel returns the canonical AARM-REQUIRES sentinel for
// the supplied attributes. Exposed so the CLI parser can construct golden
// strings in its own tests without re-implementing the format.
func FormatRequiresSentinel(req Requirement, tier Tier, bullet string) string {
	return fmt.Sprintf("AARM-REQUIRES: requirement=%s tier=%s bullet=%q", req, tier, bullet)
}

// FormatSkipSentinel returns the canonical AARM-SKIP sentinel.
func FormatSkipSentinel(category SkipCategory, reason string) string {
	return fmt.Sprintf("AARM-SKIP: category=%s reason=%q", category, reason)
}

// FormatGapSentinel returns the canonical AARM-GAP sentinel.
func FormatGapSentinel(reason string) string {
	return fmt.Sprintf("AARM-GAP: reason=%q", reason)
}

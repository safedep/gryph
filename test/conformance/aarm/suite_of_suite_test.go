package aarmconformance_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	aarm "github.com/safedep/gryph/aarm/conformance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envConformanceRunning mirrors cli.EnvSuiteRunning. Defined here as a
// literal so the conformance suite does not import cli (which would pull
// the entire CLI graph into the test binary). When the gryph CLI shells out
// to go test it sets this variable; the suite-of-suite tests below check it
// to avoid an infinite recursion (test exec gryph exec test ...).
const envConformanceRunning = "GRYPH_AARM_CONFORMANCE_RUNNING"

// resolveGryphBinary returns the absolute path to a pre-built gryph binary
// or ("", false) when none exists. Per spec section 5 the suite-of-suite
// tests must not build the binary themselves: they skip on a clean checkout.
// Operators run `make conformance` (which builds the binary first) to
// exercise these tests.
func resolveGryphBinary(t *testing.T) (string, bool) {
	t.Helper()
	root := repoRoot(t)
	if root == "" {
		return "", false
	}
	binPath := filepath.Join(root, "bin", "gryph")
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() {
		return binPath, true
	}
	return "", false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// shouldRunSuiteOfSuite reports whether the meta tests that exec the gryph
// binary should run in this process. They are skipped:
//   - under -short (suite-of-suite spawns subprocesses that re-run the full
//     suite and exceed sensible short-mode budgets),
//   - when invoked recursively from inside the gryph CLI's own go test
//     invocation (envConformanceRunning),
//   - on platforms without a pre-built gryph binary.
func shouldRunSuiteOfSuite(t *testing.T) (string, bool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping suite-of-suite test under -short; run via make conformance")
	}
	if os.Getenv(envConformanceRunning) != "" {
		aarm.Skip(t, aarm.Deferred, "skipped to avoid recursive invocation from gryph aarm conformance")
	}
	if runtime.GOOS == "windows" {
		t.Skip("suite-of-suite skipped on Windows; gryph binary not built for windows in CI")
	}
	binary, ok := resolveGryphBinary(t)
	if !ok {
		aarm.Skip(t, aarm.NotImplemented, "pre-built bin/gryph not found; run `make gryph` or `make conformance` first")
	}
	return binary, true
}

// TestSuite_JSONOutputValidatesAgainstSchema and
// TestSuite_JSONOutputByteIdenticalModuloRunMetadata are meta tests on the
// conformance suite itself. They deliberately do not call aarm.Requires so
// the CLI reporter routes them through the report's "unattributed" bucket
// rather than counting them against an AARM requirement.
func TestSuite_JSONOutputValidatesAgainstSchema(t *testing.T) {
	binary, ok := shouldRunSuiteOfSuite(t)
	if !ok {
		return
	}

	out, err := exec.Command(binary, "aarm", "conformance", "--format", "json").Output()
	require.NoError(t, err, "gryph aarm conformance --format json failed")

	var report map[string]any
	require.NoError(t, json.Unmarshal(out, &report))

	for _, key := range []string{"schema_version", "aarm_spec_version", "suite_version", "gryph_commit", "ran_at", "summary", "requirements"} {
		assert.Contains(t, report, key, "report missing required key %s", key)
	}
	summary, ok := report["summary"].(map[string]any)
	require.True(t, ok, "summary must be an object")
	for _, tier := range []string{"must", "should"} {
		bucket, ok := summary[tier].(map[string]any)
		require.True(t, ok, "summary.%s must be an object", tier)
		for _, k := range []string{"passed", "failed", "skipped", "errored"} {
			assert.Contains(t, bucket, k, "summary.%s.%s must be present", tier, k)
		}
	}
	requirements, ok := report["requirements"].([]any)
	require.True(t, ok)
	for _, ri := range requirements {
		r := ri.(map[string]any)
		for _, k := range []string{"id", "title", "tests"} {
			assert.Contains(t, r, k)
		}
		id := r["id"].(string)
		assert.True(t, strings.HasPrefix(id, "R"), "requirement id must start with R: %s", id)
		tests := r["tests"].([]any)
		for _, ti := range tests {
			tr := ti.(map[string]any)
			for _, k := range []string{"name", "tier", "status"} {
				assert.Contains(t, tr, k)
			}
			if tr["status"] == "skip" {
				assert.Contains(t, tr, "skip_category", "skipped test must carry skip_category")
			}
		}
	}
}

func TestSuite_JSONOutputByteIdenticalModuloRunMetadata(t *testing.T) {
	binary, ok := shouldRunSuiteOfSuite(t)
	if !ok {
		return
	}

	run := func() []byte {
		t.Helper()
		out, err := exec.Command(binary, "aarm", "conformance", "--format", "json").Output()
		require.NoError(t, err)
		return out
	}
	a := normalizeReport(t, run())
	b := normalizeReport(t, run())
	assert.True(t, bytes.Equal(a, b), "normalized reports must be byte-identical")
}

// normalizeReport strips the run-specific fields (ran_at, duration_ms,
// gryph_commit) from every level of the report so two runs of the same
// commit produce identical bytes. It also strips per-test duration_ms.
func normalizeReport(t *testing.T, raw []byte) []byte {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	delete(m, "ran_at")
	delete(m, "duration_ms")
	delete(m, "gryph_commit")
	if reqs, ok := m["requirements"].([]any); ok {
		for _, ri := range reqs {
			r := ri.(map[string]any)
			if tests, ok := r["tests"].([]any); ok {
				for _, ti := range tests {
					tr := ti.(map[string]any)
					delete(tr, "duration_ms")
				}
			}
		}
	}
	if unattr, ok := m["unattributed"].([]any); ok {
		for _, ui := range unattr {
			if u, ok := ui.(map[string]any); ok {
				delete(u, "duration_ms")
			}
		}
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

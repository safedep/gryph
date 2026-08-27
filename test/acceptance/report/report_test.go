package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	acc "github.com/safedep/gryph/test/acceptance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCatalog(t *testing.T, yaml string) *acc.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cat, err := acc.LoadCatalog(path)
	require.NoError(t, err)
	return cat
}

func TestStatusesFromJUnit(t *testing.T) {
	xmlIn := `<testsuites><testsuite>
	  <testcase name="TestAcceptance/policy/block/dangerous-command"></testcase>
	  <testcase name="TestAcceptance/hook/ingest/event-persisted"><failure message="boom">boom</failure></testcase>
	  <testcase name="TestAcceptance/health/status-doctor/healthy"><skipped message="no network"></skipped></testcase>
	  <testcase name="TestAcceptance/policy/block"></testcase>
	</testsuite></testsuites>`

	got, err := statusesFromJUnit([]byte(xmlIn))
	require.NoError(t, err)
	assert.Equal(t, StatusPass, got["policy/block/dangerous-command"])
	assert.Equal(t, StatusFail, got["hook/ingest/event-persisted"])
	assert.Equal(t, StatusSkip, got["health/status-doctor/healthy"])
	assert.Equal(t, StatusPass, got["policy/block"]) // parent aggregate, not a catalog id
}

func TestStatusesFromJUnitIgnoresNonAcceptance(t *testing.T) {
	xmlIn := `<testsuites><testsuite>
	  <testcase name="TestCatalogIntegrity"></testcase>
	  <testcase name="TestAcceptance/export/schema/jsonl-stable"></testcase>
	</testsuite></testsuites>`

	got, err := statusesFromJUnit([]byte(xmlIn))
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, StatusPass, got["export/schema/jsonl-stable"])
}

func TestSummarizeSplitsGapAndUnknown(t *testing.T) {
	cat := writeCatalog(t, `
- id: hook/ingest/event-persisted
  category: hook
  tier: P0
  guarantee: x
- id: policy/block/dangerous-command
  category: policy
  tier: P0
  guarantee: y
- id: export/schema/jsonl-stable
  category: export
  tier: P1
  guarantee: z
`)

	// hook has a result. policy has a script but no result. export has neither.
	scripts := map[string]bool{
		"hook/ingest/event-persisted":    true,
		"policy/block/dangerous-command": true,
	}
	sum := summarize(cat, map[string]Status{"hook/ingest/event-persisted": StatusPass}, scripts)
	assert.Equal(t, 1, sum.Counts[StatusPass])
	assert.Equal(t, 1, sum.Counts[StatusUnknown])
	assert.Equal(t, 1, sum.Counts[StatusGap])
	assert.Len(t, sum.Rows, 3)
}

func TestRenderShowsFailureSnippetAndCoverage(t *testing.T) {
	cat := writeCatalog(t, `
- id: policy/block/dangerous-command
  category: policy
  tier: P0
  guarantee: a matching block rule stops the action
- id: export/schema/jsonl-stable
  category: export
  tier: P1
  guarantee: export emits schema-tagged JSONL
`)

	results := map[string]result{
		"policy/block/dangerous-command": {status: StatusPass},
		"export/schema/jsonl-stable":     {status: StatusFail, message: "unexpected command success"},
	}
	statuses := map[string]Status{}
	for id, r := range results {
		statuses[id] = r.status
	}

	var b strings.Builder
	require.NoError(t, render(&b, summarize(cat, statuses, nil), results))
	out := b.String()

	assert.Contains(t, out, "policy/block/dangerous-command")
	assert.Contains(t, out, "unexpected command success")
	assert.Contains(t, out, "1/1") // P0 coverage: 1 pass of 1
	assert.Contains(t, out, "## policy")
	assert.Contains(t, out, "[FAIL]")
}

func TestParseJUnitRejectsMalformed(t *testing.T) {
	_, err := statusesFromJUnit([]byte("<testsuites><not-closed"))
	assert.Error(t, err)
}

func TestSnippetPrefersAssertionLineOverTestLogNoise(t *testing.T) {
	body := `=== RUN   TestAcceptance/policy/block/dangerous-command
=== PAUSE TestAcceptance/policy/block/dangerous-command
    testscript.go:584: WORK=$WORK
        PATH=/usr/bin
        HOME=/no-home
        > execexit 2 gryph _hook claude-code PreToolUse
        FAIL: scripts/policy/block/dangerous-command.txtar:9: unexpected command success`
	got := snippet(&xmlNode{Message: "=== RUN   TestAcceptance/policy/block/dangerous-command", Body: body})
	assert.Equal(t, "FAIL: scripts/policy/block/dangerous-command.txtar:9: unexpected command success", got)
}

func TestSnippetSuppressesRunHeaderOnlyBody(t *testing.T) {
	got := snippet(&xmlNode{Message: "=== RUN   TestAcceptance/health/status-doctor/healthy", Body: "=== RUN   TestAcceptance/health/status-doctor/healthy\n"})
	assert.Equal(t, "", got)
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSources_DefaultIsCompact(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(policyPath, []byte("version: \"1\"\nrules: []\n"), 0o644))

	var buf bytes.Buffer
	renderSources(&buf, tui.NewColorizer(false), []loader.Source{
		loader.NewFileSource(policyPath),
		loader.NewOptionalFileSource(filepath.Join(dir, "missing.yaml")),
	}, false)

	out := buf.String()
	assert.Contains(t, out, "Policy sources\n")
	assert.Contains(t, out, "1  file")
	assert.Contains(t, out, policyPath)
	assert.Contains(t, out, "found")
	assert.Contains(t, out, "2  file")
	assert.Contains(t, out, "missing")
	assert.Contains(t, out, "optional")
	assert.NotContains(t, out, "evaluated in order")
	assert.NotContains(t, out, "Loads every")
}

func TestRenderSources_VerboseIncludesHints(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	renderSources(&buf, tui.NewColorizer(false), []loader.Source{
		loader.NewOptionalDirSource(dir),
	}, true)

	out := buf.String()
	assert.Contains(t, out, "Policy sources\n")
	assert.Contains(t, out, "1  dir")
	assert.Contains(t, out, "empty")
	assert.Contains(t, out, "Loads every *.yaml or *.yml file sorted by filename")
}

func TestSourceToRow_ConventionalStatusStaysWithinBoundary(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	policyPath := filepath.Join(root, ".gryph-policy.yml")
	require.NoError(t, os.WriteFile(policyPath, []byte("version: \"1\"\nrules: []\n"), 0o644))

	src := loader.NewConventionalSource(deep)
	src.StopAt = root

	row := sourceToRow(src)
	assert.Equal(t, "conventional", row.Kind)
	assert.Equal(t, policyPath, row.Path)
	assert.True(t, row.Optional)
	assert.False(t, row.Problem)
	assert.Equal(t, sourceStatusFound, row.Status)
}

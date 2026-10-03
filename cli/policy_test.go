package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/config"
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

func TestRenderSources_VerboseIncludesBuiltinHints(t *testing.T) {
	var buf bytes.Buffer
	renderSources(&buf, tui.NewColorizer(false), []loader.Source{
		loader.NewBuiltinSource("**/.claude/settings.json"),
	}, true)

	out := buf.String()
	assert.Contains(t, out, "Policy sources\n")
	assert.Contains(t, out, "builtin")
	assert.Contains(t, out, "Built-in rules protecting")
}

func TestSourceToRow_File(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(policyPath, []byte("version: \"1\"\nrules: []\n"), 0o644))

	row := sourceToRow(loader.NewFileSource(policyPath))
	assert.Equal(t, "file", row.Kind)
	assert.Equal(t, policyPath, row.Path)
	assert.Equal(t, sourceStatusFound, row.Status)
	assert.False(t, row.Problem)
}

func TestWriteExamplePolicy_WritesAndRefuses(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "sub", "policy.yaml")

	require.NoError(t, writeExamplePolicy(target, false))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Gryph Security Policy")

	err = writeExamplePolicy(target, false)
	require.Error(t, err)

	require.NoError(t, writeExamplePolicy(target, true))
}

func TestPolicyFilePath_UsesConfigDir(t *testing.T) {
	tmp := t.TempDir()
	assert.Equal(t, filepath.Join(tmp, "policy.yaml"), config.DefaultPolicyFilePath(&config.Paths{ConfigDir: tmp}))
}

func TestResolveEditor_Precedence(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	assert.Equal(t, "vi", resolveEditor())

	t.Setenv("EDITOR", "nano")
	assert.Equal(t, "nano", resolveEditor())

	t.Setenv("VISUAL", "code -w")
	assert.Equal(t, "code -w", resolveEditor())
}

package loader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/pdp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeYAML(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestFileSource_Load(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.yaml")
	writeYAML(t, p, "version: \"1\"\nrules:\n  - id: foo\n    action: allow\n")

	docs, err := NewFileSource(p).Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Len(t, docs[0].Rules, 1)
	assert.Equal(t, "foo", docs[0].Rules[0].ID)
}

func TestFileSource_MissingRequired(t *testing.T) {
	_, err := NewFileSource(filepath.Join(t.TempDir(), "absent.yaml")).Load(context.Background())
	assert.Error(t, err)
}

func TestFileSource_MissingOptional(t *testing.T) {
	docs, err := NewOptionalFileSource(filepath.Join(t.TempDir(), "absent.yaml")).Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestDirSource_LoadSorted(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "b.yaml"), "version: \"1\"\nrules:\n  - id: b\n    action: allow\n")
	writeYAML(t, filepath.Join(dir, "a.yml"), "version: \"1\"\nrules:\n  - id: a\n    action: allow\n")
	writeYAML(t, filepath.Join(dir, "ignore.txt"), "not yaml")
	writeYAML(t, filepath.Join(dir, ".hidden.yaml"), "version: \"1\"\nrules: []\n")

	docs, err := NewDirSource(dir).Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 2)
	assert.Equal(t, "a", docs[0].Rules[0].ID)
	assert.Equal(t, "b", docs[1].Rules[0].ID)
}

func TestDirSource_MissingOptional(t *testing.T) {
	docs, err := NewOptionalDirSource(filepath.Join(t.TempDir(), "absent")).Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestConventionalSource_WalksUp(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	writeYAML(t, filepath.Join(root, ".gryph-policy.yml"), "version: \"1\"\nrules:\n  - id: from-root\n    action: allow\n")

	src := NewConventionalSource(deep)
	src.StopAt = root
	docs, err := src.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "from-root", docs[0].Rules[0].ID)
}

func TestConventionalSource_StopsAtBoundary(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a")
	require.NoError(t, os.MkdirAll(deep, 0o755))

	src := NewConventionalSource(deep)
	src.StopAt = root
	docs, err := src.Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestLoader_MergesRules(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	writeYAML(t, a, "version: \"1\"\nrules:\n  - id: r-a\n    action: allow\n")
	writeYAML(t, b, "version: \"1\"\nrules:\n  - id: r-b\n    action: block\n")

	policy, err := New(NewFileSource(a), NewFileSource(b)).Load(context.Background())
	require.NoError(t, err)
	require.Len(t, policy.Rules, 2)
	assert.Equal(t, "r-a", policy.Rules[0].ID)
	assert.Equal(t, "r-b", policy.Rules[1].ID)
}

func TestLoader_DuplicateRuleID(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	writeYAML(t, a, "version: \"1\"\nrules:\n  - id: dup\n    action: allow\n")
	writeYAML(t, b, "version: \"1\"\nrules:\n  - id: dup\n    action: block\n")

	_, err := New(NewFileSource(a), NewFileSource(b)).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate rule id")
	assert.Contains(t, err.Error(), "dup")
}

func TestLoader_DisabledFilter(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	writeYAML(t, a, "version: \"1\"\nrules:\n  - id: keep\n    action: allow\n  - id: drop\n    action: block\n")
	writeYAML(t, b, "version: \"1\"\nrules: []\ndisabled:\n  - drop\n")

	policy, err := New(NewFileSource(a), NewFileSource(b)).Load(context.Background())
	require.NoError(t, err)
	require.Len(t, policy.Rules, 1)
	assert.Equal(t, "keep", policy.Rules[0].ID)
	assert.Equal(t, []string{"drop"}, policy.Disabled)
}

func TestLoader_EmptyReturnsEmptyPolicy(t *testing.T) {
	policy, err := New().Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Empty(t, policy.Rules)
}

func TestLoader_PropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	writeYAML(t, bad, "rules:\n  - id: \"\"\n    action: allow\n")

	_, err := New(NewFileSource(bad)).Load(context.Background())
	assert.Error(t, err)
}

func TestLoader_PoliciesCompileTogether(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	writeYAML(t, a, "version: \"1\"\nrules:\n  - id: r\n    action: block\n    condition: \"action.tool == 'Bash'\"\n")

	policy, err := New(NewFileSource(a)).Load(context.Background())
	require.NoError(t, err)
	_, err = pdp.New(policy)
	require.NoError(t, err)
}

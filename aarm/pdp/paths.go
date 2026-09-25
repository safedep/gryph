package pdp

import (
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
)

// actionPaths holds the paths that the shell command of an action changes.
// It uses the mediator's parse when the action carries one, and parses the
// command once per evaluation otherwise.
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
	analysis := a.action.Shell
	if analysis == nil {
		parsed := shellcmd.AnalyzeCommand(a.action.Parameters.Command, a.action.Parameters.Args, a.action.WorkingDir)
		analysis = &parsed
	}
	a.targets = analysis.Changes()
	return a.targets
}

// holdsAgentRoot reports whether dir is the working directory, the home
// directory, or a parent of either.
func (a *actionPaths) holdsAgentRoot(dir string) bool {
	for _, root := range []string{a.action.WorkingDir, shellcmd.HomeDir()} {
		if root == "" {
			continue
		}
		root = path.Clean(filepath.ToSlash(root))
		if dir == "/" || dir == root || strings.HasPrefix(root, dir+"/") {
			return true
		}
	}
	return false
}

// matchesFiles reports whether the action path, or a path that the shell
// command changes, matches the rule's file patterns. A file delete or a
// shell removal of a directory that contains a matching path also matches.
// A tree write can make any path under its directory. So a tree write into
// the working directory, the home directory, or a parent of either also
// matches a pattern that starts with "**/". An agent loads its settings from
// the project root and from home, so a tree write elsewhere cannot plant a
// file that the agent loads.
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
		if t.Removes() && matchesAnyPath(r.containerPatterns, t.Path) {
			return true
		}
		if t.Access == shellcmd.AccessWriteTree && r.anyDirectory && paths.holdsAgentRoot(t.Path) {
			return true
		}
	}
	return false
}

// containerPatterns returns the glob of each parent directory of each
// pattern. For "**/.agent/hooks.json" it returns "**/.agent". It skips a
// parent whose last segment is a glob, because that parent can be any
// directory. An absolute pattern also has the root as a parent.
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
		if strings.HasPrefix(pattern, "/") {
			out = append(out, "/")
		}
	}
	return out
}

// matchesAnyDirectory reports whether a pattern starts with "**/". Such a
// pattern matches a path under any directory.
func matchesAnyDirectory(patterns []string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool {
		return p == "**" || strings.HasPrefix(p, "**/")
	})
}

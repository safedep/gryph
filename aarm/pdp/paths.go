package pdp

import (
	"path"
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

// matchesFiles reports whether the action path, or a path that the shell
// command changes, matches the rule's file patterns. A file delete or a
// shell removal of a directory that contains a matching path also matches.
// A tree write also matches when it writes into the directory that holds a
// matching path. A named tree write, the directory that a recursive copy
// creates, also matches an ancestor below the "**" of a relative pattern. A
// tree write into another parent does not match, because a copy or an
// extract into the project root, home or ~/.config is a common command.
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
		switch t.Access {
		case shellcmd.AccessRemove:
			if matchesAnyPath(r.containerPatterns, t.Path) {
				return true
			}
		case shellcmd.AccessWriteTree:
			if matchesAnyPath(r.treePatterns, t.Path) || (t.Named && matchesAnyPath(r.namedTreePatterns, t.Path)) {
				return true
			}
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

// namedTreePatterns returns the globs that a named tree write matches: the
// directory that holds each pattern, and each ancestor below the leading
// "**" of a relative pattern. So a copy of "dotfiles/.codeium" into home
// matches "**/.codeium/windsurf/hooks.json". An absolute pattern gives only
// its directory, so a tree write into home or "/" does not match.
func namedTreePatterns(patterns []string) []string {
	out := parentPatterns(patterns)
	for _, pattern := range patterns {
		if !path.IsAbs(pattern) {
			out = append(out, containerPatterns([]string{pattern})...)
		}
	}
	return out
}

// parentPatterns returns the glob of the directory that holds each
// pattern. For "**/.agent/hooks.json" it returns "**/.agent". It skips a
// pattern whose parent can be any directory, such as "**/.env".
func parentPatterns(patterns []string) []string {
	var out []string
	for _, pattern := range patterns {
		dir := path.Dir(pattern)
		if dir == "." || dir == "**" || strings.HasSuffix(dir, "/**") {
			continue
		}
		out = append(out, dir)
	}
	return out
}

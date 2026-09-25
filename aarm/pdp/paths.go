package pdp

import (
	"path"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
)

// actionPaths holds the paths that the shell command of an action uses. It
// uses the mediator's parse when the action carries one, and parses the
// command once per evaluation otherwise.
type actionPaths struct {
	action       *model.Action
	parsed       bool
	targets      []shellcmd.Target
	resolvedOnce bool
	path         []string
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
	a.targets = analysis.Targets
	return a.targets
}

// actionPath returns the action path and its resolved form. An agent can
// report a relative path, "~", "..", or a trailing slash, so a rule matches
// either form.
func (a *actionPaths) actionPath() []string {
	if a.resolvedOnce {
		return a.path
	}
	a.resolvedOnce = true
	raw := a.action.Parameters.Path
	if raw == "" {
		return nil
	}
	a.path = []string{raw}
	if resolved := shellcmd.ResolvePath(raw, a.action.WorkingDir); resolved != raw {
		a.path = append(a.path, resolved)
	}
	return a.path
}

// matchesFiles reports whether the action path, or a shell target with an
// access the rule selects, matches the rule's file patterns.
//
// A file delete, or a shell removal of a directory that contains a matching
// path, also matches. A shell read of such a directory also matches, because
// a recursive read or a copy of the directory reads every file in it. A read
// through a glob matches when the glob covers a matching path. A file_read
// action of the directory that directly holds a matching path matches when
// the rule selects reads.
//
// A tree write matches the directory that holds a matching path. A tree
// write into a parent of that directory does not match, because a copy or an
// extract into the project root or into home is a common command.
func (r compiledRule) matchesFiles(action *model.Action, paths *actionPaths) bool {
	for _, p := range paths.actionPath() {
		switch {
		case matchesAnyPath(r.filePatterns, p):
			return true
		case action.Type == model.ActionFileDelete && matchesAnyPath(r.containerPatterns, p):
			return true
		case action.Type == model.ActionFileRead && slices.Contains(r.fileAccess, shellcmd.AccessRead) &&
			matchesAnyPath(r.parentPatterns, p):
			return true
		}
	}
	for _, t := range paths.commandTargets() {
		if r.selects(t.Access) && r.matchesTarget(t) {
			return true
		}
	}
	return false
}

// selects reports whether the rule selects a shell target with the access. A
// tree write is a write and a removal, so a rule that selects either one
// selects it.
func (r compiledRule) selects(access shellcmd.Access) bool {
	if access == shellcmd.AccessWriteTree {
		return slices.Contains(r.fileAccess, shellcmd.AccessWrite) || slices.Contains(r.fileAccess, shellcmd.AccessRemove)
	}
	return slices.Contains(r.fileAccess, access)
}

func (r compiledRule) matchesTarget(t shellcmd.Target) bool {
	if t.Access == shellcmd.AccessRead && t.Glob != "" {
		return matchesAnyPath(r.filePatterns, t.Glob) || globCovers(t.Glob, r.filePatterns)
	}
	if matchesAnyPath(r.filePatterns, t.Path) {
		return true
	}
	switch t.Access {
	case shellcmd.AccessRemove, shellcmd.AccessRead:
		return matchesAnyPath(r.containerPatterns, t.Path)
	case shellcmd.AccessWriteTree:
		return matchesAnyPath(r.parentPatterns, t.Path)
	}
	return false
}

// globCovers reports whether a shell glob names a literal path pattern.
func globCovers(glob string, patterns []string) bool {
	for _, p := range patterns {
		if strings.ContainsAny(p, "*?[{") {
			continue
		}
		if ok, _ := doublestar.Match(glob, p); ok {
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
// pattern whose parent can be any directory, such as "**/.env", and the root.
func parentPatterns(patterns []string) []string {
	var out []string
	for _, pattern := range patterns {
		dir := path.Dir(pattern)
		if dir == "." || dir == "/" || dir == "**" || strings.HasSuffix(dir, "/**") {
			continue
		}
		out = append(out, dir)
	}
	return out
}

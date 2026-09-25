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
// through a glob matches when the glob can name a matching path. When the
// rule selects reads, a file_read action of a directory that contains a
// matching path also matches, unless the directory is the home directory or
// one of its parents.
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
			matchesAnyPath(r.containerPatterns, p) && !isHomeOrParent(p):
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

// matchesTarget matches a shell target. A guessed read matches only the
// file patterns, because the walker does not know whether the command reads
// a directory tree.
func (r compiledRule) matchesTarget(t shellcmd.Target) bool {
	if t.Access == shellcmd.AccessRead && t.Glob != "" {
		return globsOverlap(t.Glob, r.filePatterns) || (!t.Guess && globsOverlap(t.Glob, r.containerPatterns))
	}
	if matchesAnyPath(r.filePatterns, t.Path) {
		return true
	}
	switch t.Access {
	case shellcmd.AccessRemove:
		return matchesAnyPath(r.containerPatterns, t.Path)
	case shellcmd.AccessRead:
		return !t.Guess && matchesAnyPath(r.containerPatterns, t.Path)
	case shellcmd.AccessWriteTree:
		return matchesAnyPath(r.parentPatterns, t.Path)
	}
	return false
}

// isHomeOrParent reports whether p is the home directory or one of its
// parents. A search of these directories is common, and a rule that blocks
// it blocks too much.
func isHomeOrParent(p string) bool {
	p = shellcmd.ResolvePath(p, "")
	home := shellcmd.ResolvePath("~", "")
	return p == "/" || p == home || strings.HasPrefix(home, p+"/")
}

// globsOverlap reports whether a shell glob and one of the patterns can
// match the same path. It compares the paths one segment at a time, and a
// "**" segment matches any number of segments.
func globsOverlap(glob string, patterns []string) bool {
	globSegs := strings.Split(glob, "/")
	for _, p := range patterns {
		if intersects(globSegs, strings.Split(p, "/"), isDoubleStar, segmentsOverlap) {
			return true
		}
	}
	return false
}

func isDoubleStar(seg string) bool { return seg == "**" }

// segmentsOverlap reports whether two glob segments can match the same
// name. A segment with a brace can match anything, so the result is true.
func segmentsOverlap(a, b string) bool {
	if strings.Contains(a, "{") || strings.Contains(b, "{") {
		return true
	}
	return intersects(globTokens(a), globTokens(b), isStarToken, tokensMeet)
}

// globToken is one element of a glob segment: "*", "?", a class such as
// "[a-z]", or one literal character.
type globToken string

func isStarToken(t globToken) bool { return t == "*" }

func globTokens(seg string) []globToken {
	var out []globToken
	for i := 0; i < len(seg); i++ {
		switch seg[i] {
		case '\\':
			if i+1 < len(seg) {
				i++
			}
			out = append(out, globToken(`\`+seg[i:i+1]))
		case '[':
			if end := strings.IndexByte(seg[i+1:], ']'); end > 0 {
				out = append(out, globToken(seg[i:i+end+2]))
				i += end + 1
				continue
			}
			out = append(out, `\[`)
		default:
			out = append(out, globToken(seg[i:i+1]))
		}
	}
	return out
}

// tokensMeet reports whether two tokens that each match one character can
// match the same character. Two classes always meet, so the check can only
// over-match.
func tokensMeet(a, b globToken) bool {
	if a == "?" || b == "?" {
		return true
	}
	aClass, bClass := strings.HasPrefix(string(a), "["), strings.HasPrefix(string(b), "[")
	switch {
	case aClass && bClass:
		return true
	case aClass:
		ok, _ := doublestar.Match(string(a), literal(b))
		return ok
	case bClass:
		ok, _ := doublestar.Match(string(b), literal(a))
		return ok
	}
	return literal(a) == literal(b)
}

func literal(t globToken) string {
	return strings.TrimPrefix(string(t), `\`)
}

// intersects reports whether two token lists can match the same input. A
// star token matches any run of input, and meet reports whether two other
// tokens can match the same element.
func intersects[T any](a, b []T, star func(T) bool, meet func(T, T) bool) bool {
	seen := map[[2]int]bool{}
	var visit func(i, j int) bool
	visit = func(i, j int) bool {
		if i == len(a) && j == len(b) {
			return true
		}
		if seen[[2]int{i, j}] {
			return false
		}
		seen[[2]int{i, j}] = true
		aStar := i < len(a) && star(a[i])
		bStar := j < len(b) && star(b[j])
		switch {
		case aStar && (visit(i+1, j) || (j < len(b) && visit(i, j+1))):
			return true
		case bStar && (visit(i, j+1) || (i < len(a) && visit(i+1, j))):
			return true
		case i < len(a) && j < len(b) && !aStar && !bStar:
			return meet(a[i], b[j]) && visit(i+1, j+1)
		}
		return false
	}
	return visit(0, 0)
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

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
	shell        *shellcmd.Analysis
	resolvedOnce bool
	path         []string
}

// shellAnalysis returns the analysis of the shell command, or nil when the
// action is not a command.
func (a *actionPaths) shellAnalysis() *shellcmd.Analysis {
	if a.parsed {
		return a.shell
	}
	a.parsed = true
	if a.action.Type != model.ActionCommandExec {
		return nil
	}
	a.shell = a.action.Shell
	if a.shell == nil {
		parsed := shellcmd.AnalyzeCommand(a.action.Parameters.Command, a.action.Parameters.Args, a.action.WorkingDir)
		a.shell = &parsed
	}
	return a.shell
}

func (a *actionPaths) commandTargets() []shellcmd.Target {
	if s := a.shellAnalysis(); s != nil {
		return s.Targets
	}
	return nil
}

func (a *actionPaths) runsGryphHook() bool {
	s := a.shellAnalysis()
	return s != nil && s.GryphHook
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
// A tree write matches the directory that holds a matching path. A named
// tree write, the directory that a recursive copy creates in home or the
// working directory, also matches an ancestor below the "**" of a relative
// pattern. A named plain write matches the same way when its parent holds a
// matching path, as in "cp -r dotfiles/devin ~/.config/". A tree write into
// another parent does not match, because a copy or an extract into the
// project root, home or ~/.config is a common command.
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

// matchesTarget matches a shell target. A guessed read or a flat read
// matches only the file patterns, because the command does not read every
// file in a directory. A read of the home directory or one of its parents,
// not through a glob, also matches only the file patterns, as for a
// file_read action.
func (r compiledRule) matchesTarget(t shellcmd.Target) bool {
	treeRead := t.Access == shellcmd.AccessRead && !t.Guess && !t.Flat && (t.Glob != "" || !isHomeOrParent(t.Path))
	if t.Access == shellcmd.AccessRead && t.Glob != "" {
		return globsOverlap(t.Glob, r.filePatterns, t.MatchDot) || (treeRead && globsOverlap(t.Glob, r.containerPatterns, t.MatchDot))
	}
	if matchesAnyPath(r.filePatterns, t.Path) {
		return true
	}
	switch t.Access {
	case shellcmd.AccessRemove:
		return matchesAnyPath(r.containerPatterns, t.Path)
	case shellcmd.AccessRead:
		return (treeRead && matchesAnyPath(r.containerPatterns, t.Path)) ||
			(t.Shallow && matchesAnyPath(r.treePatterns, t.Path))
	case shellcmd.AccessWrite:
		return t.Named && matchesAnyPath(r.containerPatterns, path.Dir(t.Path)) && matchesAnyPath(r.namedTreePatterns, t.Path)
	case shellcmd.AccessWriteTree:
		return matchesAnyPath(r.treePatterns, t.Path) || (t.Named && matchesAnyPath(r.namedTreePatterns, t.Path))
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
// "**" segment matches any number of segments. With matchDot, a leading
// wildcard of a glob segment can match a leading dot.
func globsOverlap(glob string, patterns []string, matchDot bool) bool {
	globSegs := strings.Split(glob, "/")
	meet := func(g, p string) bool { return segmentsOverlap(g, p, matchDot) }
	for _, p := range patterns {
		if intersects(globSegs, strings.Split(p, "/"), isDoubleStar, meet) {
			return true
		}
	}
	return false
}

func isDoubleStar(seg string) bool { return seg == "**" }

// segmentsOverlap reports whether a shell glob segment and a pattern
// segment can match the same name. A segment with a brace can match
// anything, so the result is true. The shell does not let a leading "*",
// "?", or class match a leading dot, so such a glob segment does not meet a
// pattern segment that starts with a literal dot, unless matchDot is set.
func segmentsOverlap(glob, pattern string, matchDot bool) bool {
	if strings.Contains(glob, "{") || strings.Contains(pattern, "{") {
		return true
	}
	g, p := globTokens(glob), globTokens(pattern)
	if !matchDot && len(g) > 0 && len(p) > 0 && isWildToken(g[0]) && !isWildToken(p[0]) && literal(p[0]) == "." {
		return false
	}
	return intersects(g, p, isStarToken, tokensMeet)
}

// globToken is one element of a glob segment: "*", "?", a class such as
// "[a-z]", or one literal character.
type globToken string

func isStarToken(t globToken) bool { return t == "*" }

func isWildToken(t globToken) bool { return t == "*" || t == "?" || isClassToken(t) }

func isClassToken(t globToken) bool { return strings.HasPrefix(string(t), "[") }

// globTokens splits a glob segment into tokens. A class that does not close
// makes the rest of the segment a star token, so that the check can only
// over-match.
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
			end := classEnd(seg, i)
			if end < 0 {
				return append(out, "*")
			}
			out = append(out, globToken(seg[i:end+1]))
			i = end
		default:
			out = append(out, globToken(seg[i:i+1]))
		}
	}
	return out
}

// classEnd returns the index of the "]" that closes the class that starts
// at seg[start], or -1. A "]" right after "[", "[!", or "[^" is a member.
// A "]" inside "[:name:]", "[=c=]", or "[.c.]" does not close the class.
func classEnd(seg string, start int) int {
	i := start + 1
	if i < len(seg) && (seg[i] == '!' || seg[i] == '^') {
		i++
	}
	if i < len(seg) && seg[i] == ']' {
		i++
	}
	for i < len(seg) {
		switch {
		case seg[i] == '\\':
			i += 2
		case seg[i] == '[' && i+1 < len(seg) && strings.IndexByte(":=.", seg[i+1]) >= 0:
			end := strings.Index(seg[i+2:], string(seg[i+1])+"]")
			if end < 0 {
				return -1
			}
			i += end + 4
		case seg[i] == ']':
			return i
		default:
			i++
		}
	}
	return -1
}

// tokensMeet reports whether two tokens that each match one character can
// match the same character. Two classes always meet, and so does a class
// that doublestar cannot check exactly, so the check can only over-match.
func tokensMeet(a, b globToken) bool {
	if a == "?" || b == "?" {
		return true
	}
	switch {
	case isClassToken(a) && isClassToken(b):
		return true
	case isClassToken(a):
		return classMatches(a, literal(b))
	case isClassToken(b):
		return classMatches(b, literal(a))
	}
	return literal(a) == literal(b)
}

func classMatches(class globToken, char string) bool {
	body := strings.TrimLeft(string(class[1:len(class)-1]), "!^")
	if body == "" || body[0] == ']' || strings.ContainsAny(body, `[\`) {
		return true
	}
	ok, err := doublestar.Match(string(class), char)
	return ok || err != nil
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

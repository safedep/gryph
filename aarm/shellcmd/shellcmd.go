// Package shellcmd finds the paths that a shell command writes, moves, or
// removes. The policy engine matches these paths against file patterns, so
// one path rule covers both a direct file write and a shell command.
//
// The analysis is best effort. It does not run the command, so it cannot
// resolve unknown variables, command substitutions, or encoded payloads.
package shellcmd

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Target is a path that a command changes.
type Target struct {
	// Path is the target path with forward slashes. It is absolute when the
	// command or the working directory gives enough information.
	Path string

	// Remove is true when the command deletes, moves, or changes the
	// permissions of the path. A removal of a directory also removes every
	// path in it.
	Remove bool
}

// Env holds the values used to resolve paths.
type Env struct {
	WorkingDir string
	Home       string
}

// Targets returns the paths that command changes. When the parser rejects
// the command, every word is returned as a removal target so that the caller
// fails closed.
func Targets(command string, env Env) []Target {
	w := &walker{env: env, cwd: env.WorkingDir}
	if err := w.script(command, 0); err != nil {
		return fallbackTargets(command, env)
	}
	return w.targets
}

const maxNesting = 4

type walker struct {
	env     Env
	cwd     string
	targets []Target
}

func (w *walker) script(src string, depth int) error {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return err
	}
	syntax.Walk(f, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Stmt:
			w.redirects(n.Redirs)
		case *syntax.CallExpr:
			w.call(w.words(n.Args), depth)
		}
		return true
	})
	return nil
}

func (w *walker) redirects(redirs []*syntax.Redirect) {
	for _, r := range redirs {
		switch r.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob,
			syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
			if v, ok := w.word(r.Word); ok {
				w.add(v, false)
			}
		}
	}
}

// words resolves each argument. An argument that cannot be resolved becomes
// an empty string so that argument positions stay stable.
func (w *walker) words(args []*syntax.Word) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		v, _ := w.word(a)
		out = append(out, v)
	}
	return out
}

func (w *walker) call(args []string, depth int) {
	if len(args) == 0 || args[0] == "" {
		return
	}
	name := path.Base(args[0])
	rest := args[1:]

	switch name {
	case "cd":
		if ops := operands(rest); len(ops) > 0 {
			w.cwd = w.resolve(ops[0])
		}
	case "sudo", "doas", "env", "nohup", "command", "exec", "time", "nice", "ionice", "stdbuf", "timeout":
		w.call(skipWrapperArgs(name, rest), depth)
	case "sh", "bash", "zsh", "dash", "ksh":
		for i, a := range rest {
			if a == "-c" && i+1 < len(rest) && depth < maxNesting {
				if err := w.script(rest[i+1], depth+1); err != nil {
					w.targets = append(w.targets, fallbackTargets(rest[i+1], w.env)...)
				}
				return
			}
		}
	case "eval":
		if depth < maxNesting {
			src := strings.Join(rest, " ")
			if err := w.script(src, depth+1); err != nil {
				w.targets = append(w.targets, fallbackTargets(src, w.env)...)
			}
		}
	case "rm", "unlink", "rmdir", "shred":
		w.addAll(operands(rest), true)
	case "mv":
		dest, sources := copyOperands(rest)
		w.addAll(sources, true)
		w.addDest(dest, sources)
	case "cp", "install", "ln", "rsync":
		w.addDest(copyOperands(rest))
	case "tee", "truncate":
		w.addAll(operands(rest), false)
	case "chmod", "chown", "chgrp":
		if ops := operands(rest); len(ops) > 1 {
			w.addAll(ops[1:], true)
		}
	case "sed", "perl":
		if hasInPlaceFlag(rest) {
			w.addAll(operands(rest), false)
		}
	case "dd":
		for _, a := range rest {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				w.add(v, false)
			}
		}
	case "find":
		if findChanges(rest) {
			w.addAll(findRoots(rest), true)
		}
	}
}

// copyOperands splits the operands of a copy or move into the destination
// and the sources. The destination is the value of -t or the last operand.
func copyOperands(args []string) (string, []string) {
	ops := operands(args)
	for i, a := range args {
		dest := ""
		if (a == "-t" || a == "--target-directory") && i+1 < len(args) {
			dest = args[i+1]
		} else if v, ok := strings.CutPrefix(a, "--target-directory="); ok {
			dest = v
		}
		if dest != "" {
			if j := slices.Index(ops, dest); j >= 0 {
				ops = slices.Delete(slices.Clone(ops), j, j+1)
			}
			return dest, ops
		}
	}
	if len(ops) < 2 {
		return "", nil
	}
	return ops[len(ops)-1], ops[:len(ops)-1]
}

// addDest adds the destination of a copy or move as a write. When the
// destination is a directory, the command writes each source name into it,
// so that path is added too.
func (w *walker) addDest(dest string, sources []string) {
	if dest == "" {
		return
	}
	w.add(dest, false)
	for _, src := range sources {
		if src != "" {
			w.add(strings.TrimSuffix(dest, "/")+"/"+path.Base(src), false)
		}
	}
}

func (w *walker) addAll(values []string, remove bool) {
	for _, v := range values {
		w.add(v, remove)
	}
}

// add records a target. A path with glob characters is reduced to the
// directory before the first glob and recorded as a removal, because the
// glob can select any path in that directory.
func (w *walker) add(value string, remove bool) {
	if value == "" {
		return
	}
	if i := strings.IndexAny(value, "*?["); i >= 0 {
		value = path.Dir(value[:i] + "x")
		remove = true
	}
	w.targets = append(w.targets, Target{Path: w.resolve(value), Remove: remove})
}

func (w *walker) resolve(p string) string {
	p = expandHome(p, w.env.Home)
	if !path.IsAbs(p) && w.cwd != "" {
		p = path.Join(w.cwd, p)
	}
	return path.Clean(p)
}

func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return home + "/" + rest
	}
	return p
}

// word returns the value of a shell word. It resolves literals, quotes,
// $HOME, and $PWD. It returns false for any other expansion.
func (w *walker) word(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range word.Parts {
		if !w.wordPart(&b, part) {
			return "", false
		}
	}
	return b.String(), true
}

func (w *walker) wordPart(b *strings.Builder, part syntax.WordPart) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		b.WriteString(p.Value)
	case *syntax.SglQuoted:
		b.WriteString(p.Value)
	case *syntax.DblQuoted:
		for _, inner := range p.Parts {
			if !w.wordPart(b, inner) {
				return false
			}
		}
	case *syntax.ParamExp:
		if p.Param == nil || p.Length || p.Excl || p.Slice != nil || p.Repl != nil || p.Exp != nil || p.Index != nil {
			return false
		}
		switch p.Param.Value {
		case "HOME":
			b.WriteString(w.env.Home)
		case "PWD":
			b.WriteString(w.cwd)
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// operands returns the arguments that are not options. It does not know
// which options take a value, so an option value can appear as an operand.
// An extra operand can only add a target, so the error is safe.
func operands(args []string) []string {
	var out []string
	endOfOptions := false
	for _, a := range args {
		switch {
		case endOfOptions:
			out = append(out, a)
		case a == "--":
			endOfOptions = true
		case strings.HasPrefix(a, "-") && a != "-":
			continue
		default:
			out = append(out, a)
		}
	}
	return out
}

func skipWrapperArgs(name string, args []string) []string {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if name == "env" && strings.Contains(a, "=") {
			continue
		}
		if name == "timeout" && i == firstOperand(args) {
			continue
		}
		return args[i:]
	}
	return nil
}

func firstOperand(args []string) int {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			return i
		}
	}
	return -1
}

func hasInPlaceFlag(args []string) bool {
	for _, a := range args {
		if a == "--in-place" || strings.HasPrefix(a, "--in-place=") {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i") {
			return true
		}
	}
	return false
}

func findChanges(args []string) bool {
	for _, a := range args {
		switch a {
		case "-delete", "-exec", "-execdir", "-ok", "-okdir":
			return true
		}
	}
	return false
}

// findRoots returns the search roots of a find command. They are the
// arguments before the first expression.
func findRoots(args []string) []string {
	var roots []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
			break
		}
		roots = append(roots, a)
	}
	return roots
}

// fallbackTargets treats every word of an unparsable command as a removal
// target, so a parse failure blocks rather than allows.
func fallbackTargets(command string, env Env) []Target {
	w := &walker{env: env, cwd: env.WorkingDir}
	for _, field := range strings.Fields(command) {
		w.add(strings.Trim(field, `"'`), true)
	}
	return w.targets
}

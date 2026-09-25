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
	w := &walker{env: env}
	start := dirs{env.WorkingDir}
	if _, err := w.script(command, start); err != nil {
		w.fallback(command, start)
	}
	return w.targets
}

// maxDepth limits nested shells, eval, and wrapper chains.
const maxDepth = 8

// maxDirs limits the set of possible working directories. When the set grows
// past it, the unknown directory takes the place of the extra entries.
const maxDirs = 16

// dirs is the set of working directories a command can run in. The shell
// runs some commands only on a condition, such as the right side of "&&", so
// a "cd" there may or may not take effect. An empty string is an unknown
// directory.
type dirs []string

func union(a, b dirs) dirs {
	out := slices.Clone(a)
	for _, d := range b {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	if len(out) > maxDirs {
		out = append(out[:maxDirs-1], "")
	}
	return out
}

type walker struct {
	env     Env
	depth   int
	targets []Target
}

func (w *walker) script(src string, cwds dirs) (dirs, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return cwds, err
	}
	return w.stmts(f.Stmts, cwds), nil
}

// nested runs a script inside a nested shell. A parse error falls back to
// the word scan, so a bad nested script also fails closed.
func (w *walker) nested(src string, cwds dirs) {
	if w.depth >= maxDepth {
		return
	}
	w.depth++
	defer func() { w.depth-- }()
	if _, err := w.script(src, cwds); err != nil {
		w.fallback(src, cwds)
	}
}

func (w *walker) stmts(list []*syntax.Stmt, cwds dirs) dirs {
	for _, s := range list {
		cwds = w.stmt(s, cwds)
	}
	return cwds
}

func (w *walker) stmt(s *syntax.Stmt, cwds dirs) dirs {
	for _, r := range s.Redirs {
		w.subshells(r.Word, cwds)
		w.redirect(r, cwds)
	}
	after := w.command(s.Cmd, cwds)
	if s.Background || s.Coprocess {
		return cwds
	}
	return after
}

func (w *walker) command(cmd syntax.Command, cwds dirs) dirs {
	switch c := cmd.(type) {
	case nil:
		return cwds
	case *syntax.CallExpr:
		for _, a := range c.Assigns {
			w.subshells(a, cwds)
		}
		for _, a := range c.Args {
			w.subshells(a, cwds)
		}
		return w.call(w.words(c.Args, cwds), cwds)
	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.AndStmt, syntax.OrStmt:
			left := w.stmt(c.X, cwds)
			return union(left, w.stmt(c.Y, left))
		default:
			// Each side of a pipe runs in its own subshell.
			w.stmt(c.X, cwds)
			w.stmt(c.Y, cwds)
			return cwds
		}
	case *syntax.Subshell:
		w.stmts(c.Stmts, cwds)
		return cwds
	case *syntax.Block:
		return w.stmts(c.Stmts, cwds)
	default:
		return w.maybe(cmd, cwds)
	}
}

// maybe handles a compound command such as if, for, while, case, or a
// function. Each nested statement may or may not run, so the walker keeps
// the working directories from before and after each one, in source order.
func (w *walker) maybe(node syntax.Node, cwds dirs) dirs {
	syntax.Walk(node, func(n syntax.Node) bool {
		if s, ok := n.(*syntax.Stmt); ok {
			cwds = union(cwds, w.stmt(s, cwds))
			return false
		}
		return true
	})
	return cwds
}

// subshells runs the command and process substitutions in a word. Each one
// runs in a subshell, so it does not change the working directory.
func (w *walker) subshells(node syntax.Node, cwds dirs) {
	if node == nil {
		return
	}
	syntax.Walk(node, func(n syntax.Node) bool {
		if s, ok := n.(*syntax.Stmt); ok {
			w.stmt(s, cwds)
			return false
		}
		return true
	})
}

func (w *walker) redirect(r *syntax.Redirect, cwds dirs) {
	switch r.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
		if v, ok := w.word(r.Word, cwds); ok {
			w.add(v, false, cwds)
		}
	}
}

// words resolves each argument. An argument that cannot be resolved becomes
// an empty string so that argument positions stay stable.
func (w *walker) words(args []*syntax.Word, cwds dirs) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		v, _ := w.word(a, cwds)
		out = append(out, v)
	}
	return out
}

// call analyzes one simple command and returns the working directories
// after it.
func (w *walker) call(args []string, cwds dirs) dirs {
	if len(args) == 0 || args[0] == "" {
		return cwds
	}
	name := path.Base(args[0])
	rest := args[1:]

	switch name {
	case "cd", "pushd":
		return w.cd(rest, cwds)
	case "sudo", "doas", "env", "nohup", "command", "exec", "time", "nice", "ionice", "stdbuf", "timeout", "xargs":
		w.wrapper(name, rest, cwds)
	case "sh", "bash", "zsh", "dash", "ksh":
		if src, ok := shellScript(rest); ok {
			w.nested(src, cwds)
		}
	case "eval":
		w.nested(strings.Join(rest, " "), cwds)
	case "rm", "unlink", "rmdir", "shred":
		w.addAll(operands(rest), true, cwds)
	case "mv":
		dest, sources := copyOperands(rest)
		w.addAll(sources, true, cwds)
		w.addDest(dest, sources, cwds)
	case "cp", "install", "ln", "rsync":
		dest, sources := copyOperands(rest)
		w.addDest(dest, sources, cwds)
	case "tee", "truncate":
		w.addAll(operands(rest), false, cwds)
	case "chmod", "chown", "chgrp":
		if ops := operands(rest); len(ops) > 1 {
			w.addAll(ops[1:], true, cwds)
		}
	case "sed", "perl":
		if hasInPlaceFlag(rest) {
			w.addAll(operands(rest), false, cwds)
		}
	case "dd":
		for _, a := range rest {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				w.add(v, false, cwds)
			}
		}
	case "find":
		w.find(rest, cwds)
	}
	return cwds
}

func (w *walker) cd(args []string, cwds dirs) dirs {
	ops := operands(args)
	if len(ops) == 0 {
		return dirs{w.env.Home}
	}
	if ops[0] == "-" || ops[0] == "" {
		return union(cwds, dirs{""})
	}
	out := make(dirs, 0, len(cwds))
	for _, cwd := range cwds {
		out = union(out, dirs{resolve(ops[0], cwd, w.env.Home)})
	}
	return out
}

// wrapper analyzes the command that a wrapper such as sudo or env runs.
// Wrapper options can take a value, as in "sudo -u root", and the walker
// does not know every such option. So it tries each word after the wrapper
// as the start of the command. A wrong start is a word that names no known
// command, so it adds no target.
func (w *walker) wrapper(name string, args []string, cwds dirs) {
	if w.depth >= maxDepth {
		return
	}
	w.depth++
	defer func() { w.depth-- }()

	if dir, ok := wrapperChdir(name, args); ok {
		cwds = w.cd([]string{dir}, cwds)
	}
	for i, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		if strings.Contains(a, "=") && !strings.Contains(a, "/") {
			continue
		}
		w.call(args[i:], cwds)
	}
}

// wrapperChdir returns the directory from "env -C DIR" or "sudo -D DIR".
func wrapperChdir(name string, args []string) (string, bool) {
	short := map[string]string{"env": "-C", "sudo": "-D"}[name]
	for i, a := range args {
		if (a == short && short != "") || a == "--chdir" {
			if i+1 < len(args) {
				return args[i+1], true
			}
		}
		if v, ok := strings.CutPrefix(a, "--chdir="); ok {
			return v, true
		}
	}
	return "", false
}

// shellScript returns the script of "sh -c SCRIPT". The -c flag can be part
// of a flag group, as in "bash -lc". The script is the first operand.
func shellScript(args []string) (string, bool) {
	hasC := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "+o" || a == "-O" || a == "+O" || a == "--rcfile" || a == "--init-file":
			i++
		case a == "--":
			if hasC && i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		case strings.HasPrefix(a, "--"):
			continue
		case strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+"):
			if strings.HasPrefix(a, "-") && strings.Contains(a[1:], "c") {
				hasC = true
			}
		default:
			return a, hasC
		}
	}
	return "", false
}

// find records the changes of a find command. -delete removes the search
// roots. -exec and the related actions run a command on each file found, so
// the walker analyzes that command with "{}" set to any path under a root.
func (w *walker) find(args []string, cwds dirs) {
	roots := findRoots(args)
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-delete":
			w.addAll(roots, true, cwds)
		case "-exec", "-execdir", "-ok", "-okdir":
			end := i + 1
			for end < len(args) && args[end] != ";" && args[end] != "+" {
				end++
			}
			for _, root := range roots {
				under := strings.TrimSuffix(root, "/") + "/*"
				cmd := make([]string, 0, end-i)
				for _, a := range args[i+1 : end] {
					cmd = append(cmd, strings.ReplaceAll(a, "{}", under))
				}
				w.call(cmd, cwds)
			}
			i = end
		}
	}
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
func (w *walker) addDest(dest string, sources []string, cwds dirs) {
	if dest == "" {
		return
	}
	w.add(dest, false, cwds)
	for _, src := range sources {
		if src != "" {
			w.add(strings.TrimSuffix(dest, "/")+"/"+path.Base(src), false, cwds)
		}
	}
}

func (w *walker) addAll(values []string, remove bool, cwds dirs) {
	for _, v := range values {
		w.add(v, remove, cwds)
	}
}

// add records a target for each possible working directory. A path with
// glob characters is reduced to the directory before the first glob and
// recorded as a removal, because the glob can select any path in that
// directory.
func (w *walker) add(value string, remove bool, cwds dirs) {
	if value == "" {
		return
	}
	if i := strings.IndexAny(value, "*?["); i >= 0 {
		value = path.Dir(value[:i] + "x")
		remove = true
	}
	for _, cwd := range cwds {
		t := Target{Path: resolve(value, cwd, w.env.Home), Remove: remove}
		if !slices.Contains(w.targets, t) {
			w.targets = append(w.targets, t)
		}
	}
}

func resolve(p, cwd, home string) string {
	p = expandHome(p, home)
	if !path.IsAbs(p) && cwd != "" {
		p = path.Join(cwd, p)
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
// $HOME, and $PWD when the working directory is known. It returns false for
// any other expansion.
func (w *walker) word(word *syntax.Word, cwds dirs) (string, bool) {
	if word == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range word.Parts {
		if !w.wordPart(&b, part, false, cwds) {
			return "", false
		}
	}
	return b.String(), true
}

func (w *walker) wordPart(b *strings.Builder, part syntax.WordPart, quoted bool, cwds dirs) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		b.WriteString(unescape(p.Value, quoted))
	case *syntax.SglQuoted:
		b.WriteString(p.Value)
	case *syntax.DblQuoted:
		for _, inner := range p.Parts {
			if !w.wordPart(b, inner, true, cwds) {
				return false
			}
		}
	case *syntax.ParamExp:
		if p.Param == nil || p.Length || p.Excl || p.Slice != nil || p.Repl != nil || p.Exp != nil || p.Index != nil {
			return false
		}
		switch {
		case p.Param.Value == "HOME":
			b.WriteString(w.env.Home)
		case p.Param.Value == "PWD" && len(cwds) == 1 && cwds[0] != "":
			b.WriteString(cwds[0])
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// unescape removes the backslash escapes that the shell removes. Outside
// quotes a backslash escapes any character. Inside double quotes it escapes
// only $, `, ", \, and a newline.
func unescape(s string, quoted bool) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (!quoted || strings.IndexByte("$`\"\\\n", s[i+1]) >= 0) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
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

// fallback treats every word of an unparsable command as a removal target,
// so a parse failure blocks rather than allows.
func (w *walker) fallback(command string, cwds dirs) {
	for _, field := range strings.Fields(command) {
		w.add(strings.Trim(field, `"'`), true, cwds)
	}
}

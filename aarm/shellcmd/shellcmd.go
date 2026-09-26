// Package shellcmd finds the paths that a shell command reads, writes, moves,
// or removes, and the network hosts it contacts. The policy engine matches
// these paths against file patterns, so one path rule covers both a direct
// file access and a shell command.
//
// The analysis is best effort. It does not run the command, so it cannot
// resolve unknown variables, command substitutions, or encoded payloads. It
// records only the paths that it can resolve, and it does not record a broad
// target in place of a path that it cannot resolve.
package shellcmd

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/safedep/dry/log"
	"mvdan.cc/sh/v3/syntax"
)

// Access is how a command uses a path.
type Access string

const (
	// AccessRead means the command reads the path.
	AccessRead Access = "read"
	// AccessWrite means the command writes the path.
	AccessWrite Access = "write"
	// AccessRemove means the command deletes, moves, or changes the
	// permissions of the path. A removal of a directory also removes every
	// path in it.
	AccessRemove Access = "remove"
	// AccessWriteTree means the command can write paths in the directory
	// with names that the command line does not show, such as an extract
	// or a recursive copy.
	AccessWriteTree Access = "write-tree"
)

// Target is a path that a command uses.
type Target struct {
	// Path is the target path with forward slashes. It is absolute when the
	// command or the working directory gives enough information.
	Path   string
	Access Access
}

// Analysis is what a command does to paths and hosts.
type Analysis struct {
	// Parsed is false when the parser rejected the command or a script
	// nested in it, such as the script of "bash -c". The analysis then has
	// no targets and no hosts from the rejected script.
	Parsed  bool
	Targets []Target
	// Hosts are the lower-case host names the command contacts, without the
	// port.
	Hosts []string
}

// Changes returns the targets the command writes or removes.
func (a Analysis) Changes() []Target {
	var out []Target
	for _, t := range a.Targets {
		if t.Access != AccessRead {
			out = append(out, t)
		}
	}
	return out
}

// Env holds the values used to resolve paths.
type Env struct {
	WorkingDir string
	Home       string
}

// Analyze returns the paths that command uses and the hosts it contacts.
func Analyze(command string, env Env) Analysis {
	w := &walker{env: env}
	start := dirs{env.WorkingDir}
	if _, err := w.script(command, start); err != nil {
		w.failed = true
	}
	return Analysis{Parsed: !w.failed, Targets: w.targets, Hosts: w.hosts}
}

// AnalyzeCommand analyzes a command given as a command string plus split
// arguments, as agents report it. It resolves "~" with the user's home
// directory.
func AnalyzeCommand(command string, args []string, workingDir string) Analysis {
	line := Line(command, args)
	if line == "" {
		return Analysis{Parsed: true}
	}
	return Analyze(line, Env{WorkingDir: filepath.ToSlash(workingDir), Home: HomeDir()})
}

// HomeDir returns the user's home directory with forward slashes, or an
// empty string when it is not known. AnalyzeCommand resolves "~" with it.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warnf("shellcmd: resolve home directory: %v", err)
		return ""
	}
	return filepath.ToSlash(home)
}

// Line rebuilds the command line for the parser. Adapters that split argv
// into args lose the original quoting, so each argument is quoted again.
func Line(command string, args []string) string {
	var b strings.Builder
	b.WriteString(command)
	for _, a := range args {
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

// maxDepth limits nested shells and eval.
const maxDepth = 8

// maxCalls limits the simple commands that one analysis visits. A command
// such as a nested "find -exec" can make the walker visit many commands. When
// the budget runs out, the walker stops and keeps the targets it has.
const maxCalls = 4096

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
	calls   int
	targets []Target
	hosts   []string
	failed  bool
}

func (w *walker) script(src string, cwds dirs) (dirs, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return cwds, err
	}
	return w.stmts(f.Stmts, cwds), nil
}

// nested runs a script and returns the working directories after it. The
// caller decides whether the directory change applies, because eval runs in
// the current shell and "bash -c" does not. A script that the parser rejects
// adds no targets.
func (w *walker) nested(src string, cwds dirs) dirs {
	if w.depth >= maxDepth {
		return cwds
	}
	w.depth++
	defer func() { w.depth-- }()
	after, err := w.script(src, cwds)
	if err != nil {
		w.failed = true
		return cwds
	}
	return after
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
			w.add(v, AccessWrite, cwds)
		}
	case syntax.RdrIn:
		if v, ok := w.word(r.Word, cwds); ok {
			w.add(v, AccessRead, cwds)
		}
	}
	if v, ok := w.word(r.Word, cwds); ok {
		w.devSocket(v)
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
	if w.calls >= maxCalls {
		w.failed = true
		return cwds
	}
	w.calls++
	name := path.Base(args[0])
	rest := args[1:]

	if after, ok := w.delegate(name, rest, cwds); ok {
		return after
	}
	w.inlineURLs(rest)

	switch name {
	case "cd", "pushd":
		return w.cd(rest, cwds)
	case "rm", "unlink", "rmdir", "shred":
		w.addAll(operands(rest), AccessRemove, cwds)
	case "cp", "install", "mv", "ln":
		w.localCopy(copyTools[name], rest, cwds)
	case "rsync":
		w.remoteCopy(parseArgs(rest, rsyncOptions), rsyncFlags, cwds)
	case "scp":
		w.remoteCopy(parseArgs(rest, scpOptions), scpFlags, cwds)
	case "tee", "truncate":
		w.addAll(operands(rest), AccessWrite, cwds)
	case "chmod", "chown", "chgrp":
		if ops := operands(rest); len(ops) > 1 {
			w.addAll(ops[1:], AccessRemove, cwds)
		}
	case "sed", "perl":
		ops := operands(rest)
		if hasInPlaceFlag(rest) {
			w.addAll(ops, AccessWrite, cwds)
		} else if len(ops) > 1 {
			w.addAll(ops[1:], AccessRead, cwds)
		}
	case "awk", "gawk", "mawk":
		w.scriptTool(parseArgs(rest, awkOptions), []string{"-f", "--file"}, nil, cwds)
	case "dd":
		for _, a := range rest {
			if v, ok := strings.CutPrefix(a, "of="); ok {
				w.add(v, AccessWrite, cwds)
			}
			if v, ok := strings.CutPrefix(a, "if="); ok {
				w.add(v, AccessRead, cwds)
			}
		}
	case "find":
		w.find(rest, cwds)
	case "grep", "egrep", "fgrep", "rg":
		w.scriptTool(parseArgs(rest, grepOptions), []string{"-f", "--file"}, []string{"-e", "--regexp"}, cwds)
	case "jq":
		w.scriptTool(parseArgs(rest, jqOptions), []string{"-f", "--from-file"}, nil, cwds)
	case "tar":
		w.tar(rest, cwds)
	case "zip":
		w.zip(rest, cwds)
	case "7z", "7za", "7zr", "7zz":
		w.sevenZip(rest, cwds)
	case "unzip":
		w.unzip(rest, cwds)
	case "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unxz":
		w.compress(compressors[name], rest, cwds)
	case "sort":
		w.sort(parseArgs(rest, sortOptions), cwds)
	case "sqlite3", "source", ".":
		if ops := operands(rest); len(ops) > 0 {
			w.add(ops[0], AccessRead, cwds)
		}
	case "curl":
		w.curl(parseArgs(rest, curlOptions), cwds)
	case "wget":
		w.wget(parseArgs(rest, wgetOptions), cwds)
	case "ssh", "sftp", "telnet", "ftp":
		w.remoteShell(parseArgs(rest, remoteShellOptions[name]))
	case "nc", "ncat", "netcat":
		w.netcat(rest)
	case "git":
		w.git(rest)
	case "openssl":
		w.openssl(rest, cwds)
	default:
		if opts, ok := readCommands[name]; ok {
			w.addAll(parseArgs(rest, opts).operands, AccessRead, cwds)
		} else if opts, ok := editCommands[name]; ok {
			w.addAll(parseArgs(rest, opts).operands, AccessWrite, cwds)
		}
	}
	return cwds
}

// delegate analyzes a command that runs another command or a script: a
// wrapper, "command", a shell with a script, or eval. The walker analyzes the
// inner command or script, and not the words of the outer command, so a
// script that the parser rejects adds no host.
func (w *walker) delegate(name string, rest []string, cwds dirs) (dirs, bool) {
	if spec, ok := wrappers[name]; ok {
		w.wrapper(spec, rest, cwds)
		return cwds, true
	}
	switch name {
	case "command", "builtin":
		// These run a command in the current shell, so "command cd" changes
		// the working directory.
		return w.call(skipOptions(rest), cwds), true
	case "sh", "bash", "zsh", "dash", "ksh":
		if src, ok := shellScript(rest); ok {
			w.nested(src, cwds)
			return cwds, true
		}
	case "eval":
		return w.nested(strings.Join(rest, " "), cwds), true
	}
	return cwds, false
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

// wrapper analyzes the command that a wrapper such as sudo or env runs. It
// parses the wrapper arguments the way the wrapper does and calls only the
// program. So a chain of wrappers costs one call per wrapper.
func (w *walker) wrapper(spec wrapperSpec, args []string, cwds dirs) {
	start, chdir := spec.program(args)
	if chdir != "" {
		cwds = w.cd([]string{chdir}, cwds)
	}
	if start < len(args) {
		w.call(args[start:], cwds)
	}
}

// wrapperSpec describes how a wrapper parses its arguments.
type wrapperSpec struct {
	// values are the options that take a value. An option that is not in
	// the table takes no value.
	values map[string]bool
	// chdir are the options whose value is the working directory of the
	// program.
	chdir []string
	// assigns is true for a wrapper that takes NAME=value words before the
	// program.
	assigns bool
	// operands is the number of fixed operands before the program, such as
	// the duration of timeout.
	operands int
}

var wrappers = map[string]wrapperSpec{
	"sudo": {values: flagSet("-u", "--user", "-g", "--group", "-C", "--close-from", "-D", "--chdir",
		"-p", "--prompt", "-r", "--role", "-t", "--type", "-T", "--command-timeout", "-U",
		"--other-user", "--host"), chdir: []string{"-D", "--chdir"}, assigns: true},
	"doas":    {values: flagSet("-u", "-C")},
	"env":     {values: flagSet("-u", "--unset", "-C", "--chdir", "-S", "--split-string"), chdir: []string{"-C", "--chdir"}, assigns: true},
	"nohup":   {},
	"exec":    {values: flagSet("-a")},
	"time":    {values: flagSet("-o", "--output", "-f", "--format")},
	"nice":    {values: flagSet("-n", "--adjustment")},
	"ionice":  {values: flagSet("-c", "--class", "-n", "--classdata")},
	"stdbuf":  {values: flagSet("-i", "--input", "-o", "--output", "-e", "--error")},
	"timeout": {values: flagSet("-s", "--signal", "-k", "--kill-after"), operands: 1},
	"xargs": {values: flagSet("-a", "--arg-file", "-d", "--delimiter", "-E", "-I", "-L", "--max-lines",
		"-n", "--max-args", "-P", "--max-procs", "-s", "--max-chars", "--process-slot-var")},
	"chrt": {values: flagSet("-T", "--sched-runtime", "-P", "--sched-period", "-D", "--sched-deadline"),
		operands: 1},
	"taskset": {operands: 1},
}

// program returns the index of the program word in args and the working
// directory that a chdir option sets. The index is len(args) when args
// name no program. Options end at the first word that is not an option.
func (s wrapperSpec) program(args []string) (int, string) {
	chdir := ""
	skip := s.operands
	options := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case options && a == "--":
			options = false
		case options && strings.HasPrefix(a, "--"):
			name, v, hasValue := strings.Cut(a, "=")
			if !hasValue && s.values[name] && i+1 < len(args) {
				i++
				v = args[i]
			}
			if slices.Contains(s.chdir, name) {
				chdir = v
			}
		case options && strings.HasPrefix(a, "-"):
			for j := 1; j < len(a); j++ {
				flag := "-" + a[j:j+1]
				if !s.values[flag] {
					continue
				}
				v := a[j+1:]
				if v == "" && i+1 < len(args) {
					i++
					v = args[i]
				}
				if slices.Contains(s.chdir, flag) {
					chdir = v
				}
				break
			}
		case s.assigns && isAssignment(a):
			options = false
		case skip > 0:
			skip--
			options = false
		default:
			return i, chdir
		}
	}
	return len(args), chdir
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	return ok && name != "" && !strings.Contains(name, "/")
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
			w.addAll(roots, AccessRemove, cwds)
		case "-exec", "-ok":
			end := findActionEnd(args, i)
			for _, root := range roots {
				w.call(substitute(args[i+1:end], strings.TrimSuffix(root, "/")+"/*"), cwds)
			}
			i = end
		case "-execdir", "-okdir":
			end := findActionEnd(args, i)
			for _, root := range roots {
				w.execdir(root, substitute(args[i+1:end], "./*"), cwds)
			}
			i = end
		}
	}
}

// execdir analyzes the command of -execdir. find runs it from the
// directory of each file found, which can be any directory under the root.
// So a relative target becomes a removal of the root, as for -delete. An
// absolute target stays as it is.
func (w *walker) execdir(root string, cmd []string, cwds dirs) {
	sub := &walker{env: w.env, depth: w.depth, calls: w.calls}
	sub.call(cmd, dirs{""})
	w.calls = sub.calls
	for _, t := range sub.targets {
		if path.IsAbs(t.Path) {
			w.addTarget(t)
		} else if t.Access == AccessRead {
			w.add(root, AccessRead, cwds)
		} else {
			w.add(root, AccessRemove, cwds)
		}
	}
	for _, h := range sub.hosts {
		w.addHostName(h)
	}
	w.failed = w.failed || sub.failed
}

func findActionEnd(args []string, start int) int {
	end := start + 1
	for end < len(args) && args[end] != ";" && args[end] != "+" {
		end++
	}
	return end
}

func substitute(args []string, file string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, strings.ReplaceAll(a, "{}", file))
	}
	return out
}

// findRoots returns the search roots of a find command. They are the
// arguments after the leading options (-H, -L, -P, -D, -O) and before the
// first expression.
func findRoots(args []string) []string {
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "-H" || a == "-L" || a == "-P" || strings.HasPrefix(a, "-O") {
			i++
		} else if a == "-D" {
			i += 2
		} else {
			break
		}
	}
	var roots []string
	for _, a := range args[min(i, len(args)):] {
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
			break
		}
		roots = append(roots, a)
	}
	return roots
}

// skipOptions drops the leading options of a command.
func skipOptions(args []string) []string {
	for i, a := range args {
		if a == "--" {
			return args[i+1:]
		}
		if !strings.HasPrefix(a, "-") {
			return args[i:]
		}
	}
	return nil
}

// addDest adds the destination of a copy or move. When the destination is
// a directory, the command writes each source name into it, so that path is
// added as a write too. A relative copy, such as "cp --parents", writes the
// whole source path under the destination.
func (w *walker) addDest(dest string, sources []string, p parsedArgs, flags copyFlags, cwds dirs) {
	if dest == "" {
		return
	}
	w.add(dest, destAccess(p, sources, flags), cwds)
	relative := p.has(flags.relative...)
	for _, src := range sources {
		if _, remotePath, remote := splitRemote(src); remote {
			src = remotePath
		}
		if src == "" {
			continue
		}
		name := path.Base(src)
		if relative {
			name = relativeSource(expandHome(src, w.env.Home))
		}
		w.add(strings.TrimSuffix(dest, "/")+"/"+name, AccessWrite, cwds)
	}
}

// relativeSource returns the part of a source path that a relative copy
// keeps. rsync drops the part before a "/./" marker.
func relativeSource(src string) string {
	if _, after, ok := strings.Cut(src, "/./"); ok {
		return after
	}
	return src
}

func (w *walker) addAll(values []string, access Access, cwds dirs) {
	for _, v := range values {
		w.add(v, access, cwds)
	}
}

// add records a target for each possible working directory. A path with
// glob characters is reduced to the directory before the first glob,
// because the glob can select any path in that directory. A write through a
// glob is recorded as a tree write of the directory. A removal or a read
// through a glob is recorded as a removal or a read of the directory.
func (w *walker) add(value string, access Access, cwds dirs) {
	if value == "" || (value == "-" && access == AccessRead) {
		return
	}
	if i := strings.IndexAny(value, "*?["); i >= 0 {
		value = path.Dir(value[:i] + "x")
		if access == AccessWrite {
			access = AccessWriteTree
		}
	}
	for _, cwd := range cwds {
		w.addTarget(Target{Path: resolve(value, cwd, w.env.Home), Access: access})
	}
}

func (w *walker) addTarget(t Target) {
	if !slices.Contains(w.targets, t) {
		w.targets = append(w.targets, t)
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

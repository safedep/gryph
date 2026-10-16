package shellcmd

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// native turns a Windows path form into the forward-slash drive form that
// the rules use. Outside Windows it returns p as it is, because a backslash,
// /c/ and a colon are plain names there. Windows ignores a trailing dot or
// space of a name, and reads name:stream as the file name, so audit.db.
// and audit.db::$DATA name audit.db.
func (e Env) native(p string) string {
	if !e.Windows {
		return p
	}
	p = strings.ReplaceAll(p, `\`, "/")
	for _, prefix := range []string{"//?/", "//./"} {
		p = strings.TrimPrefix(p, prefix)
	}
	switch {
	case len(p) >= 3 && p[0] == '/' && isDriveLetter(p[1]) && p[2] == ':':
		p = p[1:]
	case len(p) >= 2 && p[0] == '/' && isDriveLetter(p[1]) && (len(p) == 2 || p[2] == '/'):
		p = strings.ToUpper(p[1:2]) + ":/" + strings.TrimPrefix(p[2:], "/")
	}
	start := 0
	if len(p) >= 2 && isDriveLetter(p[0]) && p[1] == ':' {
		start = 2
	}
	if i := strings.IndexByte(p[start:], ':'); i >= 0 {
		p = p[:start+i]
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.Trim(s, ".") != "" {
			segs[i] = strings.TrimRight(s, ". ")
		}
	}
	return strings.Join(segs, "/")
}

func isDriveLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// windowsScan is what a scan of a Windows command line finds.
type windowsScan struct {
	targets      []Target
	gryphHook    bool
	gryphResolve bool
}

// scanWindows reads a command line of PowerShell or cmd, which the POSIX
// parser misreads: both keep backslashes and expand variables in their own
// way. Each word in a Windows form is a path. The scan does not know what
// the command does with it, so each path is a change of a tree, or a
// removal when the line has a delete or move command, and a guessed read.
// The scan is best effort.
func scanWindows(line string, env Env) windowsScan {
	words := windowsWords(line)
	access := AccessWriteTree
	if slices.ContainsFunc(words, isWindowsRemoveCommand) {
		access = AccessRemove
	}
	var scan windowsScan
	for i, word := range words {
		for _, p := range env.windowsPaths(word) {
			scan.add(p, access)
			if child := childOperand(words[i+1:]); child != "" {
				scan.add(cleanPath(p+"/"+env.native(child)), access)
			}
		}
	}
	scan.gryphHook, scan.gryphResolve = windowsGryphCalls(windowsWords(unquote.Replace(line)))
	return scan
}

func (s *windowsScan) add(p string, access Access) {
	dir, glob := splitGlob(p)
	write := Target{Path: dir, Glob: glob, Access: access}
	read := Target{Path: dir, Glob: glob, Access: AccessRead, Guess: true, Shallow: true}
	s.targets = append(s.targets, write, read)
}

// childOperand returns the word that PowerShell joins to a folder in
// Join-Path X Y, Join-Path -Path X -ChildPath Y and X + 'Y': the next word
// that is not a parameter or a plus, when it is a relative path.
func childOperand(rest []string) string {
	for _, w := range rest {
		if w == "+" || strings.HasPrefix(w, "-") {
			continue
		}
		if windowsPathStart(w) >= 0 {
			return ""
		}
		return w
	}
	return ""
}

var windowsRemoveCommands = []string{
	"remove-item", "ri", "rm", "rmdir", "rd", "del", "erase",
	"move-item", "mi", "move", "mv", "rename-item", "rni", "ren", "rename",
}

func isWindowsRemoveCommand(word string) bool {
	return slices.Contains(windowsRemoveCommands, strings.ToLower(word))
}

// unquote splits the command that a quoted argument holds, as in
// powershell -c "gryph _hook x y", into its own words.
var unquote = strings.NewReplacer(`"`, " ", "'", " ")

// windowsGryphCalls reports a gryph call in the command of a Windows shell
// wrapper, cmd /c or powershell -Command, which the POSIX parser reads as
// an argument. The call counts as for the parser: a gryph word that a
// later _hook word follows, or one that later policy, approve or
// deferrals, and resolve words follow.
func windowsGryphCalls(words []string) (hook, resolve bool) {
	for i := range words {
		if n := shellWrapper(words[i:]); n > 0 {
			h, r := gryphCallIn(words[i+n:])
			hook, resolve = hook || h, resolve || r
		}
	}
	return hook, resolve
}

// shellWrapper returns the count of words of a shell wrapper at the start
// of words, or 0. Other flags can come before the command flag, as in
// cmd /d /s /c and powershell -NoProfile -ExecutionPolicy Bypass -Command.
func shellWrapper(words []string) int {
	if len(words) < 2 {
		return 0
	}
	program := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(words[0], `\`, "/"))), ".exe")
	for i, w := range words[1:] {
		flag := strings.ToLower(w)
		switch program {
		case "cmd":
			if flag == "/c" || flag == "/k" {
				return i + 2
			}
			if !strings.HasPrefix(flag, "/") {
				return 0
			}
		case "powershell", "pwsh":
			if len(flag) > 1 && flag[0] == '-' && strings.HasPrefix("command", flag[1:]) {
				return i + 2
			}
		default:
			return 0
		}
	}
	return 0
}

func gryphCallIn(words []string) (hook, resolve bool) {
	for i, w := range words {
		if !isGryphProgram(w) {
			continue
		}
		next := []string{"policy", "approve|deferrals", "resolve"}
		for _, arg := range words[i+1:] {
			if arg == "_hook" {
				hook = true
			}
			if len(next) > 0 && slices.Contains(strings.Split(next[0], "|"), arg) {
				next = next[1:]
			}
		}
		resolve = resolve || len(next) == 0
	}
	return hook, resolve
}

// windowsWords splits a command line into words the way most Windows
// shells do: a quote groups a word and goes, and the cmd escape ^ and the
// PowerShell escape ` go. A backslash stays, as a path separator.
func windowsWords(line string) []string {
	var words []string
	var b strings.Builder
	var quote rune
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case r == '^' || r == '`':
		case quote == 0 && strings.ContainsRune(" \t\r\n;|&<>(),", r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return words
}

// windowsVarRef matches a variable of cmd (%NAME% and %NAME:~0,5%), of
// PowerShell ($env:NAME, ${env:NAME}) and of a POSIX shell ($NAME, ${NAME}).
var windowsVarRef = regexp.MustCompile(`%([A-Za-z_]\w*)(?::[^%]*)?%|\$\{(?i:env:)?([A-Za-z_]\w*)\}|\$(?i:env:)?([A-Za-z_]\w*)`)

// windowsPaths returns the absolute paths that a word names in a Windows
// form. The path can start inside the word, as in --file=C:\x. A variable
// that the analysis does not know can be empty or hold any name, so such a
// word gives two paths: one without the variable, and a glob with * in
// its place.
func (e Env) windowsPaths(word string) []string {
	i := windowsPathStart(word)
	if i < 0 {
		return nil
	}
	plain, glob, unknown := e.expandWindowsVars(word[i:])
	variants := []string{plain}
	if unknown {
		variants = append(variants, glob)
	}
	var out []string
	for _, v := range variants {
		if p := e.native(v); IsAbs(p) {
			out = append(out, cleanPath(p))
		}
	}
	return out
}

func (e Env) expandWindowsVars(word string) (plain, glob string, unknown bool) {
	var p, g strings.Builder
	last := 0
	for _, m := range windowsVarRef.FindAllStringSubmatchIndex(word, -1) {
		p.WriteString(word[last:m[0]])
		g.WriteString(word[last:m[0]])
		last = m[1]
		name := ""
		for k := 2; k < len(m); k += 2 {
			if m[k] >= 0 {
				name = word[m[k]:m[k+1]]
			}
		}
		if v, ok := e.lookup(name); ok {
			p.WriteString(v)
			g.WriteString(v)
			continue
		}
		unknown = true
		g.WriteString("*")
	}
	p.WriteString(word[last:])
	g.WriteString(word[last:])
	return p.String(), g.String(), unknown
}

// lookup returns the value of a known path variable. Windows reads a
// variable name in any case. HOME is the home directory, the value that
// PowerShell gives $HOME.
func (e Env) lookup(name string) (string, bool) {
	name = strings.ToUpper(name)
	if name == "HOME" {
		return e.Home, e.Home != ""
	}
	v, ok := e.Vars[name]
	return v, ok
}

// windowsPathStart returns the index in word where a path in a Windows
// form starts, or -1. A letter, a colon and two slashes is the scheme of a
// URL, such as the s:// of https://, not a drive.
func windowsPathStart(word string) int {
	lastBackslash := strings.LastIndexByte(word, '\\')
	for i := 0; i < len(word); i++ {
		switch {
		case word[i] == '%', strings.HasPrefix(word[i:], `\\`),
			hasPrefixFold(word[i:], "$env:"), hasPrefixFold(word[i:], "${env:"),
			word[i] == '$' && i < lastBackslash:
			return i
		case isDriveLetter(word[i]) && len(word) > i+2 && word[i+1] == ':' &&
			(word[i+2] == '\\' || word[i+2] == '/' && !strings.HasPrefix(word[i+2:], "//")):
			return i
		}
	}
	return -1
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

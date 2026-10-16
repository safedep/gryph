package shellcmd

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// windowsPathVars are the environment variables that name the folders of
// the Gryph files and the agent configs on Windows. The analysis expands
// them in every shell syntax of the platform.
var windowsPathVars = []string{"USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA"}

// windowsEnv returns the variables of windowsPathVars that the process
// has, with forward slashes, keyed in upper case.
func windowsEnv() map[string]string {
	vars := map[string]string{}
	for _, name := range windowsPathVars {
		if v := os.Getenv(name); v != "" {
			vars[name] = filepath.ToSlash(v)
		}
	}
	return vars
}

// native turns a Windows path form into the forward-slash drive form that
// the rules use: backslashes become slashes, the \\?\ and \\.\ prefixes go,
// the Git Bash form /c/Users becomes C:/Users, and the path of a file URL,
// /C:/Users, becomes C:/Users. Outside Windows it returns p as it is,
// because a backslash and /c/ are plain names there.
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
	return p
}

func isDriveLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// windowsVarRef matches a variable reference of cmd (%NAME%), PowerShell
// ($env:NAME, ${env:NAME}) and a POSIX shell ($NAME, ${NAME}).
var windowsVarRef = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%|\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}|\$(?:env:)?([A-Za-z_][A-Za-z0-9_]*)`)

// windowsTokenBreak splits a command line into words without the quoting or
// the escapes of any one shell. A backslash is a path separator in cmd and
// PowerShell, so the split keeps it.
func windowsTokenBreak(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', ';', '|', '&', '(', ')', '<', '>', ',':
		return true
	}
	return false
}

// windowsTargets finds the absolute paths that a command line names in the
// form of a Windows shell. The POSIX parser reads Git Bash. A PowerShell or
// cmd command keeps backslashes and expands variables in its own way, so
// the parser misses its paths. This scan reads each word in a Windows form
// as a path: a word with a backslash, a drive, %NAME% or $env:NAME. A word
// in POSIX form, such as ~/x or $HOME/x, is the parser's. The scan does not
// know what the command does with a path, so each path is a change of a
// tree and a guessed read: a rule matches when the word names a protected
// file, or the directory that holds one. The scan is best effort.
func windowsTargets(command string, env Env) []Target {
	var out []Target
	for _, word := range strings.FieldsFunc(command, windowsTokenBreak) {
		p, ok := env.windowsPath(word)
		if !ok {
			continue
		}
		t := Target{Path: p}
		if i := strings.IndexAny(p, "*?["); i >= 0 {
			t.Glob = p
			t.Path = path.Dir(p[:i] + "x")
		}
		write, read := t, t
		write.Access = AccessWriteTree
		read.Access, read.Guess, read.Shallow = AccessRead, true, true
		out = append(out, write, read)
	}
	return out
}

// windowsPath returns the absolute path that a word names in a Windows
// form, and false when it names none after the expansion of the known
// variables. The path can start inside the word, as in --file=C:\x,
// -Path:%APPDATA%\x or -fC:\x.
func (e Env) windowsPath(word string) (string, bool) {
	i := windowsPathStart(word)
	if i < 0 {
		return "", false
	}
	word = word[i:]
	known := true
	word = windowsVarRef.ReplaceAllStringFunc(word, func(ref string) string {
		m := windowsVarRef.FindStringSubmatch(ref)
		if v, ok := e.lookup(m[1] + m[2] + m[3]); ok {
			return v
		}
		known = false
		return ref
	})
	if !known {
		return "", false
	}
	p := e.native(word)
	if !IsAbs(p) {
		return "", false
	}
	return cleanPath(p), true
}

// lookup returns the value of a known path variable. Windows reads a
// variable name in any case. HOME falls back to the home directory, the
// value that PowerShell gives $HOME.
func (e Env) lookup(name string) (string, bool) {
	name = strings.ToUpper(name)
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if name == "HOME" && e.Home != "" {
		return e.Home, true
	}
	return "", false
}

// windowsPathStart returns the index in word where a path in a Windows
// form starts: a drive (C:\ or C:/), a UNC or device prefix (\\), a cmd
// variable (%NAME%), a PowerShell variable ($env:NAME), or a variable that
// a backslash follows ($HOME\x). A letter, a colon
// and two slashes is the scheme of a URL, such as the s:// of https://, not
// a drive. It returns -1 when the word has none.
func windowsPathStart(word string) int {
	lower := strings.ToLower(word)
	for i := 0; i < len(word); i++ {
		switch {
		case word[i] == '%', strings.HasPrefix(word[i:], `\\`),
			strings.HasPrefix(lower[i:], "$env:"), strings.HasPrefix(lower[i:], "${env:"),
			word[i] == '$' && strings.ContainsRune(word[i:], '\\'):
			return i
		case isDriveLetter(word[i]) && len(word) > i+2 && word[i+1] == ':' &&
			(word[i+2] == '\\' || word[i+2] == '/' && !strings.HasPrefix(word[i+2:], "//")):
			return i
		}
	}
	return -1
}

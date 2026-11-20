package utils

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// GryphCommandPlaceholder marks the program in the plugin templates that
// RenderPlugin fills in.
const GryphCommandPlaceholder = "__GRYPH_COMMAND__"

// programName is the bare program name that a hook command used before
// hooks named the absolute path, and the fallback when the path of the
// running binary cannot be read.
const programName = "gryph"

// executablePath is overridable in tests.
var executablePath = resolveExecutable

var (
	commandOnce sync.Once
	command     string
)

// GryphCommand returns the program that a hook command names: an absolute
// path of the running binary that stays valid after a package upgrade. A
// hook that names the path does not depend on PATH, which a same-user
// process can change. The result is the bare name gryph when the path cannot
// be read, or when the binary runs under another name, for example a test
// binary.
func GryphCommand() string {
	commandOnce.Do(func() {
		command = programName
		if path, err := executablePath(); err == nil && isGryphProgram(path) {
			command = path
		}
	})
	return command
}

func resolveExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return stablePath(real, invokedPath()), nil
}

// stablePath returns a path of the binary at real that a package upgrade
// does not remove. A package manager keeps each version in its own
// directory, points a fixed link at the current version, and deletes the old
// directory on upgrade. A hook that names the versioned file then fails to
// start, and the agent runs the action with no Gryph hook. So the result is
// a link that resolves to the same file and has the same name: the Homebrew
// opt link of the formula, then the path that started this process. With
// neither, the result is real.
func stablePath(real, invoked string) string {
	for _, candidate := range []string{homebrewOptPath(real), invoked} {
		if candidate == "" || candidate == real || filepath.Base(candidate) != filepath.Base(real) {
			continue
		}
		if sameFile(candidate, real) {
			return candidate
		}
	}
	return real
}

// homebrewOptPath returns <prefix>/opt/gryph/bin/<name> for a binary at
// <prefix>/Cellar/gryph/<version>/bin/<name>, and "" for any other path.
func homebrewOptPath(real string) string {
	bin := filepath.Dir(real)
	formula := filepath.Dir(filepath.Dir(bin))
	cellar := filepath.Dir(formula)
	if filepath.Base(bin) != "bin" || filepath.Base(formula) != programName || filepath.Base(cellar) != "Cellar" {
		return ""
	}
	return filepath.Join(filepath.Dir(cellar), "opt", programName, "bin", filepath.Base(real))
}

// invokedPath returns the absolute path that started this process, before
// the resolution of symbolic links, or "" when it cannot be read.
func invokedPath() string {
	if len(os.Args) == 0 || os.Args[0] == "" {
		return ""
	}
	path := os.Args[0]
	if !strings.ContainsAny(path, `/\`) {
		found, err := exec.LookPath(path)
		if err != nil {
			return ""
		}
		path = found
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return abs
}

func sameFile(a, b string) bool {
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

// MissingProgram returns the program of a hook command when the command
// names it by an absolute path and no file is there. A package upgrade can
// remove the file that an older install named. The agent then cannot start
// the hook and runs the action with no Gryph hook. A bare program name
// resolves through the PATH of the agent, which Gryph cannot read, so it
// never counts as missing.
func MissingProgram(cmd string) (string, bool) {
	program, _, ok := splitCommand(cmd)
	if !ok || !filepath.IsAbs(program) {
		return "", false
	}
	if _, err := os.Stat(program); !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	return program, true
}

// HookCommand returns the hook command for agentName and hookType. program
// is the binary to name. An empty program names the running binary.
func HookCommand(program, agentName, hookType string) string {
	if program == "" {
		program = GryphCommand()
	}
	return quoteProgram(program) + " _hook " + agentName + " " + hookType
}

// IsHookCommand reports whether cmd runs the Gryph hook entry point for
// agentName and hookType, whatever program path it names. A command that
// an older install wrote with the bare name matches too.
func IsHookCommand(cmd, agentName, hookType string) bool {
	program, args, ok := splitCommand(cmd)
	if !ok || !isGryphProgram(program) {
		return false
	}
	return len(args) == 3 && args[0] == "_hook" && args[1] == agentName && args[2] == hookType
}

// IsGryphCommand reports whether cmd runs the Gryph hook entry point for
// any agent and hook type.
func IsGryphCommand(cmd string) bool {
	program, args, ok := splitCommand(cmd)
	if !ok || !isGryphProgram(program) {
		return false
	}
	return len(args) == 3 && args[0] == "_hook"
}

// RenderPlugin fills the program into a JavaScript or TypeScript plugin
// template, inside a string literal, so a path with backslashes or quotes
// stays one string.
func RenderPlugin(template []byte, program string) []byte {
	if program == "" {
		program = GryphCommand()
	}
	quoted, err := json.Marshal(program)
	if err != nil {
		quoted = []byte(`"` + programName + `"`)
	}
	return []byte(strings.ReplaceAll(string(template), GryphCommandPlaceholder, string(quoted[1:len(quoted)-1])))
}

// quoteProgram wraps a program path in double quotes when a shell would
// split it on a space.
func quoteProgram(program string) string {
	if strings.ContainsAny(program, " \t") {
		return `"` + program + `"`
	}
	return program
}

// splitCommand returns the program of a hook command, with its quotes
// removed, and the words after it.
func splitCommand(cmd string) (program string, args []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		end := strings.Index(cmd[1:], `"`)
		if end < 0 {
			return "", nil, false
		}
		program = cmd[1 : end+1]
		args = strings.Fields(cmd[end+2:])
		return program, args, program != ""
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

// isGryphProgram reports whether program is the gryph binary by name, on
// any path and with the Windows suffix. A name that only starts with gryph,
// such as gryphon, is another program.
func isGryphProgram(program string) bool {
	base := program
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return base == programName || base == programName+".exe"
}

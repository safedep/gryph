package utils

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// GryphCommandPlaceholder marks the program in the plugin templates that
// RenderPlugin fills in.
const GryphCommandPlaceholder = "__GRYPH_COMMAND__"

// programName is the bare program name that a hook command used before
// hooks named the absolute path, and the fallback when the path of the
// running binary cannot be read.
const programName = "gryph"

// executablePath returns the path of the running binary with symbolic
// links resolved. Tests override it.
var executablePath = resolveExecutable

var (
	programOnce  sync.Once
	command      string
	programFiles []string
)

// GryphCommand returns the program that a hook command names: an absolute
// path of the running binary that stays valid after a package upgrade. A
// hook that names the path does not depend on PATH, which a same-user
// process can change. The result is the bare name gryph when the path cannot
// be read, or when the binary runs under another name, for example a test
// binary.
func GryphCommand() string {
	loadProgram()
	return command
}

// ProgramFiles returns the absolute paths that decide which program the
// hook entries run: the path that a hook entry names, the file that it
// resolves to, and the Homebrew opt link on the way. Self-protection covers
// each one. A write to the file replaces the program, and a change of the
// link points every hook entry at another program.
func ProgramFiles() []string {
	loadProgram()
	return programFiles
}

func loadProgram() {
	programOnce.Do(func() {
		command = programName
		programFiles = nil
		resolved, err := executablePath()
		if err != nil || !isGryphProgram(resolved) {
			return
		}
		command = stablePath(resolved)
		programFiles = []string{command}
		if command != resolved {
			programFiles = append(programFiles, resolved, filepath.Dir(filepath.Dir(command)))
		}
	})
}

func resolveExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// stablePath returns the Homebrew opt link of the formula when it resolves
// to the binary at resolved, and resolved otherwise. Homebrew keeps each
// version under Cellar and deletes the old version on upgrade. A hook entry
// that names the versioned file then fails to start, and the agent runs the
// action with no Gryph hook. The opt link stays across upgrades. Homebrew
// owns it with the Cellar, so it is not easier to change than the file.
func stablePath(resolved string) string {
	if opt := homebrewOptPath(resolved); opt != "" && sameFile(opt, resolved) {
		return opt
	}
	return resolved
}

// homebrewOptPath returns <prefix>/opt/gryph/bin/<name> for a binary at
// <prefix>/Cellar/gryph/<version>/bin/<name>, and "" for any other path.
func homebrewOptPath(resolved string) string {
	bin := filepath.Dir(resolved)
	formula := filepath.Dir(filepath.Dir(bin))
	cellar := filepath.Dir(formula)
	if filepath.Base(bin) != "bin" || filepath.Base(formula) != programName || filepath.Base(cellar) != "Cellar" {
		return ""
	}
	return filepath.Join(filepath.Dir(cellar), "opt", programName, "bin", filepath.Base(resolved))
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
// names it by an absolute path and no program file is there. A package
// upgrade can remove the file that an older install named. The agent then
// cannot start the hook and runs the action with no Gryph hook. A bare
// program name resolves through the PATH of the agent, which Gryph cannot
// read, so it never counts as missing.
func MissingProgram(cmd string) (string, bool) {
	program, _, ok := splitCommand(cmd)
	if !ok || !filepath.IsAbs(program) || programFileExists(program) {
		return "", false
	}
	return program, true
}

// programFileExists reports whether a file is at program. Windows also
// runs program.exe for a name with no extension. An error other than a
// missing path does not prove that the program is gone.
func programFileExists(program string) bool {
	candidates := []string{program}
	if runtime.GOOS == "windows" && filepath.Ext(program) == "" {
		candidates = append(candidates, program+".exe")
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return true
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return true
		}
	}
	return false
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

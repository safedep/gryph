package utils

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withExecutable fixes the path of the running binary for GryphCommand and
// clears its cache.
func withExecutable(t *testing.T, path string, err error) {
	t.Helper()
	restore := executablePath
	executablePath = func() (string, error) { return path, err }
	programOnce = sync.Once{}
	t.Cleanup(func() {
		executablePath = restore
		programOnce = sync.Once{}
	})
}

func TestGryphCommand(t *testing.T) {
	tests := []struct {
		name string
		path string
		err  error
		want string
	}{
		{"absolute path", "/opt/safedep/gryph/bin/gryph", nil, "/opt/safedep/gryph/bin/gryph"},
		{"windows path", `C:\Program Files\gryph\gryph.exe`, nil, `C:\Program Files\gryph\gryph.exe`},
		{"another name keeps the PATH lookup", "/tmp/go-build/utils.test", nil, "gryph"},
		{"unreadable path", "", errors.New("no proc"), "gryph"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withExecutable(t, tt.path, tt.err)
			assert.Equal(t, tt.want, GryphCommand())
		})
	}
}

func TestHookCommand(t *testing.T) {
	withExecutable(t, "/opt/safedep/gryph/bin/gryph", nil)
	assert.Equal(t, "/opt/safedep/gryph/bin/gryph _hook claude-code PreToolUse", HookCommand("", "claude-code", "PreToolUse"))
	assert.Equal(t, "/usr/libexec/gryph _hook codex PreToolUse", HookCommand("/usr/libexec/gryph", "codex", "PreToolUse"))
	assert.Equal(t, `"C:\Program Files\gryph\gryph.exe" _hook cursor preToolUse`,
		HookCommand(`C:\Program Files\gryph\gryph.exe`, "cursor", "preToolUse"))
}

func TestIsHookCommand(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		agent string
		hook  string
		want  bool
	}{
		{"bare program", "gryph _hook claude-code PreToolUse", "claude-code", "PreToolUse", true},
		{"absolute path", "/opt/safedep/gryph/bin/gryph _hook claude-code PreToolUse", "claude-code", "PreToolUse", true},
		{"windows exe", `C:\gryph\gryph.exe _hook claude-code PreToolUse`, "claude-code", "PreToolUse", true},
		{"quoted path with a space", `"C:\Program Files\gryph\gryph.exe" _hook claude-code PreToolUse`, "claude-code", "PreToolUse", true},
		{"surrounding space", "  gryph _hook claude-code PreToolUse  ", "claude-code", "PreToolUse", true},
		{"other hook type", "gryph _hook claude-code PreToolUse", "claude-code", "PostToolUse", false},
		{"other agent", "gryph _hook claude-code PreToolUse", "codex", "PreToolUse", false},
		{"another program", "gryphon _hook claude-code PreToolUse", "claude-code", "PreToolUse", false},
		{"helper program", "/usr/bin/gryph-helper _hook claude-code PreToolUse", "claude-code", "PreToolUse", false},
		{"extra argument", "gryph _hook claude-code PreToolUse --x", "claude-code", "PreToolUse", false},
		{"too few fields", "gryph _hook claude-code", "claude-code", "PreToolUse", false},
		{"not the hook entry point", "gryph status", "claude-code", "PreToolUse", false},
		{"unterminated quote", `"C:\gryph.exe _hook claude-code PreToolUse`, "claude-code", "PreToolUse", false},
		{"empty", "", "claude-code", "PreToolUse", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHookCommand(tt.cmd, tt.agent, tt.hook))
		})
	}
}

func TestIsGryphCommand(t *testing.T) {
	assert.True(t, IsGryphCommand("gryph _hook cursor afterFileEdit"))
	assert.True(t, IsGryphCommand("/opt/safedep/gryph/bin/gryph _hook windsurf pre_run_command"))
	assert.False(t, IsGryphCommand("gryph status"))
	assert.False(t, IsGryphCommand("gryphon _hook cursor afterFileEdit"))
	assert.False(t, IsGryphCommand("echo pre-existing-hook"))
}

func TestRenderPlugin(t *testing.T) {
	template := []byte(`execFileSync("__GRYPH_COMMAND__", ["_hook"])`)
	assert.Equal(t, `execFileSync("/opt/safedep/gryph/bin/gryph", ["_hook"])`,
		string(RenderPlugin(template, "/opt/safedep/gryph/bin/gryph")))
	assert.Equal(t, `execFileSync("C:\\Program Files\\gryph\\gryph.exe", ["_hook"])`,
		string(RenderPlugin(template, `C:\Program Files\gryph\gryph.exe`)))

	withExecutable(t, "/usr/local/bin/gryph", nil)
	assert.Equal(t, `execFileSync("/usr/local/bin/gryph", ["_hook"])`, string(RenderPlugin(template, "")))
}

// writeProgram writes an empty file at path and returns path.
func writeProgram(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o755))
	return path
}

func symlink(t *testing.T, target, link string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	return link
}

// Homebrew deletes the versioned directory under Cellar on upgrade. A hook
// that names the versioned file then fails to start, so the hook names the
// opt link, which the upgrade keeps.
func TestGryphCommand_HomebrewOptLink(t *testing.T) {
	prefix := t.TempDir()
	cellar := writeProgram(t, filepath.Join(prefix, "Cellar", "gryph", "0.10.0", "bin", "gryph"))
	optLink := symlink(t, filepath.Join(prefix, "Cellar", "gryph", "0.10.0"), filepath.Join(prefix, "opt", "gryph"))
	opt := filepath.Join(optLink, "bin", "gryph")

	withExecutable(t, cellar, nil)
	assert.Equal(t, opt, GryphCommand())
	assert.Equal(t, []string{opt, cellar, optLink}, ProgramFiles())
}

func TestGryphCommand_NoOptLink(t *testing.T) {
	cellar := writeProgram(t, filepath.Join(t.TempDir(), "Cellar", "gryph", "0.10.0", "bin", "gryph"))
	withExecutable(t, cellar, nil)
	assert.Equal(t, cellar, GryphCommand())
	assert.Equal(t, []string{cellar}, ProgramFiles())
}

// An opt link that points at another version is not the running binary.
func TestGryphCommand_OptLinkToAnotherVersion(t *testing.T) {
	prefix := t.TempDir()
	cellar := writeProgram(t, filepath.Join(prefix, "Cellar", "gryph", "0.10.0", "bin", "gryph"))
	writeProgram(t, filepath.Join(prefix, "Cellar", "gryph", "0.10.1", "bin", "gryph"))
	symlink(t, filepath.Join(prefix, "Cellar", "gryph", "0.10.1"), filepath.Join(prefix, "opt", "gryph"))

	withExecutable(t, cellar, nil)
	assert.Equal(t, cellar, GryphCommand())
}

func TestProgramFiles_NotGryph(t *testing.T) {
	withExecutable(t, "/tmp/go-build/utils.test", nil)
	assert.Empty(t, ProgramFiles())
}

func TestHomebrewOptPath(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep, "opt", "homebrew")
	tests := []struct {
		name string
		real string
		want string
	}{
		{"cellar binary", filepath.Join(root, "Cellar", "gryph", "0.10.0", "bin", "gryph"), filepath.Join(root, "opt", "gryph", "bin", "gryph")},
		{"revision suffix", filepath.Join(root, "Cellar", "gryph", "0.10.0_1", "bin", "gryph"), filepath.Join(root, "opt", "gryph", "bin", "gryph")},
		{"another formula", filepath.Join(root, "Cellar", "gryphon", "1.0", "bin", "gryph"), ""},
		{"not in bin", filepath.Join(root, "Cellar", "gryph", "0.10.0", "libexec", "gryph"), ""},
		{"no cellar", filepath.Join(sep, "usr", "local", "bin", "gryph"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, homebrewOptPath(tt.real))
		})
	}
}

// After an upgrade removed the file that a hook names, status must report
// the hook as broken. A bare name resolves through the PATH of the agent.
func TestMissingProgram(t *testing.T) {
	dir := t.TempDir()
	present := writeProgram(t, filepath.Join(dir, "bin", "gryph"))
	gone := filepath.Join(dir, "Cellar", "gryph", "0.9.0", "bin", "gryph")
	spaced := writeProgram(t, filepath.Join(dir, "with space", "gryph"))
	directory := filepath.Join(dir, "directory", "gryph")
	require.NoError(t, os.MkdirAll(directory, 0o755))
	underFile := filepath.Join(present, "gryph")

	tests := []struct {
		name    string
		cmd     string
		program string
		missing bool
	}{
		{"present", present + " _hook claude-code PreToolUse", "", false},
		{"removed by an upgrade", gone + " _hook claude-code PreToolUse", gone, true},
		{"a directory", directory + " _hook claude-code PreToolUse", directory, true},
		{"a file in place of a directory", underFile + " _hook claude-code PreToolUse", underFile, true},
		{"quoted path with a space", `"` + spaced + `" _hook claude-code PreToolUse`, "", false},
		{"bare name", "gryph _hook claude-code PreToolUse", "", false},
		{"relative path", "bin/gryph _hook claude-code PreToolUse", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, missing := MissingProgram(tt.cmd)
			assert.Equal(t, tt.missing, missing)
			assert.Equal(t, tt.program, program)
		})
	}
}

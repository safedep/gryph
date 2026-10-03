package utils

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// withExecutable fixes the path of the running binary for GryphCommand and
// clears its cache.
func withExecutable(t *testing.T, path string, err error) {
	t.Helper()
	restore := executablePath
	executablePath = func() (string, error) { return path, err }
	commandOnce = sync.Once{}
	t.Cleanup(func() {
		executablePath = restore
		commandOnce = sync.Once{}
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

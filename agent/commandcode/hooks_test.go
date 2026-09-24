package commandcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/gryph/agent/utils"
	"github.com/stretchr/testify/assert"
)

func TestGenerateHooksConfig_CommandFormat(t *testing.T) {
	config := GenerateHooksConfig()
	expectedPrefix := utils.GryphCommand() + " _hook command-code "

	for _, hookType := range HookTypes {
		matchers, ok := config[hookType]
		assert.True(t, ok, "hook type %s should exist in config", hookType)
		assert.Len(t, matchers, 1, "hook type %s should have exactly one matcher", hookType)
		assert.Len(t, matchers[0].Hooks, 1, "hook type %s matcher should have exactly one hook command", hookType)

		cmd := matchers[0].Hooks[0].Command
		assert.True(t, strings.HasPrefix(cmd, expectedPrefix), "command should start with %q, got %q", expectedPrefix, cmd)
		assert.True(t, strings.HasSuffix(cmd, hookType), "command should end with %q, got %q", hookType, cmd)
	}
}

func TestGenerateHooksConfig_NoMatchers(t *testing.T) {
	// Command Code matches every tool when the matcher is omitted, and a
	// matcher on Stop/SessionStart prevents those hooks from firing at all.
	config := GenerateHooksConfig()

	for _, hookType := range HookTypes {
		assert.Empty(t, config[hookType][0].Matcher,
			"hook type %s must not set a matcher", hookType)
	}
}

func TestIsOwnedHookCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		hookType string
		want     bool
	}{
		{"exact match", "gryph _hook command-code PreToolUse", "PreToolUse", true},
		{"different hook type", "gryph _hook command-code PreToolUse", "Stop", false},
		{"gryphon lookalike", "gryphon _hook command-code PreToolUse", "PreToolUse", false},
		{"gryph-helper lookalike", "/usr/local/bin/gryph-helper _hook command-code Stop", "Stop", false},
		{"absolute path to gryph", "/usr/local/bin/gryph _hook command-code Stop", "Stop", true},
		{"other agent's hook", "gryph _hook claude-code PreToolUse", "PreToolUse", false},
		{"not a hook command", "gryph logs --limit 5", "PreToolUse", false},
		{"too few fields", "gryph _hook command-code", "PreToolUse", false},
		{"empty", "", "PreToolUse", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOwnedHookCommand(tt.cmd, tt.hookType))
		})
	}
}

func TestReadSettings_RejectsMalformedHooks(t *testing.T) {
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.json")
	assert.NoError(t, os.WriteFile(bad, []byte(`{"hooks": "not-an-object"}`), 0600))
	_, err := readSettings(bad)
	assert.ErrorContains(t, err, `invalid "hooks" section`)

	good := filepath.Join(dir, "good.json")
	assert.NoError(t, os.WriteFile(good, []byte(`{"hooks": {"Stop": []}, "other": 1}`), 0600))
	settings, err := readSettings(good)
	assert.NoError(t, err)
	assert.Contains(t, settings, "other")
}

func TestReadSettings_RejectsTopLevelNull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "null.json")
	assert.NoError(t, os.WriteFile(path, []byte(`null`), 0600))

	_, err := readSettings(path)
	assert.ErrorContains(t, err, "expected an object",
		"a top-level null must be rejected before InstallHooks can panic on a nil map")
}

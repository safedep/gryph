package agent

// HookConfigGlobs returns doublestar glob patterns matching the hook
// configuration files for every supported agent. The AARM self-protection
// rules block agent writes to these paths so a governed agent cannot rewrite
// its own hook wiring to disable Gryph. Patterns are anchored with "**/" so
// they match regardless of the user's home directory location and are
// forward-slash normalized to match the PDP's path normalization.
//
// Keep this list in sync with each adapter's detect.go hook-config location.
func HookConfigGlobs() []string {
	return []string{
		"**/.claude/settings.json",
		"**/.claude/hooks/**",
		"**/.cursor/hooks.json",
		"**/.gemini/hooks/**",
		"**/.codex/hooks.json",
		"**/.config/opencode/plugins/**",
		"**/.openclaw/extensions/**",
		"**/.codeium/windsurf/hooks.json",
		"**/.pi/agent/extensions/**",
	}
}

#!/usr/bin/env bash
# Checks that the vendor documentation still names the managed settings
# keys and paths that gryph install --managed relies on. A vendor can
# rename a key, and the locked class then degrades without a signal. The
# weekly workflow runs this script and fails on a missing name, so a human
# checks the agent release and the class in
# docs/agent-enforcement-coverage.md.
set -u

status=0

check() {
  local agent="$1" url="$2"
  shift 2
  local body
  if ! body=$(curl -fsSL --retry 3 --max-time 60 -A "gryph-docs-check" "$url"); then
    echo "FAIL $agent: cannot fetch $url"
    status=1
    return
  fi
  local missing=()
  for token in "$@"; do
    if ! grep -qF -- "$token" <<<"$body"; then
      missing+=("$token")
    fi
  done
  if [ ${#missing[@]} -eq 0 ]; then
    echo "ok   $agent: $url"
  else
    echo "FAIL $agent: $url is missing: ${missing[*]}"
    status=1
  fi
}

# Claude Code, class locked. Owner: abhisek.
check "claude-code" "https://code.claude.com/docs/en/managed-settings.md" \
  "allowManagedHooksOnly" "managed-settings.d" "/etc/claude-code" \
  "Application Support/ClaudeCode" "ClaudeCode" "disableAllHooks"

# Codex, class locked. Owner: abhisek.
check "codex" "https://learn.chatgpt.com/docs/enterprise/managed-configuration" \
  "allow_managed_hooks_only" "requirements.toml" "/etc/codex"
check "codex-hooks" "https://learn.chatgpt.com/docs/hooks" \
  "allow_managed_hooks_only" "managed_dir"

# Cursor, class system_path.
check "cursor" "https://cursor.com/docs/agent/hooks" \
  "failClosed" "/etc/cursor/hooks.json" "Application Support/Cursor/hooks.json"

# Gemini CLI, class system_path. The source is the reference, the docs lag.
check "gemini" "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/cli/src/config/settings.ts" \
  "GEMINI_CLI_SYSTEM_SETTINGS_PATH" "/etc/gemini-cli/settings.json" "GeminiCli/settings.json"
check "gemini-schema" "https://raw.githubusercontent.com/google-gemini/gemini-cli/main/packages/cli/src/config/settingsSchema.ts" \
  "hooksConfig" "hooks"

# Windsurf, class system_path.
check "windsurf" "https://docs.devin.ai/desktop/cascade/hooks" \
  "/etc/devin/hooks.json" "Application Support/Devin/hooks.json"

exit $status

# Upgrade fixtures

`audit-v<version>.db` is a database written by that released version of
gryph. The `storage/upgrade/previous-release-db-opens` acceptance script
opens it with the current binary and checks that events survive and the
schema migrates.

Regenerate the fixture after each release. Build the released tag, write a
known set of events through the real hook path, and copy the database file
before any newer binary touches it:

```bash
git worktree add /tmp/gryph-rel v<version>
(cd /tmp/gryph-rel && go build -o /tmp/gryph-rel-bin ./cmd/gryph)
export HOME=/tmp/gryph-fixture XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share
echo '{"session_id":"fixture-session","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_use_id":"f1"}' | /tmp/gryph-rel-bin _hook claude-code PreToolUse
echo '{"session_id":"fixture-session","cwd":"/p","hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"/p/main.go","content":"package main"},"tool_response":{"success":true},"tool_use_id":"f2"}' | /tmp/gryph-rel-bin _hook claude-code PostToolUse
echo '{"session_id":"fixture-session","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/p/.env"},"tool_use_id":"f3"}' | /tmp/gryph-rel-bin _hook claude-code PreToolUse
echo '{"session_id":"fixture-session","cwd":"/p","hook_event_name":"SessionEnd","reason":"user_exit"}' | /tmp/gryph-rel-bin _hook claude-code SessionEnd
cp $XDG_DATA_HOME/gryph/audit.db test/acceptance/testdata/upgrade/audit-v<version>.db
git worktree remove /tmp/gryph-rel
```

Update the fixture file name and the expected counts in the script when the
fixture changes. Keep one fixture: the most recent release.

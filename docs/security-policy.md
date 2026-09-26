# Security Policy

Gryph enforces a YAML policy over every agent action. A rule can **block**, **warn**, **guide**, or **allow**. Blocked actions never reach the agent's tool, guidance is delivered back to the agent as stderr text, and every decision is persisted to the event log.

Policies are evaluated by an AARM-shaped Policy Decision Point. This document provides guidance for
policy authoring and usage.

## Quick start

```bash
gryph policy init          # write the example policy to the global config dir
gryph policy edit          # open it in $EDITOR to author your rules
gryph policy validate
gryph policy test --action file_write --path ./secrets/db.env
```

Edit the global policy between steps using `gryph policy edit`. Every feature has inline documentation in the generated file.

Turn enforcement in configuration. See `gryph config show` for details. You can also run `gryph
policy set policy.enabled=true` to enable policy enforcement or edit the `config.yml` file directly.

```yaml
policy:
  enabled: true
```

Once enabled, every supported agent hook runs through the engine.

## Where policy files live

Gryph loads policy from three sources, in this order:

1. **Global policy file** (`${ConfigDir}/policy.yaml`, optional). The single operator-owned file. On macOS this is `~/Library/Application Support/safedep/gryph/policy.yaml`; on Linux `~/.config/safedep/gryph/policy.yaml`. A missing file is not an error.
2. **Policies directory** (`${ConfigDir}/policies/*.yaml` and `*.yml`, optional). Each file is a separate policy document. Files load in sorted name order and merge after the global file. A missing directory is not an error. This lets you author policy as many small, self-contained files instead of one large file.
3. **Built-in self-protection rules** (always appended, never filtered). These protect the config directory, the database, and agent hook configs from agent self-modification. A second rule, `gryph-builtin-protected-reads`, blocks agent reads of the database (with its `-wal`, `-shm`, and `-journal` files) and of the receipt signing key. A third rule, `gryph-builtin-hook-command`, blocks an agent shell command that runs `gryph _hook`. Such a command can record a forged event. An agent may read the policy files and the hook configs. Self-protection is best effort. See [Self-protection limits](#self-protection-limits).

With `policy.enabled: true` and no user files on disk, the merged policy contains built-in self-protection rules only.

Both the global file and the policies directory sit inside `${ConfigDir}`, so both are protected by the built-in self-protection rules. Gryph resolves no other location. A file at any other path is never loaded as policy.

### Self-protection limits

Self-protection is best effort. It blocks file writes and deletes to protected paths, and shell commands that Gryph can parse as a change to a protected path. It blocks file reads of the database and the signing key, and shell commands that Gryph can parse as a read of them. It does not stop every bypass. A human or an agent can get past it in many ways, for example:

- A command that builds the path at run time, such as a variable, a command substitution, or a base64 payload.
- A script file or an interpreter (`python -c`, `node -e`) that writes the file or runs `gryph _hook`.
- A copy of the `gryph` binary under another name that runs `_hook`.
- A command that builds the `_hook` word or the gryph program at run time, such as a variable, a glob, `xargs`, a function, a shell script read from standard input, or an `eval` of generated text.
- A process that runs outside the agent's hook path, as the same operating-system user.
- A command that the shell parser rejects.
- An archive with absolute member paths (`tar -P`, `7z -spf`, `unzip -:`), or a recursive copy into a parent of a protected directory, such as `cp -r evil/safedep ~/.config/`.
- A human who edits the file or turns off `policy.self_protection.enabled`.
- A symbolic link to a protected directory, or a `~user/` path.
- On macOS, a path in a different letter case. The file system ignores case, and the match does not.
- The Gryph commands that print the audit data, such as `gryph query`, `gryph export`, and `gryph cat`.

The rule `gryph-builtin-hook-command` is best effort. It catches only a literal `gryph _hook` call. It checks each call in the command, also inside wrappers (`env`, `sudo`, `exec`, `command`, `setsid`, `strace`, `parallel`, and others), `find -exec`, `bash -c` (also after a flag group such as `-euo pipefail`), a here-document or a here-string with literal text that a shell reads, `eval`, `watch`, `flock`, `su -c`, `runuser`, and `script`. Gryph decodes ANSI-C quoting (`$'\x5fhook'`) and expands braces before the check. A call blocks when its program is gryph, or a variable or a command substitution, and one argument is the literal word `_hook`. So `gryph _hook claude-code UserPromptSubmit`, `bash -c 'gryph _hook x y'`, and `$G _hook x y` block. A word that can only become `_hook` when the shell runs the command does not block. So `gryph query --session "$SID"`, `gryph _hoo? x y`, `echo _hook | xargs gryph`, and `eval "$S"` pass. A word such as `_hook` in the arguments of another program, as in `grep -rn _hook cli/`, does not block.

The read rule also blocks a file read of each directory that holds the database or the signing key, at any depth, below the home directory. This blocks a `Grep`, `Glob`, or `LS` tool call on the data directory, the config directory, their `safedep` parents, `~/.config`, and `~/.local/share`. On macOS, these are the `~/Library` directories that hold them. A read of one file in these directories, such as `policy.yaml`, passes. A file read of the home directory, or of a parent of it, passes. So does a shell read of it, such as `grep -r x ~` or `tar -C ~ -czf home.tgz .`. A tool that searches the whole home directory can read the protected files, so this is a limit of the read rule. A `sqlite3` command that names a file with a SQL expression is also a limit.

Kernel-based self-protection is on the roadmap. Until then, treat self-protection as a guard against mistakes and simple attempts, not as a security boundary. The [threat model](./security-policy-threat-model.md) lists the lower-level controls for a hardened deployment.

Write a file with `gryph policy init [name|path]` or open one with `gryph policy edit [name|path]`. See [Commands](#commands). Run `gryph policy list` to see every active source. Per-host managed policy is a planned future iteration. Today, one host governs its own policy.

Use `disabled:` to suppress a rule by ID. `disabled:` is scoped to the file that declares it. It removes only rules defined in the same file. A file cannot disable a rule from another file, and no user file can disable a built-in rule. Rule IDs must be unique across all files. User rules may not use the `gryph-builtin-` prefix. Namespace your rule IDs by the file's purpose to avoid collisions.

```yaml
# in the same file that defines block-rm-rf-root
disabled:
  - block-rm-rf-root
```

## Authoring a rule

A rule is a YAML object with at minimum an `id` and an `action`. Everything else is optional.

```yaml
version: "1"
rules:
  - id: block-prod-writes
    action: block
    severity: high
    match:
      action_types: [file_write]
      file_patterns:
        - "**/prod/**"
    message: |
      Refusing write to {{.Action.Params.Path}}. Production changes go
      through the release pipeline.
```

Run `gryph policy test --action file_write --path /repo/prod/config.yaml` to see the rule fire.

### Match criteria

| Field | Type | Notes |
|---|---|---|
| `action_types` | list | `file_read`, `file_write`, `file_delete`, `command_exec`, `network_request`, `tool_use`, `session_start`, `session_end`, `notification`, `subagent_start`, `subagent_stop`, `user_prompt` |
| `file_patterns` | list | Doublestar globs (`**`) over the action path. For `command_exec`, also over the shell targets that `file_access` selects (see below) |
| `file_access` | list | `read`, `write`, `remove`. The shell targets that `file_patterns` match. The default is `[write, remove]`. Requires `file_patterns` |
| `command_patterns` | list | Go regexps over the shell command |
| `tool_names` | list | Exact tool names like `Bash`, `Write`, `WebFetch` |
| `content_patterns` | list | Go regexps over the captured content preview |
| `working_directory_patterns` | list | Doublestar globs over the agent's cwd |

An empty `match` block matches every action. Combine with `scope` to narrow further.

For a `command_exec` action, Gryph parses the shell command and finds the paths it changes: redirect targets (`>`, `>>`), `tee`, `cp` / `mv` / `install` / `ln` destinations, `rm` / `unlink`, `truncate`, `chmod` / `chown`, `sed -i` / `perl -i`, `dd of=`, editors (`vim`, `vi`, `nvim`, `ex`, `nano`), `sort -o`, `gzip`, `bzip2`, `xz`, `zip`, `7z`, `tar`, `unzip`, and the output files of `curl` and `wget`. A recursive copy of a directory, a copy of directory contents, an archive extract, and a recursive download can write paths in the destination directory that the command line does not show. They match a pattern only when the destination is the directory that holds the pattern, or a path that the pattern matches. A parent directory does not match. So `tar xzf node_modules.tgz` or `unzip -o dist.zip` in the project root, and `cp -r dotfiles/nvim ~/.config/`, do not block under self-protection or under a rule on `**/.env`. A recursive copy of a directory into a directory that holds a protected path still matches when the copy creates that path, so `cp -r dotfiles/devin ~/.config/` blocks under a rule on `**/.config/devin/config.json`. `tar xzf a.tgz -C ~/.config/safedep/gryph` blocks. For `find`, `-delete` changes the search roots, and `-exec` runs its command on any path under the roots. `-execdir` runs its command from any directory under the roots, so a relative path it changes counts as a change to the roots. Gryph looks inside wrappers such as `sudo`, `env`, `nice`, `timeout`, `setsid`, and `strace`. It skips the wrapper options and their values, and it takes the first remaining word as the program. Gryph also looks inside `bash -c` (also `bash -lc` and `bash -euo pipefail -c`), a here-document or a here-string that a shell reads, `eval`, `watch`, `flock`, `su -c`, `runuser`, `script`, and `parallel`, and follows a literal `cd`, also through `eval`, `command cd`, and `builtin cd`. A `cd` inside a subshell, a pipe, or `bash -c` does not carry over. A command after `&&`, `||`, or in an `if` or loop body may not run, so Gryph checks the paths from every working directory it could have. `file_patterns` matches these paths. A delete or move of a directory also matches every pattern under that directory. The same is true for a `file_delete` action. A read, such as `cat` or `find -exec cat`, does not match by default. A command that does not parse records no paths, so it does not match.

Gryph also finds the paths a command reads: input redirects (`<`), the sources of `cp`, `mv`, `rsync`, and `scp`, `dd if=`, and the file operands of read commands such as `cat`, `head`, `tail`, `grep`, `sed` and `awk` without `-i`, `tar`, `base64`, `strings`, `xxd`, and `sqlite3`. A rule with `file_access: [read]` matches these paths. A shell read of a directory also matches when the directory is a literal parent in a pattern, because a copy or a recursive read, such as `cp -r dir` or `grep -r x dir`, reads every file in it. For example, a read of `/data` or `/data/gryph` matches `/data/gryph/audit.db`, and a read of `conf` matches `**/conf/.env`. A pattern segment with a glob character is not a literal parent. So `**/.env` does not match a recursive read of a directory, such as `grep -r x .` or `tar czf out.tgz .`, even when the directory holds a `.env` file. A read through a glob matches when the glob and a pattern can match the same path: `cat dir/*` matches `dir/secret.txt`, `cat .e*` matches `**/.env`, and `cat dir/*.md` does not. A glob that can name a directory that holds a matching path also matches, so `cp -r dir/* /out` matches `dir/sub/secret.txt`. As in bash without `dotglob`, a shell glob segment that starts with `*`, `?`, or a bracket class does not match a name that starts with a dot. So `cat *` does not match `**/.env`, and `cat .e*` and `cat */.e*` do. Gryph knows bracket classes such as `[]a]`, `[!a]`, and `[[:alpha:]]`. A class that does not close matches any name. A pattern segment with a brace, such as `*.{yml,yaml}`, matches every glob segment. For a `file_read` action, a rule that selects `read` also matches a read of a directory that holds a matching path at any depth. A read of the home directory or one of its parents, by a `file_read` action or by a shell command such as `grep -r x ~`, matches the file patterns only. A glob below home, such as `cp -r ~/.c* /out`, does not get this exemption. A flat read matches the file patterns only, because it reads only the files that it names. The flat reads are the read commands such as `cat`, `head`, `wc` and `md5sum`, `diff` and `zcat` without `-r`, `grep` without `-r`, `-R` or `-d recurse`, `awk`, `jq`, and `cp` without `-r`, `-R` or `-a`. So `grep -n PATH ~/.*` and `cat ~/.*` do not match a directory such as `~/.config`, and `grep -rn PATH ~/.*` and `rg PATH ~/.*` do. For `find -exec`, Gryph uses the `-name` pattern when one `-name` test before the first action selects the files. So `find . -name '*.go' -exec grep x {} +` does not match `**/.env`, and `find . -exec cat {} +` does. Unlike the shell, `find -name` lets a leading `*` match a dot, so `find . -name '*.env' -exec cat {} +` matches `**/.env`.

For `sqlite3`, Gryph reads the database of a `file:` URI after it removes the query and a `localhost` host and decodes percent escapes. It also reads the `-init` file, and the files that `.open`, `.read`, `.import`, `.restore`, or `.load` names in an operand or in a `-cmd` value. Gryph reads the file of `ATTACH` at the start of a statement, and of `readfile()`, `fsdir()`, or `load_extension()`, only when one single-quoted string names the file. It skips string literals and comments. So `select * from t where name like '%attach%'` and `select 1; -- attach` read no file. When an expression names the file, Gryph records no read. Gryph checks the command of `.shell` and `.system` as a shell command. `curl` reads the file of a `file://` URL. `tar` applies each `-C` to the members after it. Gryph expands a brace list such as `audit.{db,bak}` into its words. A brace sequence such as `{1..9}` becomes the glob `*`.

For a command that Gryph does not know, each operand is a possible read. So is the value after `=` in an option such as `--file=x`, and each tail of a short option such as `-fx`, because the option can take its value in the same word. For `git`, each operand and option value is a possible read, relative to the last `-C` directory. A `-C`, `--git-dir`, or `--work-tree` directory is a read of that directory, so it matches the patterns under it. A rule that selects `read` matches such an operand when a pattern matches it. A directory operand of such a command does not match the patterns under it, because the command may not read the directory tree. Commands that read no file content, such as `ls`, `stat`, `du`, `echo`, and `mkdir`, have no reads.

Gryph resolves the action path before it matches: it expands `~`, joins a relative path with the working directory, and removes `.`, `..`, and a trailing slash. A pattern matches the path as the agent reports it or the resolved path. Set `file_access: [read, write, remove]` to match every shell target.

```yaml
- id: no-secret-reads
  action: block
  match:
    action_types: [file_read, command_exec]
    file_patterns: ["**/.env"]
    file_access: [read]
```

This rule blocks `cat .env`, `cat config/.e*`, and a `file_read` of `.env`. It does not block `grep -r x .` or `cat *`. To block a recursive read of a directory that holds the file, add a pattern with a literal parent, such as `/repo/config/.env`.

The parse is best effort. It can match more than the command changes, and it can miss a change. Gryph does not run the command, so it cannot resolve an unknown variable, a command substitution, a script file, a process substitution, input to `xargs` or `parallel`, or an encoded payload. Gryph records only the paths that it can resolve. It does not fail closed on a command that it cannot parse or cannot resolve. For example, `rm -rf ~/.cc ) (`, `curl -w @format.txt`, `tar -xPf a.tar`, and a `7z` command that Gryph does not know do not block. For `xargs -I{}` and `parallel`, Gryph puts the glob `*` in place of the replace string, so `xargs -I{} cp {} dir/{}` writes `dir/*`.

### Scope

```yaml
scope:
  agents:   [claude-code, cursor]
  projects: [payments]
  tools:    [Bash]
```

`scope` is AND-combined with `match`. Omit a field to mean "any". Useful for shipping one policy file across teams and having rules opt in only where they apply.

### Conditions (CEL)

`condition` is a CEL expression that must return bool. It runs after `match` succeeds.

```yaml
- id: warn-large-edit
  action: warn
  match: { action_types: [file_write] }
  condition: >
    action.params.lines_added > 200 &&
    !action.params.path.contains("generated")
  message: |
    Large edit ({{.Action.Params.LinesAdded}} lines) to {{.Action.Params.Path}}.
    Consider splitting into smaller commits.
```

Available variables:

```
action.type / tool / operation / agent / working_dir / project
action.params.{path, command, args, url, size_bytes, lines_added, lines_removed, content}
action.data_classifications        list, set by the heuristic classifier
action.injection_score             float 0..1, for tool calls, post events and intents.
                                   Each match adds 0.15. A strong phrase adds at most 0.6,
                                   and all weak phrases together add at most 0.45.
action.kind                        intent, action, or observation
action.origin                      where the content came from, as the adapter claims it
action.source                      the MCP server of an mcp origin
action.sources                     every MCP server that the tool name can name
action.hosts                       hosts of the shell command and the URL, "?" for a host that Gryph cannot read
action.read_paths / write_paths    paths that the action reads, and writes or removes
action.human_principal             captured identity, see Identity capture
action.service_identity            CI / service identity, see Identity capture
action.role_scope                  OS uid/gid + asserted scopes
action.gryph_hook                  true when a shell command runs gryph _hook
context.{total_actions, files_read, files_written, commands_executed,
         network_requests, errors, tools_used, session_duration_ms,
         classifications_seen, tags_seen, tag_seq, origins_seen,
         entities_seen, egress_hosts, entries, intent_available,
         actions_since_intent}
```

`action.data_classifications` carries labels like `secret`, `pii`, `source_code`, `config`, `git_internal`, `external_url`. `context.classifications_seen` is the running union across the session.

The counters count actions, and the current event is in them. A pre and post hook pair for one tool call is one action: the post event is an observation of the pre event, and it does not add to a counter. A post event with no recorded pre event is an action. A blocked action counts. `errors` counts actions and observations with an error result.

A user prompt is an intent. It is not an action, and it does not add to a counter. `context.intent_available` is true when the session has at least one intent. An agent with no prompt hook never has one. `context.actions_since_intent` counts the actions after the latest intent. A new prompt resets it to zero. A prompt becomes the latest intent only when it reaches the agent. A prompt that a rule blocks or defers does not. An escalated prompt becomes the latest intent only when the approver approves it. A denied or timed-out prompt does not. A prompt whose evaluation fails does not become the latest intent, also when `fail_mode: open` lets it run. A rule with no `action_types` does not apply to prompts, so a broad rule, such as a cap on `total_actions`, cannot stop the user from typing. A rule with `action_types: [user_prompt]` matches the prompt, and `content_patterns` read the prompt text. A CEL condition reads at most the first 8 KiB of the prompt in `action.params.content`, and `action.content_truncated` is then true. `content_patterns` still read the whole prompt, and they cost nothing in CEL, so put a long regex there. When one rule's condition fails, a matched `block`, `escalate` or `defer` rule still decides. With no such rule, `fail_mode` decides:

```yaml
- id: prompt-injection
  action: block
  match:
    action_types: [user_prompt]
    content_patterns: ["(?i)ignore (all )?previous instructions"]
```

For a prompt, `action.params.content` and `content_patterns` read the full prompt that the agent gets. The full prompt includes the content of referenced files that the agent adds, such as the Gemini CLI `@` file block. `action.params.content` holds at most the first 8 KiB of the full prompt, so a CEL condition stays under its cost limit. When Gryph cuts it, `action.content_truncated` is true. `content_patterns` always match the whole prompt. The recorded intent holds only the part that the user typed. Gryph cuts the Gemini CLI prompt at the first start line of a file block. So a user who types a fake block before the real block loses user text from the intent. File content never goes into the intent.

The intent fields trust the prompt events that reach `gryph _hook`. The hook input does not prove that the user typed the prompt. So an agent that can run `gryph _hook` can forge a prompt, and reset `context.actions_since_intent`. The built-in rule `gryph-builtin-hook-command` blocks a shell command that runs `gryph _hook`. The intent fields are only as strong as this block. The block is best effort, as the other self-protection rules are. See [Self-protection limits](#self-protection-limits). Privilege separation between the agent and the hook will remove this dependency.

**Upgrade note.** Before this change, the Cursor `beforeSubmitPrompt` hook, the Windsurf `pre_user_prompt` hook, and the Codex and Devin `UserPromptSubmit` hook made `tool_use` events with that tool name. Now each prompt is a `user_prompt` action with no tool name. A rule that stopped prompts in one of these ways no longer matches them:

- A rule on `tool_names` or `scope.tools` with one of these names.
- A rule on `action_types: [tool_use]`.
- A rule with no `action_types`.

A rule that must stop prompts must list `user_prompt` in `action_types`. Gryph logs a warning at policy load, and `gryph policy validate` prints one, for a rule that names one of these tool names.

`action.hosts` is best effort. It holds `?` for a network tool whose operand Gryph cannot read (`curl $URL`, `curl -K file`, `curl --resolve`, `wget -i file`, `socat`, `ssh -F file`, `ssh -o ProxyCommand=...`, `ssh -D`), for a network tool that `xargs` or `parallel` gives input arguments, and for shell code that Gryph cannot read (`eval "$X"`, `bash -c "$X"`, `... | sh`, `bash <(...)`, `bash /dev/stdin`, `source <(...)`, `su user`). It holds `?` for a shell option that Gryph does not know, because the option can hide the script. It holds `?` for a command that Gryph does not know when an argument is the bare name of a network tool or an absolute path to it, such as `mytool curl x.example`. A relative path or a package name, such as `go test ./internal/ssh` or `docker pull curlimages/curl`, does not count, and build and package tools such as `go`, `make`, `npm`, `docker`, and `man` do not give `?` in this way. It holds `?` for a git fetch or push to a named remote, for a git config variable such as `GIT_CONFIG_GLOBAL`, and for `GIT_SSH` with a program other than `ssh`. It also holds the proxy, jump, and forward hosts of `ssh -J`, `-W`, `-L`, and `-R`, of `ssh -o ProxyJump`, `-o HostName`, and `-o LocalForward`, of `curl -x` and `--connect-to`, of a proxy variable such as `ALL_PROXY` or `https_proxy`, of `GIT_SSH_COMMAND` and `git -c core.sshCommand`, of `git -c http.proxy` and `-c https.proxy`, and of a `git -c url.<base>.insteadOf` rewrite. It does not hold a proxy or a host alias from a config file that the command does not name, such as `~/.ssh/config`, `~/.curlrc`, `~/.wgetrc`, a git config file, or the ssh command of `rsync -e`. Gryph records a read of a sourced regular file, such as `source ./env.sh`. It does not read the code in the file, so the hosts of that code are not in `action.hosts`. It is empty for a program that opens a connection itself, such as a Python script, `npm publish` or `gh gist create`, and for a tool with no URL, such as WebSearch or most MCP tools. So an egress allow list on `action.hosts` is a check on the command, not a network control. `glob()` and `file_patterns` are case-sensitive.

`context.entities_seen` holds the `path:`, `host:` and `mcp:` keys of the session, including the current action and blocked actions. The `path:` keys come from the action path and from the read and write paths of a shell command. A guessed read of a command that Gryph does not know, such as `./...` in `go test ./...`, is not a key. Each kind of key (`path`, `host`, `mcp`) holds at most 500 keys, so a session with many paths still records a new host. `context.egress_hosts` holds the hosts that earlier actions contacted. A blocked or deferred action contacted no host, so it adds none. An escalated action adds its hosts only with its post event, after an approval.

`context.entries` is the entry log: the latest entries of the session, oldest first. `policy.context.cel_entries` sets the count (default 100, at most 1000). `gryph config set` rejects a value out of this range. A config file or environment variable with such a value loads with a warning, and Gryph uses the nearest bound. Gryph loads the log only when a rule reads it. Each item is a map with `seq`, `kind`, `action_type`, `tool`, `path`, `command`, `host`, `mcp_server`, `origin`, `classes`, `tags`, `decision` and `result`. `command` is the stored command, after redaction. No item holds content. Gryph cleans `path` before a rule reads it. It expands `~`, joins a relative path with the working directory of the event, and removes `.`, `..`, and repeated slashes. So `/root/.ssh/./id_rsa` and `/root//.ssh/id_rsa` reach a rule as `/root/.ssh/id_rsa`. Gryph reads at most 65,536 characters of a path. A longer path keeps its first and last 32,768 characters, with a `/` between them. The log cuts `command` to its first 1024 bytes, the clean `path` to its last 1024 bytes, and `tool`, `host` and `mcp_server` to 256 bytes, so that padding cannot push a rule over its limits. `glob(path, pattern)` matches one path against one doublestar pattern:

```yaml
- id: write-after-key-read
  action: block
  match:
    action_types: [file_write]
  condition: 'context.entries.exists(e, e.action_type == "file_read" && glob(e.path, "**/*.pem"))'
```

`glob()` does not match a directory that holds a matching path. A `file_patterns` rule with `file_access: [read]` does. So use `file_patterns`, not `glob()` over `action.read_paths`, to match a shell read of a secret, such as `tar c ~/.ssh`.

`context.semantic_drift` is removed. `gryph policy validate` and `gryph policy install` reject a
policy that reads it or `.Context.SemanticDrift`. An installed policy that reads it still loads
with a warning, and the value is always zero.

### Facts and tags

The session context records facts. Your policy decides what the facts mean.

| Value | Set by | Notes |
|---|---|---|
| `kind`, `phase` | The adapter's `Hooks()` table | Fixed per hook |
| `origin`, `source` | The adapter, as a claim | From the tool name and the path |
| `data_classifications` | The built-in classifier | A closed set. A fact, not a decision |
| `tags` | Only your tag rules | Gryph ships no tag rule |
| The decision | Only your rules, plus self-protection | - |

`action.origin` is one of `user`, `agent`, `file_project`, `file_external`, `command`, `web`, `mcp`, and `unknown`. A web tool (`WebFetch`, `WebSearch`, `browser_*`) or a network request gives `web`. An `mcp__<server>__<tool>` tool gives `mcp`, and `action.source` names the server. A read inside the working directory gives `file_project`, and any other read, including a `~` path, gives `file_external`. A read with no path or no working directory gives `unknown`. The origin is a claim about the path string, and Gryph does not resolve symbolic links. A shell command gives `command`, and a write gives `agent`. Gryph does not decide which origin is trusted.

A server name can hold `__`, so a tool name such as `mcp__github__x__get` has no single reading. `action.source` is then empty, so a rule that trusts one server fails closed. `action.sources` lists every reading (`github` and `github__x`), and `context.origins_seen` gets `mcp:<server>` for each one. A rule that denies one server must match `action.sources`, not `action.source`:

```yaml
- id: deny-evil-server
  action: block
  match:
    action_types: [tool_use]
  condition: '"evil" in action.sources'
```

Claude Code and other agents that use `mcp__<server>__<tool>` names give the server name from the agent config. Windsurf sends the server name. A Cursor MCP hook sends no server name, so Gryph makes one from the server URL or command. When the adapter names the server, as Cursor and Windsurf do, `action.source` holds that name. The server author chooses the tool name, so an evil server can name a tool `mcp__github__get_issue`. Gryph then records the server that the adapter names, not the server in the tool name. On Cursor and Windsurf, match the server with `action.source` or `action.sources`, not with `action.tool.startsWith("mcp__<server>__")`.

- A remote server gives its host, its port when the URL has one, and the first path segment, such as `mcp.example.com/evil` for `https://mcp.example.com/evil/sse`. A first segment `sse` or `mcp` names the transport, so `https://mcp.example.com/sse` gives `mcp.example.com`. Gryph removes a trailing dot from the host and removes the default port of the scheme (443 for `https`, 80 for `http`). So `https://evil.example.:443/sse` gives `evil.example`.
- A local server gives its program as written. A program with no slash gives its name, such as `github-mcp-server` for `github-mcp-server stdio`. A program path stays whole, such as `/usr/local/bin/github-mcp-server` for `/usr/local/bin/github-mcp-server stdio`, because any directory can hold a program with a trusted name.
- A launcher starts many servers, so Gryph uses the package, module, image or script that it starts. It skips the launcher flags and removes a version, tag or digest. Gryph removes only a version or a tag. For npm that is a dist-tag such as `latest` or a semver range such as `^1.2.0`. For PyPI it is the lower-case `latest` or a PEP 440 version after `@` with no space. Each number in the version must fit in 64 bits, as uv requires. A spaced `name @ X` is always a direct reference. Any other spec stays in the name, so it never matches the plain package name. Examples are an npm file path, archive, URL or alias (`good@.`, `good@x.TGZ`, `good@file:/tmp/x`, `good@npm:evil`) and a PyPI direct reference (`good@good-1.0-py3-none-any.whl`, `good@1evil`, `good @ 1.0`, `good @ git+https://x/good`). The launchers are `npx`, `pnpx`, `bunx`, `npm exec`, `pnpm dlx`, `yarn dlx`, `bun x`, `uvx`, `uv run`, `uv tool run`, `pipx run`, `python -m`, `node`, `deno run`, `docker run` and `podman run`. For example, `npx -y @evil/mcp-server@1.0` gives `@evil/mcp-server`, `python -m mcp_server_time` gives `mcp_server_time`, and `docker run -i --rm -e TOKEN ghcr.io/github/github-mcp-server:v1` gives `ghcr.io/github/github-mcp-server`. A script path stays as written.
- A package flag (`-p` or `--package` for the npm launchers, `--from` or `--spec` for the Python launchers) names the package that holds the command. Any package can hold a command with a trusted name, so the flag value is the name. For example, `npx -p @evil/pkg github-mcp-server` gives `@evil/pkg`, and `uvx --from evil good` gives `evil`. Two or more package flags give the launcher name.

The launcher parse is best effort. When Gryph cannot find the launcher operand, the name is the launcher, such as `npx` or `docker`. A `docker run` flag that Gryph does not know takes a value. A rule that trusts one Cursor server must not trust a launcher name, because each server that the launcher starts has that name.

For a post event, `content_patterns` and the scorer read the tool output. They read each string value and each map key of the output, because a tool such as an MCP server controls its keys. So a rule on a post event matches what the agent received. Gryph keeps at most 1 MiB of the output. Over the cap, each string keeps a fair share, and `action.content_truncated` is true. A rule that needs the full output must handle `content_truncated`.

`action.kind == "observation"` needs a linked pre event. The Gemini, Windsurf and OpenClaw adapters and the Cursor after hooks do not link a post event, so their post events have the kind `action`. To match what the agent received from every agent, use `action.phase == "post"`.

A rule with `action: allow` and `tags` labels an event and does not change the decision, because `allow` has the lowest precedence. The context entry stores the tags of every rule that matched, at any decision. `context.tags_seen` lists the tags of earlier entries, and `context.tag_seq` maps each tag to the sequence of the first entry that has it. A rule cannot see the tags of the event under evaluation. `context.origins_seen` lists the origins of the session, with `mcp:<server>` for MCP. Tags do not make two rules conflict.

A tag that the session does not have is not a key of `context.tag_seq`. Guard the lookup, or use an optional lookup:

```
"secret_read" in context.tag_seq && context.tag_seq["secret_read"] > 3
context.tag_seq[?"secret_read"].orValue(0) > 3
```

In a message template, `{{index .Context.TagSeq "secret_read"}}` gives 0 for a missing tag.

A tag name starts with a lower-case letter, holds lower-case letters, digits, `_` and `-`, and has at most 63 characters. `gryph policy validate` and `gryph policy install` reject any other name. An installed policy with another name still loads with a warning, so an upgrade does not stop your hooks.

`examples/policies/secret-exfiltration.yaml` ships this pattern as an example, not a built-in. Install it with `gryph policy install`. This policy tags a secret read by path or by content, and blocks a network command after it:

```yaml
- id: tag-secret-read
  action: allow
  tags: [secret_read]
  match:
    action_types: [file_read, command_exec]
    file_patterns: ["**/.env", "**/.env.*", "**/*.pem", "**/.aws/credentials"]
    file_access: [read]
- id: tag-secret-content
  action: allow
  tags: [secret_read]
  match:
    action_types: [file_read, command_exec, tool_use]
    content_patterns: ['AKIA[0-9A-Z]{16}', '-----BEGIN [A-Z ]*PRIVATE KEY-----']
  condition: 'action.phase == "post"'
- id: tag-untrusted-input
  action: allow
  tags: [untrusted_input]
  match:
    action_types: [tool_use, network_request]
  condition: 'action.origin in ["web", "mcp"]'
- id: block-egress-after-secret-read
  action: block
  severity: high
  match:
    action_types: [command_exec, network_request, tool_use]
  condition: '"secret_read" in context.tags_seen && size(action.hosts) > 0'
  message: "Blocked: network access after a secret read in this session."
```

`action.human_principal`, `action.service_identity`, and `action.role_scope` carry the AARM R6 identity fields. They are empty strings when capture is disabled or the resolver could not derive a value. See [Identity capture](#identity-capture).

CEL evaluation runs sandboxed with a 100 ms timeout.

### Messages

`message` is a Go `text/template` rendered when the rule matches. Available references:

```
{{.Action.Type}}            {{.Action.Tool}}        {{.Action.Agent}}
{{.Action.Params.Path}}     {{.Action.Params.Command}}
{{.Context.TotalActions}}   {{.Context.FilesWritten}}
{{.Rule.ID}}                {{.Rule.Severity}}
```

The rendered message is delivered to the agent on stderr for block and guidance decisions.

Gryph renders the message two times. The agent and the approval prompt get
the message rendered from the full action. The receipt and the
`error_message` of the stored event get the message rendered from the stored
action. When the logging level or a sensitive path removes the content of the
event, the stored action has no URL, no line counts, no write content, and no
tool-input parameters. So a stored message never holds a value that the
stored event does not keep. For example, at `logging.level: minimal` the rule
message `blocked fetch to {{.Action.Params.URL}}` reaches the agent with the
URL, and the store keeps `blocked fetch to`. The redactor also runs on the
stored `error_message`. A template can fail on the stored action, for example
`{{index .Action.Params.Args 0}}` when the args are removed. Then the store
keeps `rule <id>`, and the decision does not change.

## Decisions

| Decision | What happens | Exit code |
|---|---|---|
| `allow` | Action proceeds | 0 |
| `warn` | Action proceeds, message recorded | 0 |
| `guidance` | Action proceeds, message delivered to agent | 0 |
| `block` | Action refused, message delivered to agent | 2 |
| `escalate` | Action pauses; operator approves or denies via CLI prompt. Approved -> allow. Denied or timed out -> block. See [Approval](#approval-workflow). | 0 or 2 |
| `defer` | Action is blocked at the hook, recorded as a deferral, and queued for operator resolution. Resolution writes a follow-up receipt. Timeout flips the queue row to deny and writes a deny follow-up receipt. Requires a non-empty `reason`. See [Deferrals](#deferrals). | 2 |

When multiple rules match, the most restrictive wins:

```
block > escalate > defer > guidance > warn > allow
```

## Commands

### Authoring and validation

| Command | Purpose |
|---|---|
| `gryph policy init [name\|path]` | Write the fully documented example policy to a target. No argument targets `${ConfigDir}/policy.yaml`. A bare name targets `<name>.yaml` in the policies directory. A path targets that literal file, a candidate for review and install. Use `--force` to overwrite. |
| `gryph policy edit [name\|path]` | Open a policy file in `$EDITOR`. Same target rules as `init`. Scaffolds from the example when the file is missing. A name or the global file validates the merged policy after save. A path validates that file alone. |
| `gryph policy list` (alias `ls`) | List every active source (global file, each policies file, built-ins) with its rule count. A broken file shows an error marker and does not hide the rest. The last line reports the merged total or a conflict. |
| `gryph policy install <path> [--name N] [--force] [--dry-run]` | Validate a candidate file, then copy it into the policies directory so it becomes active. The destination name is the source basename, or `<name>.yaml` with `--name`. Refuses to overwrite without `--force`. `--dry-run` validates and shows the destination without copying. |
| `gryph policy schema` | Print the JSON Schema. Pipe into editor tooling or an AI agent. |
| `gryph policy validate [--file PATH]` | Parse and compile the merged policy, reporting the rule count and sources. With `--file`, validate one file in isolation, without merging the active policy. Use `--file` to lint a candidate before install. |
| `gryph policy test ...` | Dry-run a synthetic action through the merged policy. With `--file PATH`, dry-run against one file plus the built-in rules, to check a draft before install. See `--help` for flags. |

`gryph policy test` accepts `--format json` for machine-readable output.

An agent authors a candidate, then a human promotes it:

```
agent$ gryph policy init ./candidate.yml           # write to an unprotected path
agent$ $EDITOR ./candidate.yml                      # edit the candidate
agent$ gryph policy validate --file ./candidate.yml # lint it
human$ gryph policy install ./candidate.yml         # the human review gate
```

An agent cannot write into `${ConfigDir}/policies/` with its file tools, because the self-protection rules block it. Only a human-run `install` (or a plain copy) places a file there. See the [threat model](./security-policy-threat-model.md) for the limits of this control.

## Verifying a policy

Do these steps each time you change a policy file.

1. Check the syntax.

   ```bash
   gryph policy validate
   ```

   Fix all errors before you continue. `gryph policy edit` runs this automatically when you save a named or global file. Use `gryph policy validate --file <path>` to check a candidate before install.

2. Test each rule with a synthetic action. `gryph policy test` does not touch the database and does not run an agent.

   ```bash
   gryph policy test --action command_exec --command "rm -rf /"
   gryph policy test --action file_write --path /app/prod/config.yaml
   ```

   Add `--file <path>` to dry-run a draft file plus the built-in rules, before you install it.

   A rule on the session context needs a context. `--context-file <yaml>` reads one. Its keys are the `context.*` names, and an unknown key is an error. `--kind` and `--origin` set the action facts, and the output lists the tags of every matched rule:

   ```yaml
   # ctx.yaml
   tags_seen: [secret_read]
   tag_seq: {secret_read: 1}
   entries:
     - {seq: 1, kind: action, action_type: file_read, path: /work/.env}
   ```

   ```bash
   gryph policy test --action command_exec --command "curl https://x.example" --context-file ctx.yaml
   ```

   Test three cases per rule: an action that must match, an action that must not match, and an action near the boundary of the rule.

3. Test with a real agent. Start a session with a hooked agent, do an action that matches your rule, and check that the agent receives the correct block or guidance message.

   WARNING: Keep `policy.fail_mode: closed` during tests. A broken policy then blocks actions instead of allowing them silently. If this locks you out, set `fail_mode: open` temporarily, fix the policy, and set it back.

4. Check the receipts.

   ```bash
   gryph policy receipts --decision block
   gryph policy receipts --verify --all-sessions
   ```

5. Check the context accumulator if your rule uses `context.*` variables. The counters shown here are the same values the CEL conditions see.

   ```bash
   gryph policy context --session <id|prefix>
   gryph policy context --verify --session <id|prefix>
   ```

### Authoring safety rules

1. Always run `gryph policy validate` before you use a policy.
2. Always test a new rule with `gryph policy test` before you test with a real agent.
3. Start new rules with `action: warn` or `enabled: false`. Change to `block` after you check the receipts.
4. Do not use `fail_mode: open` in production.
5. Keep `self_protection` enabled. It stops an agent from changing its own controls.
6. Make `command_patterns` as narrow as possible. Wide patterns cause false blocks.
7. Give each rule a clear `message`. The agent reads this text and changes its behavior.
8. Namespace rule IDs by the file's purpose. IDs must be unique across every policy file.

### Runtime inspection

| Command | Purpose |
|---|---|
| `gryph policy context` | List per-session counters (action counts, tools used, classifications seen). `--session <id\|prefix>` drills into one session and shows its recent entries. |
| `gryph policy receipts` | List receipt rows for mediated actions. `--session`, `--decision`, `--since`, `--until` filter. Pass `--show-hash` to include the per-row hash. |
| `gryph policy receipts --verify` | Recompute the hash chain and verify any signatures. `--session ID` verifies one chain in full; `--all-sessions` verifies every chain. Exits non-zero on break or invalid signature. |
| `gryph policy receipts export` | Stream receipts as JSONL or CSV. `--include-signatures` adds the Ed25519 signature columns. |
| `gryph policy receipts verify-log --input FILE` | Verify an exported chain stand-alone. No database access needed. Verifies signatures when `--trust-store` resolves to a populated store. NOTE: `verify-log` reads a file, not the database. Run `gryph policy receipts export --include-signatures` first, or pipe: `gryph policy receipts export --include-signatures \| gryph policy receipts verify-log --input -`. |
| `gryph policy approve list` | List pending approval requests. CLI prompts run in-process, so this is always empty in the CLI frontend. |
| `gryph policy approve history` | Show receipts whose decision was `escalate`, `approved`, `denied`, or `approval_timeout`. |
| `gryph policy deferrals` | List the pending-deferral queue. `--status` filters to `pending`, `resolved_allow`, `resolved_deny`, `resolved_timeout`, or `all`. `--session ID` scopes to one session. |
| `gryph policy deferrals resolve --id ID --decision allow|deny [--note TEXT]` | Resolve a queued deferral by id (or id-prefix). Writes a follow-up receipt with `deferral_of_sequence` set, emits a `deferral_resolved` self-audit row. |
| `gryph policy deferrals sweep [--dry-run]` | Flip every expired pending deferral to `resolved_timeout`, write a deny follow-up receipt for each, emit `deferral_timeout` per row and a `deferral_sweep` summary. |

### Signing keys

| Command | Purpose |
|---|---|
| `gryph policy keys generate` | Create an Ed25519 keypair, write the private key to `<config dir>/keys/receipt.key` (0600), append the pubkey to the trust store. `--force` rotates the key and records a `receipt_key_rotated` self-audit row. |
| `gryph policy keys list` | List trusted public keys. |
| `gryph policy keys trust --pub FILE` | Add an external public key from a JSON file. Rejects entries whose `key_id` does not match `sha256(pub)[:8]`. |
| `gryph policy keys revoke --key-id ID` | Remove a key from the trust store. The private key file is left in place. |

## Receipts

Every mediated action produces a receipt row in the event store. The default `policy.log_all_evaluations: true` records receipts for `allow` decisions too, which keeps Gryph aligned with AARM's "receipt for every action" requirement. Operators who want the prior behavior (only `block` / `guidance` / `warn` / `escalate` rows) set `policy.log_all_evaluations: false` explicitly. Note that the new default raises per-event storage and signing cost on allow-heavy workloads.

Receipts form a per-session hash chain (`hash`, `prev_hash`). The hash now also covers the SHA-256 of the active policy document (`policy_hash`), so an after-the-fact rule edit is visible at verify time. The chain detects tampering and lets you verify the audit trail off-host.

Receipt rows carry the three identity fields (`human_principal`, `service_identity`, `role_scope`) captured at the mediation boundary. They surface in the `gryph policy receipts --format json` view and in the JSONL and CSV exports. Pre-Phase-6 rows have NULL identity columns and continue to verify cleanly: the hash recipe treats the empty string as the same length-prefixed zero bytes as the insert path.

Signing defaults to `sign_mode: auto`: receipts carry an Ed25519 signature when a key file is present at the configured `key_path`, and skip the signature when no key is on disk. Pick the explicit mode that matches your operational policy:

```yaml
policy:
  receipts:
    sign_mode: auto      # default: sign when a key exists, otherwise unsigned
    # sign_mode: always  # hard-fail at startup if the key is missing
    # sign_mode: never   # skip signing unconditionally
```

The legacy `sign: true` / `sign: false` bool is still accepted as a deprecated alias for `sign_mode: always` / `sign_mode: never`. Each receipt then carries an Ed25519 signature and `signer_key_id`. `gryph policy receipts --verify` walks the chain, recomputes every hash, and verifies signatures against the trust store. `gryph policy receipts export ... | gryph policy receipts verify-log --input -` round-trips the same checks without database access.

### Context chain

The session context log (`context_entries`) carries a per-session hash chain of the same shape as the receipt chain: each entry stores `sequence`, `prev_hash`, `hash`, and `hash_version`. The chain attests to the facts of the entry at mediation time, including the decision, not the post-hook result, so a result update never invalidates the chain. Verify it with:

```
gryph policy context --verify --session <id|prefix>     # one session, full chain
gryph policy context --verify                           # sessions touched by the most recent --limit rows
gryph policy context --verify --all-sessions            # every session in the log
```

`--verify` exits non-zero on any chain break and records a `context_chain_broken` self-audit row. `--format json` emits a machine-readable verdict (`entries`, `chain_breaks`, `summary`).

The upgrade to `context_entries` drops the old `aarm_context_actions` and `aarm_context_states` tables. It first copies the tools, the classes, the entities and the network count of each session, so a session that runs across the upgrade keeps them. The old entry log is lost. Receipts do not reference them, so the receipt chain stays verifiable. The migration never drops a column or an index, so the retired `audit_events.conversation_context` column stays in place, unused.

The chain is not signed today. The receipt chain remains the authenticated audit log. The context chain is for the policy engine to read and for tamper-evidence within the same database.

## Deferrals

A `defer` decision pauses the agent's tool call by blocking it at the hook,
records a `defer` receipt with `result_status=deferred`, and queues a
pending-deferral row that an operator resolves out-of-band (or the timeout
sweep flips to deny). Gryph mediates synchronously on hooks and cannot resume
a previously-blocked tool call, so the agent has to re-issue the action after
resolution if it still wants to perform it.

A defer rule looks like:

```yaml
- id: defer-on-missing-classification
  action: defer
  reason: wait_for_classification
  match:
    action_types: [file_write]
  condition: "size(action.data_classifications) == 0"
```

The `reason` field is required on defer rules and surfaces on the receipt's
`defer_reason` column and in the operator-facing block message returned to
the agent.

### Auto-defer triggers

Two trigger types produce a synthetic defer decision even without an explicit
`action: defer` rule, when the PDP detects insufficient or conflicting input:

- `fresh_session_insufficient_context` fires when a rule's CEL condition
  references context fields that are still zero or empty AND the session is
  younger than `policy.defer.fresh_session_seconds` (default 60). The action
  defers rather than evaluating against an unfilled snapshot. The intent
  fields never trigger it. A session with no intent is a fact, not missing
  data.
- `conflicting_policies` fires when multiple rules match at the winning
  severity tier with materially different rendered messages. Each decision
  lives at its own tier under the precedence scheme, so the practical case
  this catches is two or more rules of the same decision (e.g. two `block`
  rules) firing with different justifications. The PDP normally surfaces the
  first match's message; this trigger surfaces the ambiguity instead. Gated
  by `policy.defer.conflict_triggers_defer` (default true).

Both triggers are gated by `policy.defer.enabled` (default true).

### Resolution and timeout

Operators inspect the queue with `gryph policy deferrals` and resolve with
`gryph policy deferrals resolve --id PREFIX --decision allow|deny`. Each
resolution:

1. Updates the deferral row to `resolved_allow` / `resolved_deny`.
2. Inserts a follow-up receipt in the same session whose
   `deferral_of_sequence` points at the original defer receipt's sequence.
3. Emits a `deferral_resolved` self-audit row.

Timeouts run via `gryph policy deferrals sweep` (also folded into
`gryph retention cleanup`). Any pending deferral whose `expires_at` is past
flips to `resolved_timeout` and gets a deny follow-up receipt. AARM R4
forbids implicit allow on timeout, so the only valid value for
`policy.defer.auto_resolve_on_timeout` is `deny`.

```yaml
policy:
  defer:
    enabled: true
    fresh_session_seconds: 60
    conflict_triggers_defer: true
    timeout_seconds: 600
    auto_resolve_on_timeout: deny
```

The receipt-hash chain stays valid across the deferral lifecycle: the
follow-up receipt is appended at the next sequence with `prev_hash` pointing
at the original defer receipt's hash. `gryph policy receipts --verify`
exercises the entire chain in one pass.

## Approval workflow

A rule with `action: escalate` pauses the agent's tool call and prompts the operator on `/dev/tty` for approve or deny.

```yaml
policy:
  approval:
    mode: cli            # or nop to deny all escalations
    timeout_seconds: 60
    require_note: false
```

The receipt row records the final outcome (`approved`, `denied`, or `approval_timeout`) and the approver identity. Review past decisions with `gryph policy approve history`. If no controlling terminal is available, the request denies; the safe default applies for unattended runs.

## Risk signals

Two heuristic signals populate the action record. Disable either if a custom analyzer fits better.

```yaml
policy:
  classify:
    enabled: true
    fail_open: false
    extra_patterns:
      pii: ["**/customer-list*"]
  injection_score:
    enabled: true
```

`classify` labels paths and URLs. An `extra_patterns` key that is not a built-in class, such as `customer_data`, is a custom label. A condition such as `'customer_data' in action.data_classifications` matches it. It does not become a content label. `injection_score` scans tool calls, post events and prompts for prompt-injection phrases and returns a float between 0 and 1. Each match adds 0.15. A strong phrase (`ignore previous instructions`, `disregard previous`, `forget instructions`) counts up to four matches, so four hits of one strong phrase give 0.6. A weak phrase (`you are now`, `system prompt`, `act as`, `prompt injection`) counts up to two matches, so it adds at most 0.3. All weak phrases together add at most 0.45. Normal text, such as a README for an LLM app, often holds weak phrases, so only a strong phrase can take the score past 0.5. A phrase matches at word boundaries. White space, a Unicode space, a dash, `*`, a backtick, `~`, `&nbsp;`, `&#160;` and `&#xa0;` separate words. `_` separates the words of a strong phrase. It does not separate the words of a weak phrase, so an identifier such as `system_prompt` does not match. Gryph removes each invisible format character (Unicode category Cf, such as a zero-width space or a soft hyphen) and each combining mark (category Mn) before it matches. It also changes common Cyrillic and Greek letters that look like ASCII letters, such as the Cyrillic small letter i (U+0456), to the ASCII letter. One filler word (`all`, `the`, `any`, `your`, `my`, `prior`, `above`) can come between two words, such as `ignore all previous instructions`. The first word can take `s`, `ed` or `ing`, such as `ignoring previous instructions`. The last word can take a common suffix, such as `system prompts` or `disregard previously`. Use them in conditions:

Defer fires automatically on insufficient context (fresh sessions whose
counters have not filled in yet) and on conflicting policies (multiple rules
at the same severity returning different decisions), unless disabled via
`policy.defer.enabled: false`. See [Deferrals](#deferrals).


```yaml
- id: block-secret-network-write
  action: block
  match: { action_types: [network_request] }
  condition: "'secret' in context.classifications_seen"

- id: warn-suspicious-tool-input
  action: warn
  match: { action_types: [tool_use] }
  condition: "action.injection_score > 0.5"
```

### Identity capture

Gryph captures three identity-level fields at the mediation boundary and writes them onto every action and receipt:

- `human_principal`: operator-asserted via `GRYPH_HUMAN_PRINCIPAL` (SSO claim, email, etc.). Falls back to `uid:<N>:<username>` derived from the OS process credentials. On Unix the format is `uid:<N>:<username>`. On Windows (no uid available) it is `user:<username>`.
- `service_identity`: operator-asserted via `GRYPH_SERVICE_IDENTITY`. Otherwise auto-detected for GitHub Actions, Buildkite, GitLab, and CircleCI, with a generic `ci:unknown` fallback when `CI=true` is set.
- `role_scope`: operator-asserted via `GRYPH_ROLE_SCOPE`. Otherwise derived from OS uid/euid/gid plus up to eight supplementary groups.

Capture runs once at process start and is cached. Disable the layer with `policy.identity.enabled: false` to leave the three fields empty.

```yaml
policy:
  identity:
    enabled: true                  # off disables capture entirely
    require_human_principal: false # if true, missing principal denies the action
```

When `require_human_principal: true`, the Mediator blocks any action whose `human_principal` is empty before consulting the PDP. The block message is `Action denied: no verifiable human principal`, a receipt with `decision=block` is still written, and an `identity_missing` self-audit row is emitted. The switch is a silent no-op when `enabled: false` (we cannot enforce what we did not capture).

The OS-derived human principal is a weak proxy. On a developer workstation `uid:501:alice` is the most we know. Real SSO identity requires the operator to set `GRYPH_HUMAN_PRINCIPAL` from the SSO session. Receipts record what was captured. Downstream tooling decides what to trust.

Policies can gate on identity directly:

```yaml
- id: block-prod-without-sso
  action: block
  match: { action_types: [file_write], file_patterns: ["**/prod/**"] }
  condition: "!action.human_principal.startsWith('sso:')"
  message: "Prod writes require SSO identity, got {{.Action.HumanPrincipal}}"
```

### Safe-by-default classification

AARM R2 requires the engine to default to the highest sensitivity level when no classification mechanism produces a result. Gryph honours this by appending the `unknown_sensitive` label to any action the classifier left unlabeled. The label fires in three cases: the classifier is disabled (`classify.enabled: false`), the classifier ran and matched nothing, or the action has no classifiable surface (no path, URL, or content). Policies that gate on classification now fail safe instead of waving the action through.

Rules that match on explicit labels (`'secret' in context.classifications_seen`) are unaffected. Rules can also opt into a paranoid mode by gating on `'unknown_sensitive' in action.data_classifications`.

Operators who explicitly want classification off and do not want the fail-safe label flip `classify.fail_open: true`:

```yaml
policy:
  classify:
    enabled: false
    fail_open: true   # opt out of the AARM safety-net label
```

The default (`fail_open: false`) keeps AARM conformance.

## Worked examples

### Block destructive shell

```yaml
- id: block-rm-rf-root
  action: block
  severity: critical
  match:
    action_types: [command_exec]
    command_patterns:
      - '(?i)\brm\s+-[rf]+\s+(/|~|\$HOME)(\s|$)'
  message: Refusing destructive command {{.Action.Params.Command}}
```

### Refuse writes that leak credentials

```yaml
- id: block-aws-key-in-write
  action: block
  severity: critical
  match:
    action_types: [file_write]
    content_patterns:
      - 'AKIA[0-9A-Z]{16}'
  message: |
    Content for {{.Action.Params.Path}} contains what looks like an AWS
    access key. If this is a fixture, redact it first.
```

### Cap session volume

```yaml
- id: warn-session-write-volume
  action: warn
  severity: low
  match:
    action_types: [file_write]
  condition: "context.files_written >= 25"
  message: |
    This session has written {{.Context.FilesWritten}} files. Consider
    stopping to review the diff before continuing.
```

### Allow docs edits explicitly

```yaml
- id: allow-docs-edits
  action: allow
  tags: [docs]
  match:
    action_types: [file_write]
    file_patterns:
      - "**/*.md"
      - "**/docs/**"
```

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| Rule never matches | Wrong `action_types` or pattern | Run `gryph policy validate` to confirm the file is present and valid (it prints the resolved path). Then `gryph policy test --action <type> --path <path>` and inspect the matched-rule output. |
| Policy fails to load | Syntax or compile error | `gryph policy validate` reports the first compile error with the rule ID and line. Run `gryph policy list` to see which file is broken. |
| All actions are blocked | `fail_mode: closed` and the policy has an error | Run `gryph policy validate` and fix the error. Set `fail_mode: open` temporarily if you are locked out. |
| Policy is enabled but nothing is blocked | Layer disabled or agent not registered | Confirm `policy.enabled: true` and that the agent in question is registered. Check `gryph query --status blocked` to see what was caught. |
| Receipts are unsigned | No key in the keys directory | Run `gryph policy keys generate`. Check `gryph policy keys list`. |
| Duplicate rule ID error | Two rules use the same `id`, possibly in different files | Give each rule a unique ID. Namespace IDs by the file's purpose. |
| `verify-log` asks for `--input` | The command reads a file, not the database | Run `gryph policy receipts export --include-signatures` first. |
| Override a rule for one project | Per-project overlays are not supported yet | Tighten the rule's `scope` (agents, projects, tools) to exclude the project. To turn a rule off, add `disabled: [rule-id]` in the file that defines the rule. |

<details>
<summary><strong>Threat model: receipt keys and trust</strong></summary>

Read this before relying on receipt signatures as evidence outside your own host.

### What signing protects

- **Tamper detection on exported receipts.** A third party with the pubkey can verify a JSONL export came from your host and was not modified in transit.
- **Per-host attribution.** Aggregating receipts from many hosts at one SIEM, each host's signature ties its rows back to that host's key.
- **In-DB tamper by something that does not have key access.** Rare on a single-user host since the key sits in `~/.config/safedep/gryph/keys/` at the operator's UID.

### What signing does not protect

- **The operator deciding to lie.** They hold the key. They can regenerate it and re-sign a forged chain. Self-attestation is unsolvable without an external anchor.
- **Same-UID malware.** Anything running as the operator can read the 0600 key file. We enforce owner check and `O_NOFOLLOW` on read, which blocks symlink swaps and cross-user reads. It does not block code the operator already trusts.
- **Tail truncation.** Drop the last N rows and the prefix still verifies. No external head commitment.
- **Backdating.** `recorded_at` is set by the signer.
- **A compromised agent shaping events before mediation.** Gryph signs what the Mediator saw. The hook runs in-process with the agent.

### Trust roots

1. The private key file at `<config dir>/keys/receipt.key`. Mode `0600`, owner-checked.
2. The trust store at `<config dir>/keys/receipt-pub.json`. World-readable so SOC tools can inspect it. Writable only at filesystem perms; `keys trust` rejects entries whose `key_id` does not derive from their pubkey, but nothing stops you from adding your own freshly generated key.
3. The mediation path itself. The chain attests to what Gryph computed, not to what the agent did downstream.

### When to enable signing

- You export receipts off-host for audit by a separate team. Signing earns its keep.
- You ship receipts into a SIEM that pre-shares a per-host pubkey. Signing earns its keep.
- You run a single-user workstation and only ever read receipts locally. Signing is decoration; the hash chain alone covers tamper-evidence within the DB.

### Hardening beyond this design

- Anchor each session's head (`session_id, last_sequence, last_hash`) to an external append-only log (Rekor, Git, S3 Object Lock) so tail truncation becomes detectable.
- Move the key behind macOS Keychain, Linux Secret Service, or a YubiKey so same-UID malware cannot extract the seed.
- Pin pubkeys at the verifier from an out-of-band channel rather than trusting whatever lands in `receipt-pub.json` on the producing host.

</details>

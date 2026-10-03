# Red-team report

Each bypass of the threat model, tried against the protected file outside the hook path, under each provider, on linux/amd64 with Go go1.25.6, at 2026-10-03T20:48:32Z. `stopped` means the file did not change, `open` means it did, `n/a` means the provider could not run here.

| Bypass | builtin rules | landlock | fanotify | Note |
|---|---|---|---|---|
| script file | open | stopped | stopped | a shell script file appends to the file |
| interpreter | open | stopped | stopped | python appends to the file |
| built-up path | open | stopped | stopped | the path is built at run time |
| archive with absolute member | open | stopped | stopped | tar extracts a member whose name is the absolute path of the file |
| symlink | open | stopped | stopped | a write through a symbolic link elsewhere |
| truncate without open | open | stopped | open | truncate(2) empties the file with no open |
| second agent binary | open | open | stopped | a copy of a shell under another name, started outside any launcher |
| user namespace bind mount | open | stopped | open | a user namespace mounts the attacker's directory over the protected one, and a reader in it sees the attacker's file |
| kill -STOP on the hook | open | open | open | the hook is stopped until the agent times out, and the agent decides alone |

Providers:

- builtin rules: the rules see a mediated tool call only, and a process outside the hook path is not one
- landlock: the bypass runs under gryph run, except the second agent binary, which starts outside the launcher
- fanotify: the watcher marks the managed files, whose mode the suite widens on purpose

## Overhead on the hook path

| Measurement | Runs | p50 | p95 | p99 |
|---|---|---|---|---|
| hook | 30 | 43.6ms | 52.8ms | 67.1ms |
| hook through gryph run | 30 | 56.6ms | 62.4ms | 62.6ms |
| hook under a managed configuration | 30 | 43.3ms | 48.9ms | 52.1ms |
| hook under a managed configuration with fanotify | 30 | 42.8ms | 54.1ms | 55.5ms |

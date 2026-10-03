# The decision service

On a managed host, the hooks of every agent send each action to one
decision service that runs outside the user. The service evaluates the
policy, keeps the audit trail of each account, signs the receipts with a
machine key, and answers the approval requests. The hook process in the
agent's session decides nothing. This page is the guide for an
administrator. The [developer guide](./supervisor-dev.md) has the wire
format and the internals, and the [CLI reference](./cli-reference.md#supervisor-run)
has every flag.

## Topology

```
agent (user A) --> gryph _hook (user A) --+
                                          |  one socket, root-owned path
agent (user B) --> gryph _hook (user B) --+--> gryph supervisor run (service account)
                                          |      users/<uid A>/  store, chain, keys, requests
gryph logs (user A) ----------------------+      users/<uid B>/
```

- The hook runs as the agent's user, reads the payload, and sends it as it
  is. It opens no database and reads no key.
- The service runs as the service account (`_gryph` by default), under
  the service manager, with socket activation on Linux. It knows every
  client from the kernel, by the peer credentials of the connection, never
  from a field of a request.
- The uid of the client is the key of its partition under the state
  directory (`/var/lib/safedep/gryph/users/<uid>/`, mode 0700). A
  partition has its own store, receipt chain, context state, approval
  requests and grants. No request names a partition.
- The read commands of a user (`gryph logs`, `query`, `sessions`, `policy
  receipts` and the others) read through the same socket and see the
  partition of their own account, nothing else.

The service trusts root and the kernel. A root user can stop it, edit the
managed files, and replace the binary. The
[threat model](./security-policy-threat-model.md) says what the service
changes against each adversary.

## Turning it on

`gryph install --managed` installs the service when the managed
configuration sets `supervisor.enabled: true`. The
[managed install guide](./mdm.md) has the contract. The install writes
the configuration with the switch off, makes the service account, the
state directory, the spool root and the machine keys, writes the units,
starts the socket, waits for the service to answer, and only then writes
the switch on. A service that does not answer leaves the switch off, so a
rollout cannot block the agents of a host.

Only the managed file sets these keys. A user configuration cannot point
the hook at a service of its own.

| Key | Default | Meaning |
|---|---|---|
| `supervisor.enabled` | `false` | The hook becomes a client of the service. A socket alone never does. |
| `supervisor.socket` | `/run/safedep/gryph/hook.sock` | The socket. Root owns it and every directory above it. |
| `supervisor.profile` | `enforce` | `enforce` or `pilot`. The profile decides what a blocking hook does when the service is out of reach. |
| `supervisor.pilot_until` | none | The date the pilot ends, `2006-01-02` or RFC 3339. A pilot needs one. After it the host runs `enforce`. |
| `supervisor.state_dir` | `/var/lib/safedep/gryph` | The partitions, the keys and the pid file of the service. |
| `supervisor.spool_dir` | `/var/spool/safedep/gryph` | Where a hook leaves what it could not send. |
| `supervisor.server_identity` | `_gryph` | The account behind the socket that the hook accepts next to root. |
| `supervisor.unavailable.blocking` | `block` | The verdict of a blocking hook when the service is out of reach. |
| `supervisor.unavailable.prompt` | `allow` | The same for a prompt hook. A block there stops the user, not the agent. |
| `supervisor.unavailable.other` | `allow` | The same for a lifecycle or a post-action hook. |
| `policy.approval.*` | see the [approval workflow](./security-policy.md#a-managed-host-with-the-decision-service) | The channels, the floor, the waits, the grant scope and the admin group. |
| `collection.level` | `policy` | `evidence`, `policy` or `full`: what leaves the host for the team. See [the collection level](#the-collection-level). |
| `supervisor.fanotify.enabled` | `false` | Linux: start the [kernel watcher](#the-kernel-watcher) that stops a write to the managed files by a non-privileged account. |
| `supervisor.fanotify.state_file` | `fanotify.json` next to the socket | Where the watcher reports what it protects. |

`gryph doctor --managed --json` reports the profile of the service, the
key scope and whether the host has an approver channel. `gryph doctor`
for a user shows the socket, the profile and the time a pilot has left.

## Fail modes

The hook bounds every wait, so an agent never hangs on Gryph: 300 ms for
the connect and the welcome, 2 s for a decision or the hook timeout of
the agent minus a margin when that is shorter, and the configured
`inline_wait` for an approval prompt, bounded the same way. A hook that
fails never returns an exit code the agent reads as anything but a
block or an allow.

| What failed | Blocking hook | Prompt, lifecycle or post-action hook |
|---|---|---|
| No socket, a refused connect, or no welcome in time | `supervisor.unavailable.blocking`, default `block`, with one line on stderr: "Gryph supervisor is not running. Run `gryph doctor`." | `supervisor.unavailable.prompt` or `.other`, default `allow` |
| After the welcome: a deadline, a rate limit, a refusal, a lost connection, or an answer that is not a decision | `block` | `allow` |
| A decision with a verdict this binary does not know | `block` | `block` |
| The socket is not the system's: a path chain or a peer that fails the check | `block`, in every profile, with "Gryph refused the decision service at <socket>" | `block` |
| The payload does not parse | as no welcome | as no welcome |

A stop of the service with `SIGTERM` drains the open requests first, so a
hook that waits on a prompt gets its answer. A kill cannot drain: the hook
takes the second row.

In the `pilot` profile a blocking hook that cannot reach the service does
not take the `unavailable` verdict. It evaluates the managed policy and
the built-in rules itself, with no store, no context and no signature. A
rule that reads the session context blocks, because the hook has no
context. The decision goes to the spool marked degraded. A pilot past
`pilot_until` runs `enforce`.

Every failure leaves an entry in the spool of the account.

## The spool

The spool is the drop directory of the hooks: `<spool_dir>/<uid>/`. The
service creates the root at start with the set-group-ID bit, the sticky
bit and mode 733, so an account makes its own directory, no account lists
another account's entries, and the service reads them with the group bits
alone. The hook writes one file per failure: the frame the service did
not see, the verdict the hook gave, and the reason.

The service reads the spool once at start and then every minute. One pass
takes at most 256 entries and 64 MiB from one account. An entry becomes an
event in the agent session with the hook's verdict as its result, and a
receipt with the decision `unverified`. The service runs no evaluation on
it and the session counters do not move. A file that is not a regular
file, has another owner, has more than one link, is too large or does not
decode is refused unread and removed. The refused and dropped files become
one `spool_refused` tamper event with the names. Entries that a hook
decided alone while the service was running become one `degraded` tamper
event with the count: that pattern means a hook could not reach a running
service, which a same-user flood causes on purpose.

## The read path

With the service on, the hooks of an account record nothing in the user's
database. The read commands open the socket with the same identity check
as the hook and ask the service, which answers from the partition of the
account that the kernel reports for the connection. A user keeps access
to their own log on a managed host and sees no other account. Without the
service the commands fail with the socket path in the error.

`gryph policy context --window` reads a local database and is not
available through the service. The interactive `gryph query` searches a
local index and stays on the local database. `gryph cost --sync` reads
the transcripts as the user and sends the totals to the service.

Before a host gets the service, the hooks record into a database in the
account's home. `gryph supervisor import` carries it into the service, as
the account. The reconcile job runs it once. Imported rows keep the
account's own signatures and show the `imported` marker.

## Keys and receipts

The service signs every receipt with the machine key, in the `supervisor`
scope. The key lives at `<state_dir>/keys/receipt.key`, owned by the
service account, mode 0600. Root never reads it. `gryph install
--managed` and `gryph supervisor keys rotate` put the public half in the
managed trust store at `<managed dir>/keys/receipt-pub.json`, root-owned,
so every verifier on the host trusts it. A receipt that a user key signed
keeps the `user` scope, and an audit never reads one as the other. See
[key custody](./security-policy.md#key-custody).

`gryph supervisor keys rotate` writes a new key, keeps the old public
half in the trust store, and reloads the running service with `SIGHUP`.
Run it on a schedule or after an incident.

## Approvals

The service answers every `escalate` decision. It stores the request in
the partition of the account, asks on the terminal of the hook when the
rule accepts the developer's own answer, and otherwise waits for a member
of the admin group, who answers with `gryph policy approve resolve`. On a
host with polkit the service asks for the password of the approver. Every
answer and every use of a grant is on the receipt chain, with the
channel, the assurance, the approver and the trust of the connection. The
[approval workflow](./security-policy.md#approval-workflow) has the
channels, the floor, the grants and
[the trust of a connection](./security-policy.md#the-trust-of-a-connection).

## The collection level

The managed configuration sets how much of each event leaves the host for
the team, with `collection.level`. Only the managed file sets it. A user
configuration cannot raise or lower it, and a host without a managed
configuration collects nothing.

| Level | What leaves | Export profile |
|---|---|---|
| `evidence` | The receipts and the facts of each action, with every content value digested. | `metadata` |
| `policy` | The fields that rules match on: the command with its URLs stripped, the paths, the tool, the agent, the rules. Prompts and content digested, every secret dropped. The default. | `policy` |
| `full` | Every value. A team opts in. | `full` |

No cloud target exists yet, so the target is `none` and nothing leaves the
host. The level says what the export profile would send. `gryph doctor`
shows the level, the profile and the target on a managed host, and `gryph
doctor --managed --json` reports them under `collection`.

The decision service tells the developer once: the first hook after the
managed file sets a level, and the first hook after each change, carries
one line as guidance, "Collection level policy is in force on this host.
Your log stays readable with gryph logs." A managed file that sets no
level gives no notice. The change is on the self-audit log of the account
as `collection_level`. The service reads the level at start, so a change
reaches the accounts after the next restart. The local log stays readable
by the developer at every level.

## The units

On Linux the install writes two systemd units:

- `/etc/systemd/system/gryph-supervisor.socket` holds the socket at
  `supervisor.socket`, root-owned, mode 0666, so every account connects.
  The root-owned path chain is what a hook verifies before it speaks.
- `/etc/systemd/system/gryph-supervisor.service` runs `gryph supervisor
  run` as the service account, with `Restart=always`, no new privileges,
  no capability, a read-only system except the state directory and the
  spool root, no access to any home, private `/tmp` and devices, Unix
  sockets only, the `@system-service` call filter and umask 0077.
  `ExecReload` sends `SIGHUP`, the key reload.

The socket stays open across a restart or an upgrade of the service, so a
hook that connects in that window waits for the welcome instead of taking
the absent-service verdict. `gryph uninstall --managed` turns the switch
off first, then stops and removes the units. macOS and Windows have no
service installer yet.

## The kernel watcher

On Linux, `supervisor.fanotify.enabled: true` in the managed configuration
adds a second process, `gryph supervisor fanotify`, in its own unit
`gryph-fanotify.service`. It runs as the service account with two
capabilities and nothing else: `CAP_SYS_ADMIN` for the fanotify
permission group, `CAP_SYS_PTRACE` to read the open flags of the process
that asks. It is not part of the decision service and talks to nobody.

The watcher marks the managed directory and its files, the `policies` and
`keys` directories, the binary the hook entries name, and the managed hook
file of every agent with a locked entry. When a process opens one of them,
the kernel holds the open until the watcher answers. A read passes. An
open for a write, a truncate or a create by a process that is not root
and not the service account is refused with "Operation not permitted",
whatever the mode of the file says. So a managed file whose chain became
writable by mistake stays out of the user's reach, and a user namespace
that mounts over the path is still the limit of the access checks, not of
the watcher.

The watcher writes a state file next to the socket,
`/run/safedep/gryph/fanotify.json`, readable by every account: its pid,
the paths it marks, the count of opens it refused, and the last changes it
noticed. `gryph doctor` and the reconcile pass read it: while the watcher
lives, the `binary`, `policy` and `config` rows, and the `hook_config` row
of an agent with a locked entry, read `prevent_same_user` with the
provider `fanotify`, and a host with no weaker row earns the `locked`
profile. A watcher that is gone reports nothing, and the rows fall back to
what the built-in rules give.

Known limits:

- Root can stop the watcher and write anything. The watcher does not
  defend against root, and `gryph doctor` shows when it is gone.
- A rename or an unlink has no permission event in the kernel. The
  watcher records it in the state file with the account that made it,
  marks the file at the path again when one exists, and `gryph doctor`
  shows the change as the drift of the row until the watcher restarts.
- An open with `O_CREAT` creates the entry before the kernel asks, so a
  refused create leaves an empty file behind.
- The watcher reads the open flags from `/proc/<tid>/syscall` of the
  thread that opens. A 32-bit process, or a process that opens with
  `openat2`, gives no flags, and the watcher refuses its open, read or
  write: the protection fails closed. An exec of a marked file passes:
  the kernel opens it for a read.
- A `truncate(2)` on a path has no permission event. The watcher records
  it as a change it could not stop. The mode of a managed file is what
  stops it, as for a rename.
- A process in another mount namespace that reaches the file through a
  bind mount is seen, because the mark is on the inode. A copy of the file
  in another place is not the protected file.

## What the service logs

The service logs operational facts only: an account by uid, a request by
its short id, an agent name, a frame type, and errors. It never logs the
content of an event: no command, no path, no prompt, no payload. The
content goes to the store of the partition, under the logging level of
the managed configuration, and leaves the host only through an export
with an [export profile](./content-labels.md#export).

## Limits

- The service protects the decision and the audit trail from a process of
  the agent's user. It does not stop root, and it does not stop an agent
  that works outside the hook path: a direct write, a network call, a
  tool with no hook.
- A hook that cannot reach the service takes the `unavailable` verdict of
  its column. A same-user process can take the service out of reach for
  its own account, which is why a blocking hook blocks by default.
- The trust of a connection is a signal from the process tree, not proof.
  The [threat model](./security-policy-threat-model.md#bypass-catalog)
  lists the known bypasses with their status.

# The wire format of the decision service

The hook client runs as the agent user. The decision service runs outside
the user. They talk over a local socket with the frames in `decision/ipc`.
This page is the reference of those frames. The service itself and the
client mode of the hook come in later changes.

## Framing

A frame is a 4-byte big-endian length and a JSON document of that length.
The document is an envelope: `{"type": "<name>", "body": {...}}`. A frame
above 8 MiB is refused before any byte of the body is read, and a frame of
length zero is refused. Both end the connection with one `error` frame,
because the stream is out of step after a refused length.

Every string field has a bound, and every string must be UTF-8. A body
that fails a bound gets `error` with code `invalid`. A frame type the
receiver does not know gets `error` with code `unsupported`. In both cases
the connection goes on with the next frame. Unknown fields in the envelope
and in a body are ignored, so a newer peer can add fields.

## Handshake

The first frame of a connection is `hello`. The server answers `welcome`.
A client that gets no `welcome` within its budget treats the service as
down. A first frame of another type ends the connection with `error`.

| Frame | Direction | Body |
|---|---|---|
| `hello` | client to server | `proto` (int, the protocol version, 1), `client_version` (string, 64), `caps` (up to 32 strings of 64) |
| `welcome` | server to client | `proto`, `server_version` (64), `mode` (64: `enforce`, `pilot`, or `none` when no service decides) |

## Frames after the handshake

| Frame | Direction | Body | Answer |
|---|---|---|---|
| `handle` | client to server | `agent` (64), `hook_type` (64), `raw_payload` (base64 of the agent payload, up to 6 MiB), `project_claim.name` (256), `cost_totals` (the session cost the hook side collected), `inline_wait` (nanoseconds the client can wait for a prompt, zero for none) | `decision`, or `prompt` and then `decision`, or `error` |
| `decision` | server to client | `decision` (64), `reason` (4096), `guidance` (4096). The fields of the hook response. | |
| `prompt` | server to client | `nonce` (64), `action_digest` (128), `deadline` (RFC 3339), `request_id` (64), `view` (what the terminal shows: `agent`, `tool`, `type`, `path`, `command`, `url`, `message` (4096 each), `rules` (up to 64), `wait`) | `prompt_reply` |
| `prompt_reply` | client to server | `nonce`, `decision` (`approve`, `deny` or `none`), `note` (4096) | the `decision` of the `handle` |
| `report_hook_error` | client to server | `agent`, `hook_type`, `raw_size`, `raw_event` (up to 64 KiB), `message` (4096). The fields of the hook error. | `ack` |
| `query` | client to server | `kind` (64), `params` (up to 64 entries, 64 and 1024), `limit` | `query_result` or `error` |
| `query_result` | server to client | `rows` (up to 64 JSON documents), `next` (1024) | |
| `session_cost` | client to server | `session_id`, `cost_totals` (the session cost the client collected from the transcript) | `ack` or `error` |
| `import_events` | client to server | `session_id`, `events` (up to 64 events of the user's own database) | `import_result` or `error` |
| `import_receipts` | client to server | `session_id`, `receipts` (up to 64 receipts, with their hashes and signatures) | `import_result` or `error` |
| `import_session` | client to server | `session` (the session row, sent first) | `import_result` or `error` |
| `import_result` | server to client | `taken` (the count of rows the server stored) | |
| `approve` | client to server | `request_id`, `decision` (`allow` or `deny`), `scope` (64), `note` (4096). The answer of an approver to a request of another account. | `approve_result` or `error` |
| `approve_result` | server to client | `request_id`, `state` (`approved`, `denied` or `superseded`), `assurance`, `grant_id`, `note` | |
| `ack` | server to client | empty | |
| `error` | server to client | `code` (64), `message` (4096) | |

The error codes: `unsupported`, `invalid`, `rate_limited`, `deadline`,
`unauthorized`, `internal`. A client maps a code it does not know like
`internal`.

## What the server trusts

Nothing in a frame. `handle` carries the raw agent payload and the server
parses it with its own adapter registry, so no parsed event travels on the
wire. The agent name, the hook type, the project claim and the cost totals
are claims the server records as claims. The server knows the client from
the kernel, through the peer credentials of the socket, never from a frame.

The server never opens a path that a claim names. The working directory
and the transcript path of an event are strings to it. The hook side alone,
as the agent user, detects the project in the working directory
(`utils/projectdetection`) and reads the transcript for the cost
(`agent/claudecode/transcript`). Two checks keep it so:

- `supervisor/architecture_test.go` fails when a file of the package
  imports one of those packages, `hookside`, `cli`, `core/cost` or
  `os/user`, and when `utils/projectdetection`, `hookside`,
  `agent/claudecode/transcript` or `cli` appears anywhere in the dependency
  tree of the package.
- The `forbidigo` rule in `.golangci.yml` refuses a raw `os.Open`,
  `os.OpenFile`, `os.Create`, `os.ReadFile`, `os.WriteFile`, `os.Stat`,
  `os.Lstat`, `os.ReadDir`, `os.Mkdir` and `os.MkdirAll` in `supervisor/`
  outside a test. The package opens every file through `platform/nofollow`
  on a handle of the state directory: each component is opened relative to
  the one before it, a link is refused, and the kernel checks the type on
  the open.

## The client's reading of a verdict

A `decision` whose `decision` names a verdict this binary does not know
blocks, as a hook response with an unknown verdict blocks today. A missing
`welcome`, a deadline, a rate limit and a server error each map to the
verdict the fail mode of the hook names.

## The server

`gryph supervisor run` is the service. It takes its socket from the
service manager when it was started with socket activation (`LISTEN_FDS`),
else it opens `--socket` in the foreground. It runs as the service account,
never as root. `--allow-root` exists for a test.

The accept loop only accepts, reads the peer credentials from the kernel
(`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` and `LOCAL_PEERPID` on macOS) and
counts the connection. Every connection runs on its own goroutine, so a slow
request never holds the next connection. On Linux the service also takes a
`pidfd` on the peer, so a later check of the process cannot land on another
process that took the same pid.

The uid the kernel reports is the tenant key. The partition of an account
lives at `<state dir>/users/<uid>/`, mode 0700, with its own store (`audit.db`), receipt
chain and context state. No field of a request names a partition. One mutex
per partition serializes the writes, so the chain stays in order without a
lock on the database. The state directory must exist before the server
starts: `gryph supervisor run` creates it when it is missing, and the
service unit owns it on a managed host. The server opens it once and
creates `users/` and `users/<uid>/` relative to that handle. A link in
place of either refuses the partition, and the connection gets `error
internal`.

Limits per account, with the defaults: 16 open connections, 20 requests a
second with a burst of 40, and a 30 s idle timeout per connection. The
newest connection above the limit gets `error rate_limited` and closes. A
request over the rate gets `error rate_limited`. The first refusal in a
minute becomes a tamper event `rate_limited` in the system session of the
account, so a review sees the flood without the flood filling the store.

The hidden `gryph supervisor send --socket <path> [--text] [--idle N]`
sends the frames on stdin to a service and prints the replies. With
`--idle` it first opens that many connections that send hello and hold.
The acceptance scripts under `supervisor/` use it.

## Approvals

The service answers an `escalate` decision itself: the partition's
mediator gets `supervisor.approver` as its approval service in place of
the terminal prompt or the nop of a user install. The approver keeps the
requests and the grants of the account in the partition store
(`storage.ApprovalStore`) and decides each request once:
`ResolveApprovalRequest` applies to a pending row only, so the first
answer wins by the clock of the store and a later one gets
`ErrApprovalRequestDecided`.

The inline prompt runs on the connection of the hook. The serve loop is
in the handler while the prompt is open, so the prompter writes `prompt`
and reads the next frame itself, with the read deadline of the prompt
plus one second of grace. One nonce, one connection, one reply: a
`prompt_reply` that reaches the loop gets `error invalid`, because no
prompt is open there, and a reply with another nonce counts as no answer.
When the hook closes first, the prompter goes with the connection and the
request stays pending. The partition releases its write mutex for the
wait and takes it again for the outcome, so the other tool calls of the
session go on.

The client sets the wait: `min(policy.approval.inline_wait, hook timeout
minus 500 ms)` in `handle.inline_wait`, zero under two seconds. The
server bounds it again by its own `inline_wait`. The whole exchange of
the hook ends before the hook timeout of the agent.

A request expires at `request_ttl`. Every `handle` and every read of the
approval kinds closes the overdue requests of the partition first, and
the spool pass closes them for every open partition. An expiry sets
`approval_timeout` on the receipt and `system:expiry` as the approver. The
next `handle` of the session appends the outcomes nobody reported yet to
its answer: an allow becomes guidance, so the agent reads the lines.

A grant (`aarm_approval_grants`) binds to the partition, the action
digest (`approval.ActionDigest`: type, tool, operation, agent, the clean
working directory, path, command, args and URL), the session and the
scope. `MatchApprovalGrant` runs before a new request. An answer from the
terminal stores no grant.

The local admin answers with `approve` (`Server.approve`). The server
finds the request across every partition under `users/`, then checks the
peer: in the group of `policy.approval.local_admin.group`
(`account.MemberOf`, cached 30 s), not the uid of the partition that
holds the request, and the login identity (`peercred.Peer.LoginIdentity`,
`/proc/<pid>/loginuid` on Linux) against the one stored on the request
from the hook's connection. The same identity, or none, is
`self-elevated`, refused unless `allow_self_elevated`, which also lets it
meet a `local-admin` floor. A refusal is `error unauthorized` and a self-
audit row `approval_refused` in the requester's partition. The answer
resolves the request under the write lock of that partition, stores the
grant on an allow that is not a review item, updates the receipt
(`approved` or `denied`, plus the `approval` record), and audits
`approval_granted` or `approval_denied`. `ErrApprovalRequestDecided`
answers `superseded` and audits `approval_superseded`. The approval query
kinds take `all_accounts` in the filter, or `all=1` for a prefix lookup,
and the server honors it for a member of the group only.

A `handle` of a hook after the action (`Action.Phase` is not `pre`) makes
a review item: `review` on the row, no prompt, no wait, the note as
guidance, and no grant on an allow.

`platform/localauth` is the authority of the platform behind an
`Authorizer` interface: `Available` and `Authorize(subject, action,
interactive)`. On Linux it is polkit over the system bus
(`github.com/godbus/dbus/v5`): `CheckAuthorization` with a `unix-process`
subject (pid and start time from `/proc/<pid>/stat`), the action
`io.safedep.gryph.approve`, `AllowUserInteraction`, and a cancellation id
that a timed-out call (90 s) cancels. The install writes the action file
(`localauth.PolicyFile`, `auth_self`) and the uninstall removes it. The
server asks when `Available` (cached 30 s) and the peer passed the group
and uid checks: an authorized answer at `local-admin` becomes
`local-auth`; anything else refuses unless `allow_without_auth`. The
client's `resolve` runs `localauth.Agent` (`pkttyagent --process <pid>
--notify-fd`) across the `approve` call, with a 2 min client deadline.
Tests give the server a fake `Authorizer` through `Options.Authorizer`.
The acceptance condition `[polkit]` needs the system bus socket,
`pkcheck` and `pkttyagent`.

## Trust

`Server.agentAncestor` walks the parents of the peer (`procs.Ancestors`,
`/proc/<pid>/stat` field 4 on Linux) while the peer is still the process
seen at accept (`peercred.Peer.SameProcess`, by the start time read at
accept), and returns the nearest process of the peer's uid whose name an adapter
declares through `ProcessNamer`. A process of another uid in the chain
is skipped. `partition.trustFor` compares it with the
`agent_process` of the session (`name:pid:start`): the first agent seen
binds the session, the same agent is `agent`, another agent or none while
the bound one lives is `low`, and none with no binding is `unknown`. A
bound agent that is gone (`procs.StartedAt`) releases the binding. The
trust travels on `HookRequest.PeerTrust`, which the service sets and never
reads from the hook side, to `Event.PeerTrust`, the `peer_trust` column of
the event and the receipt, and `approval.Request.PeerTrust`. The decision
service turns a low-trust intent into an observation before the context
sees it. The approver skips the prompt and raises the floor to
`local-admin` for a low-trust request, and `Server.approve` refuses an
answer from under an agent. Tests give the server a process tree through
`Options.Ancestors` and `Options.Processes`.

## The units

On Linux the service runs under systemd with socket activation
(`platform/service`). `gryph-supervisor.socket` holds the socket at
`supervisor.socket`, root-owned, mode 0666, backlog 1024: every account
connects, and the root-owned path chain is what a hook client verifies.
`gryph-supervisor.service` runs `gryph supervisor run` as the service
account with `Restart=always` and the hardening set: `NoNewPrivileges`, an
empty capability set, `ProtectSystem=strict` with the state directory
(`StateDirectory=safedep/gryph`) and the spool root (`ReadWritePaths`) as
the only writable paths, `ProtectHome=yes` (the service cannot open a home
even by mistake), `PrivateTmp`, `PrivateDevices`, the kernel and cgroup
protections, `RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID`,
`LockPersonality`, `MemoryDenyWriteExecute`,
`RestrictAddressFamilies=AF_UNIX`, `SystemCallFilter=@system-service` and
`UMask=0077`. `ProtectProc` stays off: the peer check reads `/proc/<pid>`.
`ExecReload` sends `SIGHUP`, the key reload. The socket stays open across
a restart or an upgrade of the service, so a hook that connects in that
window waits for the welcome instead of taking the absent-service
fallback.

`gryph install --managed` controls the order: the configuration goes in
with `supervisor.enabled` off, then the service account (the package
post-install script makes it too), the state directory, the spool root
and the keys, then the units. The command waits up to 10 seconds for the
service to answer a hello over the socket, with the identity check of a
hook, and only then writes the configuration as the administrator gave
it, with the switch on. A service that does not answer leaves the switch
off and the run partial, so a rollout cannot block the agents of a host.
`gryph uninstall --managed` turns the switch off first, then stops and
removes the units. macOS (launchd) and Windows have no service installer
yet.

## Keys

The service signs with the machine key, never with a user's key. A key in
a user's home is one that same-user malware may have read, so the service
does not import it: the receipts a user key signed keep their signatures
and their `user` scope, and the trust store keeps the public key.

| Key | Path | Owner and mode |
|---|---|---|
| Receipt signing key | `<state dir>/keys/receipt.key` | the service account, 0600 |
| The published public halves, the current key last | `<state dir>/keys/receipt-pub.json` | the service account, 0644 |
| Export key | `<state dir>/keys/export.key` | the service account, 0600 |
| Trust store | `<managed dir>/keys/receipt-pub.json` | root, 0644 |

`gryph install --managed` makes the keys as root and hands them to the
service account (`engine.EnsureMachineKeys`, `engine.TrustMachineKey`).
A private key that exists is never read by root: the key id comes from
the published file. The service makes the keys itself at start when they
are missing, as the service account, and then only root can put the
public half in the trust store: `gryph supervisor keys rotate` carries
every published half, the old key's included, into the trust store on its
way, so the receipts of a key that no install published still verify. Every
partition signs with the machine key in the `supervisor` scope and with
`sign_mode: always` (`engine.PartitionConfig`). The managed file alone can
name another `policy.receipts.key_path`. The hook client never reads a
key.

`gryph supervisor keys rotate` runs as root: a new key under the state
directory, the public half appended to the trust store, the files handed
to the owner of the state directory, and `SIGHUP` to the pid in `<state
dir>/supervisor.pid`. On the signal the service retires every partition
(`Server.Reload`): a partition with no connection closes at once, one with
connections closes with its last, and the next contact of the account
opens a fresh partition that reads the new key. The old key signs nothing
after that, and its public half stays in the trust store.

## Reads

On a managed host with the service on, the hooks record nothing in the
user's database, so the read commands (`logs`, `query`, `sessions`,
`session`, `cat`, `diff`, `stats`, `cost`, `policy receipts`, `policy
approve history`, `policy deferrals list`) read through the service. The
CLI opens the socket with the same identity check as the hook
(`App.InitReadStore`), and the service answers every `query` from the
partition of the account that the kernel reports for the connection. No
field of a request names a partition, and the service serves nothing but
reads: `storage.ReadStore` holds the methods those commands use, and
`storage/remote` implements it over `query` frames. The full
`storage.Store` never goes over the socket.

The `kind` of a `query` names one method, and `params` carries its
arguments: `id`, `prefix`, `filter` (the filter of the local call as
JSON), `after` and `limit` for the follow read, and `page`. The service
runs the same filter as a local read would and answers in pages of 64
rows: `next` names the next page, and the client asks for it until `next`
is empty. The kinds: `event`, `event_by_prefix`, `events`, `count_events`,
`session_events`, `events_after`, `session`, `session_by_prefix`,
`sessions`, `receipts`, `receipt_session_ids`, `deferred_actions`,
`deferred_action_by_prefix`, `context_state_by_prefix`. A kind the
service does not know gets `error invalid`. Every read counts against the
rate limit of the account.

`gryph cost --sync` reads the transcripts as the user, as the hook does,
and sends the totals in a `session_cost` frame. The service stores them on
the session of its own partition, marked `client_reported`, and refuses a
session that is not there. The interactive `gryph query` searches a local
index and stays on the local database. The self-audit rows that a failed
verification writes go to the local store only.

## Import

Before a host gets the service, the hooks of an account record into a
database in the account's home. `gryph supervisor import` carries it into
the service, as the account: root never opens a user's database. The
command reads the sessions, the events and the receipts of the local
database and sends them in `import_events`, `import_receipts` and
`import_session` frames, up to 64 rows each. The session row goes first,
because the events and the receipts reference it. The service skips a row
that is already there and counts it as zero, so a run that stopped gets
completed by the next one. The reconcile job of the account runs the import once
on a managed host, and a marker file in the data directory
(`import.done`) ends later runs before they start. `--force` runs it
again.

The service stores the rows in the partition of the peer account with
`imported` set on the session, the events and the receipts, because the
account could have changed them before the import. Imported rows never
touch the accumulator, and a session the service recorded itself refuses
them. A receipt keeps its hash, its signature and its key: the chain is
the one the user's database held, in the `user` scope, and it verifies
against the account's own trust store, which stays in the home. `sessions`,
`query` and `policy receipts` print the marker.

## The spool

The spool is the drop directory of the hook clients. `gryph supervisor run`
creates the root (`supervisor.spool_dir`, `--spool-dir`) when it is missing,
with the set-group-ID bit, the sticky bit and mode 733: an account makes its
own directory under it, the directory takes the group of the root, and no
account lists or removes another account's entries. On a managed host the
packaging gives the root to the service account, so that group is the
service account's. The client makes `<root>/<uid>/` with mode 770 and
writes each entry as `<unix nanoseconds>-<pid>.json` with mode 640, through
a temporary name and a rename. The service reads and removes the entries
with the group bits alone, as the service account, with no privilege.

An entry is one JSON object: `recorded_at`, `kind` (empty for an action the
client decided alone, `tamper` for a finding of the client), `verdict`,
`reason` and `frame` (the `handle` frame the service did not see).

The service reads the spool once at start and then every
`--ingest-interval` (default 1 m). One pass runs as follows, per account
directory:

- The owner of the directory, read from its open handle, is the account.
  A directory named for another uid still feeds the partition of its
  owner.
- Every file is opened relative to the directory handle with no-follow
  and no wait, so a link or a FIFO cannot send the pass elsewhere or hold
  it. The kind, the owner and the link count come from the open
  descriptor: a file that is not regular, has another owner, has more than
  one link, is over the file cap (the frame cap plus 64 KiB) or does not
  decode as an entry is refused unread. The frame of an entry passes the
  same bounds as a frame on the wire.
- The pass takes at most `--spool-max-files` entries (default 256) and
  64 MiB from one account. The entries past that are removed unread and
  counted as dropped.
- A file the pass took, refused or dropped is removed. A file the
  partition could not record stays for the next pass.

An entry with a `handle` frame becomes an event in the agent session, with
the client's verdict as its result, and a receipt with the decision
`unverified` and the message "client verdict <verdict> without the decision
service: <reason>". The service runs no evaluation on it, and the
accumulator never sees it: a spooled action changes no context counter. A
`tamper` entry becomes a `server_identity` tamper event in the system
session of the account. The files a pass refused or dropped become one
`spool_refused` tamper event with the names and the reasons. Entries that
the client decided alone while the service was running, by their
`recorded_at`, become one `degraded` tamper event with the count: that
pattern means the client could not reach a running service, which a
same-user flood causes on purpose.

## The client

A managed configuration with `supervisor.enabled: true` turns the hook into
a client. A socket alone never does, and a user configuration cannot point
the hook at a service of its own. In client mode the hook reads the
payload, opens no store, reads no key, and parses the payload only for the
claims that the agent user alone can make: the project of the working
directory and, on session end, the cost totals of the transcript. It sends
the raw payload and renders the answer.

The client bounds every wait. Connect plus welcome has 300 ms. A decision
has 2 s, or the agent's hook timeout minus 500 ms when that is shorter. The
client never hangs past the agent's timeout and never returns an exit code
that is not a block.

What a failure gives depends on the fail-mode column of the hook
(`HookSpec.FailColumn`: `blocking`, `prompt`, `other`) and on where it
failed:

| Failure | Blocking hook | Prompt or other hook |
|---|---|---|
| No socket, refused connect, no welcome in time (`ErrConnect`) | `supervisor.unavailable.blocking`, default `block` with the line "Gryph supervisor is not running. Run `gryph doctor`." | `supervisor.unavailable.prompt` or `.other`, default `allow` |
| After welcome: deadline, rate limit, server error, a frame that is not a decision | `block` | `allow` |
| A decision with a verdict this binary does not know | `block` | `block` |
| The payload does not parse | as `ErrConnect`, and the client reports the error to the service when it can | as `ErrConnect` |

Before hello the client checks that the socket is the system's. The socket
path and the expected service account come from the managed configuration
only (`supervisor.socket`, `supervisor.server_identity`, default `_gryph`).
Two checks, both from the kernel and the file system and never from the
peer: root owns the socket and every directory above it, with no directory
writable by group or other (a sticky directory passes), and the peer of the
socket runs as root or as the service account. With socket activation the
peer is the service manager, so the path check is the one that proves the
socket is the system's. A failure blocks every hook in every mode, prints
"Gryph refused the decision service at <socket>: <reason>", leaves a tamper
entry in the spool, and never triggers the fallback: a same-user process
that binds a socket of its own gets a block, not an allow.

Every failure leaves an entry in the spool of the account
(`supervisor.spool_dir`, default `/var/spool/safedep/gryph/<uid>/`): the
frame the service did not see, the verdict the client gave, and the reason.
An allow there is an action the service records later, marked as
unverified. The [spool](#the-spool) section has the layout and the pass.

Only the managed file sets `supervisor.unavailable.{blocking,prompt,other}`
(`block` or `allow`).

In the `pilot` profile (`supervisor.profile: pilot`, with
`supervisor.pilot_until`, the date the pilot ends), a blocking hook that
gets `ErrConnect` does not take the `unavailable` verdict. It runs the
local-ephemeral evaluation instead: the managed policy and the built-in
rules, in process, with no store, no accumulator, no receipt and no
signature (`engine.NewEphemeralEvaluator`). Every rule whose condition
reads `context.*` blocks (`pdp.WithDegraded`, `aarm.WithDegraded`), because
the hook has no context and a condition over zeros would let through what
a counter should stop. The gate is that flag, never the accumulator type:
the Nop accumulator is the default and `gryph policy test` runs on it.
`gryph policy test --degraded` shows the same evaluation. A managed policy
that does not load blocks. The decision goes to the spool with `kind:
degraded`, and the service records it as an unverified receipt and counts
it for the `degraded` tamper event like any entry decided without it. The
managed policy files must be readable by every account for this mode: the
hook runs as the agent user. Prompt and lifecycle hooks keep the
`unavailable` verdict, allow by default. After `pilot_until` the host runs
`enforce`; `gryph doctor` shows the time the pilot has left.

The hidden `gryph supervisor fake --socket <path> [--no-welcome] [--reply
<frame-file>]` is a service that misbehaves on purpose, for the acceptance
scripts under `hook/client/`.

## Testing

`gryph supervisor protocol [--text]` is a hidden command that runs the
connection loop over stdin and stdout with no decision service behind it:
`hello` gets `welcome`, every other frame gets `error` with
`unsupported`. With `--text` each reply is one JSON line. The acceptance
script `ipc/protocol/unknown-frame` uses it. `FuzzDecode` in
`decision/ipc` runs in the nightly fuzz job:

```bash
go test -run '^$' -fuzz=FuzzDecode -fuzztime=10m ./decision/ipc/
```

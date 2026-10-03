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
| `handle` | client to server | `agent` (64), `hook_type` (64), `raw_payload` (base64 of the agent payload, up to 6 MiB), `project_claim.name` (256), `cost_totals` (the session cost the hook side collected) | `decision` or `error` |
| `decision` | server to client | `decision` (64), `reason` (4096), `guidance` (4096). The fields of the hook response. | |
| `prompt` | server to client | `nonce` (64), `action_digest` (128), `deadline` (RFC 3339) | `prompt_reply` |
| `prompt_reply` | client to server | `nonce`, `decision` (64), `note` (4096) | `ack` or `error` |
| `report_hook_error` | client to server | `agent`, `hook_type`, `raw_size`, `raw_event` (up to 64 KiB), `message` (4096). The fields of the hook error. | `ack` |
| `query` | client to server | `kind` (64), `params` (up to 64 entries, 64 and 1024), `limit` | `query_result` or `error` |
| `query_result` | server to client | `rows` (up to 64 JSON documents), `next` (1024) | |
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

## The client's reading of a verdict

A `decision` whose `decision` names a verdict this binary does not know
blocks, as a hook response with an unknown verdict blocks today. A missing
`welcome`, a deadline, a rate limit and a server error each map to the
verdict the fail mode of the hook names.

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

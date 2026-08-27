# Gryph Policy Threat Model

This document states what the Gryph policy layer defends. It states how Gryph
combines policy. It states the limits. Read it with the user guide in
[security-policy.md](./security-policy.md).

## What Gryph is

Gryph applies a policy layer to AI coding agents. It mediates an agent's actions
through hooks. It checks each action against a policy. It returns one decision:
allow, warn, guidance, defer, escalate, or block. It writes a signed receipt for
each mediated action.

Gryph is a control on the agent. It is not a control on the host.

## What Gryph does not defend

Gryph runs as the same operating-system user as the agent. Gryph trusts that
user, the host, and the runtime. Gryph does not defend against:

- A compromised runtime or operating system.
- A root adversary. Malware that runs as the same user and tries to disable
  Gryph.
- An agent that runs any process as the same user to write files directly,
  outside the mediated tool path.

These are outside the trust boundary. The built-in self-protection rules make
tampering harder. They are a same-user control. A determined same-user adversary
can defeat them. The [hardened deployment](#genuine-gaps-and-hardened-deployment)
section lists the lower-level controls that close these gaps.

## Actors

- **Operator.** The human who owns the host and writes policy. Trusted.
- **Agent.** The AI coding agent. Not trusted to change policy. A prompt-injected
  agent can try to weaken enforcement.
- **Reviewer.** The human who installs a candidate policy file with `gryph policy
  install`. Trusted.

## Policy resolution

Gryph resolves policy from three sources, in order. The first is the global file
`${ConfigDir}/policy.yaml`. The second is the directory
`${ConfigDir}/policies/*.yaml`. The third is the built-in self-protection rules.
Both user sources sit inside the config directory. Gryph resolves no other
location. Gryph never loads a file at any other path as policy.

The built-in `gryph-builtin-protected-files` rule protects the config directory.
It blocks an agent's mediated `file_write` or `file_delete` into that directory.

## How decisions combine

The engine checks every rule that matches. It keeps the decision with the
highest precedence:

```
block > escalate > defer > guidance > warn > allow
```

Two properties follow. Both keep policy safe.

1. **An allow can never override a block.** `allow` has the lowest precedence. An
   allow rule cannot lower a block from another rule. The order of the files does
   not change this. Precedence decides, not position.
2. **A new rule can only raise a decision, never lower it.** A new file adds a
   block, a warn, or an allow. Its allow cannot weaken a block from another file.
   So a new policy file cannot weaken policy. A bad file can only block too much.
   A block is the safe result. `gryph policy list` and the receipts show it.

The built-in rules load last. They have the highest precedence. No user rule can
override them.

## The built-in floor cannot be disabled

`disabled:` removes rules. Two limits keep it from weakening the floor:

- `disabled:` acts only on the file that declares it. It removes only rules from
  the same file. A file cannot disable a rule from another file.
- A user file cannot name a built-in rule ID. The `gryph-builtin-` prefix is
  reserved. `disabled:` never acts on the built-in source.

The operator turns the built-in rules off only with
`policy.self_protection.enabled` in the config file. It is an all-or-nothing
switch. The config file sits inside the protected config directory.

## Same-file disabled scope

`disabled:` acts only on the file that declares the rule. This matches the file
an author or an agent edits. One installed file cannot remove a rule from another
file. It cannot remove a rule from the operator's global file. An installed file
only adds rules. This keeps the review step below simple.

## The review step for candidate files

An agent can write a candidate policy file at an unprotected path with `gryph
policy init <path>`. The file is a candidate. Gryph never resolves it as policy.
A human installs a reviewed candidate into `${ConfigDir}/policies/` with `gryph
policy install`.

`install` first validates the candidate alone. Then it validates the merged
result that would follow the install. A file can be valid alone but break the
merge, for example with a duplicate rule ID or a reserved prefix. `install`
refuses such a file before it copies it.

The step has a hard part and a soft part:

- **Hard, by design.** The agent's own file tools cannot write into
  `${ConfigDir}/policies/`. The built-in file rule blocks the mediated write.
- **Soft, same-user.** `gryph policy install` writes into the directory as the
  Gryph process. It is not a mediated tool call. An agent that runs shell
  commands as the same user can run `install` itself. This is the same-user limit
  from above. The design does not close it. Every `install` writes a self-audit
  row. The install is visible after the fact.

A block always beats an allow. `disabled:` acts only on one file. So a policy
that an agent installs can only add rules. It cannot lower the built-in floor. It
cannot remove the operator's rules. The remaining risk is over-blocking. A block
is the safe result.

## Genuine gaps and hardened deployment

Gryph alone cannot close the same-user gaps. A hardened deployment adds a
lower-level control that protects Gryph's own surfaces, for example an EDR agent
or a kernel mechanism.

- **Policy Enforcement Point (the hooks).** The agent's hook configuration sits
  in a user-writable location. A same-user process can edit it to remove or
  redirect the Gryph hook. It does not touch policy to do this. Gryph's
  self-protection catches only the mediated write. A kernel or EDR control that
  protects the hook configuration files closes this gap. Gryph cannot close it on
  its own.
- **Policy Decision Point (the binary).** The decision runs in the user-side
  `gryph` binary. A same-user adversary can patch the binary. It can put another
  binary earlier on `PATH`. It can preload code into the binary. The fix is to
  install the `gryph` binary under root ownership on a protected location. The
  same kernel or EDR control that protects the hooks protects the binary. One
  mechanism covers both the hooks and the binary.

A lower-level control that protects the hooks and the binary closes these gaps.
The same-user adversary can no longer unhook Gryph. It can no longer change the
decision. The policy files in the config directory get the same protection. Until
then, Gryph makes tampering harder for a same-user adversary. It does not stop a
determined one.

## Related hardening

The receipt design lists more controls that make audit-trail tampering harder.
Anchor each session head to an external append-only log. Move the signing key
behind a platform keystore. Pin verifier public keys from a separate channel. See
the receipt sections of [security-policy.md](./security-policy.md).

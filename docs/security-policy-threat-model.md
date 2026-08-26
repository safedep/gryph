# Gryph Policy Threat Model

This document states what the Gryph policy layer defends, how it combines
policy, and where its limits are. Read it with the user guide in
[security-policy.md](./security-policy.md).

## What Gryph is

Gryph applies a policy layer to AI coding agents. It mediates an agent's
actions through hooks. It evaluates each action against a policy and returns a
decision: allow, warn, guidance, defer, escalate, or block. It records a signed
receipt for each mediated action.

Gryph is a control on the agent. It is not a control on the host.

## What Gryph does not defend

Gryph runs as the same operating-system user as the agent. It assumes that
user, the host, and the runtime are honest. Gryph does not defend against:

- A compromised runtime or operating system.
- A root adversary, or malware that runs as the same user with intent to
  disable Gryph.
- An agent that runs an arbitrary process as the same user to write files
  directly, outside the mediated tool path.

These are outside the trust boundary. The built-in self-protection rules raise
the cost of a lazy tamper attack, but they are a same-user soft control. A
determined same-user adversary can defeat them. The
[hardened deployment](#genuine-gaps-and-hardened-deployment) section lists the
lower-level controls that close these gaps.

## Actors

- **Operator.** The human who owns the host and authors policy. Trusted.
- **Agent.** The AI coding agent. Untrusted with respect to policy. A
  prompt-injected agent may try to weaken enforcement.
- **Reviewer.** A human who promotes a candidate policy file into the active
  set with `gryph policy install`. Trusted.

## Policy resolution

Policy resolves from three sources, in order: the global file
`${ConfigDir}/policy.yaml`, the directory `${ConfigDir}/policies/*.yaml`, and
the built-in self-protection rules. Both user sources sit inside the config
directory. Gryph resolves no other location. A file at any other path is never
loaded as policy.

The config directory is protected by the built-in `gryph-builtin-protected-files`
rule. An agent's mediated `file_write` or `file_delete` into it is blocked.

## The combining model is monotone

The engine evaluates every matching rule and keeps the highest-precedence
decision:

```
block > escalate > defer > guidance > warn > allow
```

Two properties follow, and they are the core of the policy security argument:

1. **An allow can never override a block.** `allow` is the lowest tier. A rule
   that allows an action cannot lower a block from another rule. Order between
   files does not change this. Precedence decides, not position.
2. **Adding a rule can only raise a decision, never lower it.** A new file can
   add a block, a warn, or an allow. Its allow cannot weaken an existing block.
   So adding a policy file is non-weakening. The worst a bad additive file can
   do is block too much. That fails in the safe direction, and it is visible in
   `gryph policy list` and in receipts.

The built-in rules load last and carry the top tier. No user rule can shadow
them.

## The built-in floor cannot be disabled

`disabled:` removes rules. Two limits keep it from weakening the floor:

- `disabled:` is scoped to the file that declares it. It removes only rules
  defined in the same file. A file cannot disable a rule from another file.
- A user file cannot name a built-in rule ID. The `gryph-builtin-` prefix is
  reserved, and the built-in source is never subject to a `disabled:` list.

The only switch for the built-in rules is `policy.self_protection.enabled` in
the operator's config file. It is a coarse, all-or-nothing control. The config
file is itself inside the protected config directory.

## Same-file disabled scope

`disabled:` reaches only the file that declares the rule. This matches the unit
an author or an agent sees. It also keeps one installed file from removing a
rule that another file, or the operator's global file, established. An installed
file can add rules. It cannot subtract from what another file set. This makes
the review gate below safe to reason about: a promoted file only adds.

## The review gate for candidate files

An agent can author a candidate policy file at an unprotected path with
`gryph policy init <path>`. The file is a candidate. It is never resolved as
policy. A human promotes a reviewed candidate into `${ConfigDir}/policies/`
with `gryph policy install`.

`install` validates the candidate alone, then validates the merged result that
would follow the install, so a file that is valid on its own but breaks the
merge (a duplicate rule ID across files, a reserved prefix) is refused before it
is copied.

The gate has a hard part and a soft part:

- **Hard, by construction.** The agent's own file tools cannot write into
  `${ConfigDir}/policies/`. The built-in file rule blocks the mediated write.
- **Soft, same-user.** `gryph policy install` writes into the directory as the
  Gryph process, not as a mediated tool call. An agent that can run shell
  commands as the same user can run `install` itself. This shares the same-user
  limit above. It is not closed by construction. Every `install` writes a
  self-audit row, so a promote is at least visible after the fact.

Because the combining model is monotone and `disabled:` is same-file scoped, a
policy an agent manages to install can only add rules. It cannot lower the
built-in floor and it cannot remove the operator's rules. The residual risk is
over-blocking, which fails safe.

## Genuine gaps and hardened deployment

Gryph alone cannot close the same-user gaps. A hardened deployment adds a
lower-level control, for example an EDR agent or a kernel mechanism, that
protects Gryph's own surfaces.

- **Policy Enforcement Point (the hooks).** The agent's hook configuration
  lives in a user-writable location. A same-user process can edit it to remove
  or redirect the Gryph hook, without touching policy. Gryph's self-protection
  catches only the mediated write. A kernel or EDR control that protects the
  hook configuration files closes this gap. Gryph cannot close it on its own.
- **Policy Decision Point (the binary).** The decision runs in the user-side
  `gryph` binary. A same-user adversary can patch it, shadow it earlier on
  `PATH`, or preload code into it. The path is to install the `gryph` binary
  under root ownership on a protected location, guarded by the same kernel or
  EDR control that protects the hooks. One mechanism covers both the hooks and
  the binary.

With the hooks and the binary protected by a lower-level control, the same-user
adversary can no longer unhook Gryph or forge a decision, and the policy files
in the config directory gain the same protection. Until then, the honest
statement is that Gryph raises the cost of tampering for a same-user adversary
but does not stop a determined one.

## Related hardening

The receipt design lists further controls that raise the cost of tampering with
the audit trail: anchor each session head to an external append-only log, move
the signing key behind a platform keystore, and pin verifier public keys out of
band. See the receipt sections of [security-policy.md](./security-policy.md).

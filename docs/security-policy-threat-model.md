# Gryph Policy Threat Model

Gryph applies a policy layer to AI coding agents. It mediates each agent action
through hooks and returns a decision: allow, warn, guidance, defer, escalate, or
block. It writes a signed receipt for each mediated action. This document states
what that layer defends and where its limits are. Read it with
[security-policy.md](./security-policy.md).

## Trust boundary

Gryph is a control on the agent, not on the host. It runs as the same
operating-system user as the agent, and it trusts that user, the host, and the
runtime. Gryph does not defend against:

- A compromised runtime or operating system.
- A root adversary, or malware that runs as the same user to disable Gryph.
- An agent that runs any process as the same user to write files directly,
  outside the mediated tool path.

The built-in self-protection rules make tampering harder, but they are a
same-user control. A determined same-user adversary can defeat them. The
[gaps](#gaps-and-hardened-deployment) section lists the lower-level controls that
close this.

## How policy resolves and combines

Gryph resolves policy from three sources: the global file
`${ConfigDir}/policy.yaml`, the files in `${ConfigDir}/policies/`, and the
built-in self-protection rules. Both user sources sit inside the config
directory. Gryph loads no file at any other path.

The engine keeps the decision with the highest precedence:
`block > escalate > defer > guidance > warn > allow`. Two properties follow:

- An allow can never override a block. Precedence decides, not file order.
- A new file can only add rules. It cannot lower a decision another rule makes,
  so it cannot weaken policy. The worst it can do is block too much, which is the
  safe direction.

The built-in rules load last and always apply. No policy file can remove them: a
user file may not use the reserved `gryph-builtin-` prefix, and `disabled:` acts
only on rules in the file that declares it. The operator turns the built-in rules
off only with `policy.self_protection.enabled` in the config file, an
all-or-nothing switch.

## Candidate review

An agent cannot write into the config directory. The built-in file rule blocks
its mediated `file_write` there. So an agent drafts a candidate at an unprotected
path, and a human installs it with `gryph policy install`. This is the review
gate.

The gate is hard against the agent's file tools, but soft against a shell: an
agent that runs commands as the same user can run `install` itself. This is the
same-user limit above. Every install writes a self-audit row, so it is visible.
Because a new file can only add rules, an installed file cannot remove the
built-in rules or the operator's rules. The remaining risk is over-blocking, the
safe direction.

## Gaps and hardened deployment

Gryph alone cannot close the same-user gaps. A hardened deployment adds a
lower-level control, for example an EDR agent or a kernel mechanism, to protect
two surfaces:

- The hooks, which are the enforcement point. The agent's hook configuration sits
  in a user-writable location. A same-user process can edit it to unhook Gryph,
  without touching policy.
- The binary, which is the decision point. The decision runs in the user-side
  `gryph` binary. A same-user adversary can patch it, put another binary earlier
  on `PATH`, or preload code. Install the binary under root ownership on a
  protected path.

The same lower-level control protects both surfaces, and the config-directory
policy files gain the same protection. Until then, Gryph makes tampering harder
for a same-user adversary, but does not stop a determined one.

The receipt design lists further controls against audit-trail tampering. See the
receipt sections of [security-policy.md](./security-policy.md).

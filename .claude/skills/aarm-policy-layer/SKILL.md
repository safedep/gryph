---
name: aarm-policy-layer
description: Use when working on Gryph's AARM security layer or policy engine. Trigger this whenever the user changes code under aarm/ or cli/policy.go, or works on policy evaluation, the PDP, policy rules, CEL conditions, receipts and the receipt hash chain, receipt signing, the context accumulator, approvals, deferrals, identity capture, data classification, injection scoring, or the self-protection rules. Also trigger on phrases like "policy engine", "policy decision point", "AARM", "receipt chain", "gryph policy", "policy rule", or "context accumulator", even when the user does not name a file.
---

# AARM / Policy Layer

The full, current guide is `docs/aarm-dev.md`. It is the single source of truth for this task. Read it and follow it step by step.

Do not change the AARM layer from memory. The layer spans about 15 packages under `aarm/`, plus the wiring in `cli/policy.go`. The request flow chains a normalize step, an identity gate, the context accumulator, the PDP, a decision branch, and a receipt write. A change in one step can break another. The doc maps the flow, the packages, and the extension points.

## How to use this skill

1. Read `docs/aarm-dev.md` in full.
2. Use the package map to find the right package for your change.
3. Add a Mediator dependency through a `MediatorOption` in `aarm/check.go`, then wire it in `loadPolicyMediator` in `cli/policy.go`.
4. Run `make test` and `make lint` before you finish. Run `make generate-schema` after any change to the policy schema or a payload type.

## Invariants

Three invariants break silently if you miss them. The doc explains all three.

1. The receipt and context hashes are consensus formats. A change to field order or canonicalization breaks every existing chain. Update the verifier and the property tests in the same change.
2. The `aarm` package stays decoupled from `storage` and `cli`. Carry CLI-shaped side effects through the hooks (`DeferralHook`, `ApprovalAuditHook`, `IdentityAuditHook`). Do not add a `storage` or `cli` import to `aarm`.
3. Policy resolution is additive and same-user safe. `disabled:` is scoped to the file that declares the rule, block always beats allow, and no user file can disable a built-in. Keep these when you touch `Loader.Load` or the merge, so an installed policy file can never weaken the built-in floor or another file's rules. See `docs/security-policy-threat-model.md`.

## Keep the doc correct

The doc is the source of truth, so keep it accurate. If you find the code no longer matches the doc, update `docs/aarm-dev.md` in the same change. Do not fork the guidance into this skill.

## Keep the user-facing policy doc fresh

`docs/security-policy.md` is the user-facing guide for policy authors. Whenever you make a UX-related change in the policy layer, update it in the same change. UX-related changes include:

- New, renamed, or removed `gryph policy ...` commands or flags (init, edit, list, install, validate, test, receipts, context, deferrals, approve, keys).
- Changes to the policy YAML surface: rule fields, match criteria, CEL variables, decisions, or the `disabled:` mechanism.
- Changes to configuration keys under `policy:` in the config file, or to their defaults.
- Changes to default behavior a user can observe: sign modes, fail modes, retention, auto-defer triggers, approval prompts, block or guidance message format.
- Changes to file locations the user interacts with: `policy.yaml`, the `policies/` directory, the keys directory, the trust store.

Keep `docs/aarm-dev.md` for internals and `docs/security-policy.md` for the user surface. A change that touches both layers updates both docs.

# Lens: Closed World

**What it is.** A constraint that *increases* creativity: the best new ideas use resources
**already inside the problem's world** — the existing system, its immediate environment, things
already at hand — rather than importing new external components. Constraint forces you to see
latent uses in what you already have. Pairs tightly with SIT.

## Procedure

1. **Draw the boundary.** List everything already inside Orbit's world (see
   `references/orbit-context.md`): SPIFFE attestation, OPA PDP/PAP + PEP, Envoy/Istio edge, the
   identity-proxy sidecar, the inference gateway, the run control plane, the operator,
   Tenant/Platform CRDs, the OTel trace/audit stream, the delegation/identity model, existing
   tenant data, the **credential broker pattern** (GitHub App + passthrough adapters, egress
   control). Include the *empty-but-committed* stubs (the MCP/tool gateway, the event spine) —
   they're inside the committed world too. (Verify state against the code; this list goes stale.)
2. **For the problem, forbid new external primitives.** Ask: "Solve this using *only* what's
   already listed." A new third-party service, a new protocol, a new data store = out of bounds
   for this pass.
3. **Find latent uses.** Which existing component, used differently, already contains the
   solution? The trace stream is also an audit spine. The identity-proxy is also the credential
   injection point. The policy decision is also a UX signal ("you'd need scope X").
4. Write each idea as: existing resource → reused for new benefit → dual-user value → Who/What/How.

## Orbit micro-example

Problem: "agents need to discover which tools they're allowed to call." Closed-World move: the
PEP (`policy-gateway`) already knows the allow-list for every agent at decision time — so the
*same* policy data, exposed as a read endpoint, becomes a "what can I do?" capability for the
agent. No new service, no new source of truth. Benefit (agent): self-describing permissions.
Benefit (human): one place defines and reveals access. Advances **What can it access?**

## Failure modes

- **Reaching outside the world.** The moment the idea needs a brand-new external system, you've
  left Closed World — note it and move it to a different lens, don't pretend it's Closed-World.
- **Confusing "no new code" with "no new external primitive."** Closed-World allows building —
  it forbids importing *new categories of thing*. Reusing Envoy in a new way is in-bounds.
- **Stopping at the first reuse.** Push every component for a latent second use.

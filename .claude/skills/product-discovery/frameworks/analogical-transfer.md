# Lens: Analogical transfer

**What it is.** Steal structure from a mature domain that already solved a structurally-similar
problem. Agent governance is young; the problems it faces (identity, authorization, secrets,
auditing, multi-tenancy, edge enforcement) are *ancient* in adjacent domains. Map their patterns
onto agents and you inherit decades of hard-won design.

## Procedure

1. **Abstract the Orbit problem** to its structural shape, stripped of "agent". ("A
   non-deterministic principal needs scoped, revocable, audited access to resources it doesn't
   own, enforced by something it can't bypass.")
2. **Find source domains with that shape** and study how they solved it (use the dossier /
   `context7` for current specifics):
   - **Kubernetes RBAC + ServiceAccounts** — identity, roles, bindings, namespaces as tenancy.
   - **Cloud IAM** (AWS/GCP) — roles vs. policies, assume-role / STS short-lived credentials,
     permission boundaries, session tags → human→agent delegation analog.
   - **Service mesh** (Istio/Envoy) — sidecar enforcement the workload can't bypass; mTLS identity.
   - **OAuth / OIDC** — delegation, scopes, token exchange (RFC 8693) → on-behalf-of an agent.
   - **CI/CD & secrets managers** — OIDC federation instead of stored secrets; ephemeral creds.
   - **Database / capability security** — row-level security, capability tokens, object capabilities.
3. **Transfer the pattern, then test the disanalogy.** What's *different* about agents that breaks
   the borrowed pattern? (Agents are non-deterministic and may be adversarially steered — so
   "trust the workload after auth" from service mesh must be *tightened*, not copied.)
4. Output: source pattern → Orbit translation → what changes because the principal is an agent →
   dual-user value.

## Orbit micro-example

Source: AWS STS `AssumeRole` — a principal exchanges its identity for short-lived, scoped
credentials it never stores. Transfer: human delegates to agent → agent's call triggers a
just-in-time scoped credential exchange at the edge, never held. Disanalogy: the agent may be
prompt-injected, so the scope must be *narrower than* and *bounded by* the human's, and every
exchange audited to the human — strengthening the delegation model rather than copying it.

## Failure modes

- **Surface analogy.** Borrowing vocabulary ("roles!") without the mechanism. Map the *structure*.
- **Ignoring the disanalogy.** The agent's non-determinism is usually the thing that makes a naive
  copy unsafe — always state what you tightened.
- **Single-source fixation.** Triangulate 2–3 domains; the overlap is the robust pattern.

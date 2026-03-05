# 0009. Agent Execution Flow Patterns & Delegation

**Status:** Accepted
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Defining how authority flows between principals when agents execute — covering delegated, unassisted, and human-consulted patterns — and the token format that carries delegation context through the platform.

---

## Context

Orbit agents do not always act under their own authority alone. An agent may be acting under explicit authority granted by a human, it may be operating fully autonomously under its own registered permissions, or it may need to pause and escalate a decision to a human when it encounters something outside its authorised scope.

These are structurally distinct patterns with different identity implications. The platform must be able to express which pattern is in effect on any given request, because policy evaluation, audit records, and credential brokering all depend on this context.

### Background

ADR-0008 established the unified `Principal` model and the platform session token. Every actor in the platform — human or agent — is represented as a Principal and authenticated via a platform session token. What ADR-0008 does not cover is what happens when one principal acts on behalf of another, or when an agent needs to bring a human into its decision loop.

The three execution flow patterns Orbit must support are:

1. **On Behalf Of (Delegated)** — A human grants an agent authority to act within a scope derived from the human's own permissions. The agent carries a delegation chain that makes the human's authorisation explicit and auditable.
2. **Unassisted** — The agent acts entirely under its own registered principal, with no human delegation context. Policy applies the constraints appropriate for autonomous operation.
3. **Human Informed / Consulted** — The agent encounters a decision outside its authorised scope and escalates upward to a human, pausing its own execution until a response is received. The human's response creates a bounded delegation event.

### Problem Statement

We need to define:
1. The token format used to carry delegation context through the platform
2. How each execution flow pattern is represented in the identity layer
3. What the policy engine receives in each pattern so it can apply appropriate constraints
4. How delegation events are recorded for audit purposes
5. How delegated authority is revoked

### Goals

- **Auditability**: The full delegation chain — who authorised what, for what scope, expiring when — must be visible in every request context
- **Policy integration**: The policy engine receives a consistent structure regardless of flow pattern, with delegation context present when applicable and explicitly absent when not
- **Standards-based**: Adopt existing standards for delegation tokens rather than inventing a bespoke format
- **Bounded authority**: Delegated authority must always be explicitly scoped and time-limited — an agent cannot receive broader authority than the delegating human possesses
- **Revocability**: Delegation grants must be revocable before their natural expiry

### Non-Goals

- Defining escalation path configuration (which human or group an agent escalates to — this is a runtime/registration concern deferred for later)
- Implementing the policy engine itself (this ADR defines the contract the policy engine consumes)
- Defining UI or API flows for human approval of escalations (product-layer concern)
- Covering machine-to-machine delegation between services (out of scope for initial implementation)

---

## Decision Drivers

* **RFC compliance**: Prefer established standards to bespoke token formats — reduces implementation risk and improves interoperability
* **Policy engine contract**: The delegation context must be legible to OPA policy rules in a consistent structure
* **Audit completeness**: Delegation chains must be fully reconstructable from the event log without additional lookups
* **Least privilege**: Delegated tokens must be strictly scoped — no ambient authority, no permission escalation
* **Operational simplicity**: Delegation grants should be manageable by operators without deep cryptographic knowledge

---

## Considered Options

### Option 1: Bespoke delegation chain token format

**Description:** Design a custom JWT structure for delegation chains — e.g. a `delegation_chain` claim containing an array of `{principal_id, scope, expiry}` objects.

**Pros:**
- Fully tailored to Orbit's data model
- No dependency on external standards or libraries

**Cons:**
- Reinvents a solved problem — delegation token semantics are well-specified in RFC 8693
- Interoperability risk if Orbit ever needs to accept delegations from external systems
- Increases the implementation and security review burden

### Option 2: OAuth 2.0 Token Exchange — RFC 8693

**Description:** Adopt RFC 8693 (OAuth 2.0 Token Exchange) as the mechanism and format for delegation chain tokens. RFC 8693 defines a standard token exchange endpoint and token structure with explicit `act` (actor) and `may_act` claims designed for delegation and impersonation scenarios.

The `act` claim is a nested JSON object identifying the actor performing the exchange — it is chainable, enabling multi-hop delegation to be represented faithfully:

```json
{
  "sub": "{delegating-human-principal-id}",
  "act": {
    "sub": "{agent-principal-id}"
  },
  "scope": "write:deployments read:config",
  "exp": 1234567890,
  "tenant_id": "platform-team"
}
```

**Pros:**
- Established standard with well-defined semantics for actor, subject, and scope
- The `act` claim natively supports chained delegation (agent acting on behalf of human who was delegated to by another human)
- Widely understood in the industry — reduces onboarding friction for engineers familiar with OAuth
- Orbit's token exchange endpoint becomes a standard-compliant implementation

**Cons:**
- RFC 8693 is designed with OAuth 2.0 authorization servers in mind — Orbit is not a full OAuth AS, so the implementation is a subset
- Some RFC 8693 fields are not applicable to Orbit's model (e.g. `resource` indicator, `audience` restrictions beyond the platform)

### Option 3: SPIFFE federation for delegation

**Description:** Express delegation as a SPIFFE identity with a modified SPIFFE ID path (e.g. `spiffe://orbit.acme/tenant/t1/agent/bot/on-behalf-of/{human-id}`).

**Pros:**
- Keeps everything in the SPIFFE identity model

**Cons:**
- SPIFFE IDs are identifiers, not authority tokens — they carry no scope, expiry, or permission information
- This conflates identity (who is the workload?) with delegation (what authority has been granted?) — the wrong abstraction
- Would require SPIRE to be involved in every delegation event — operationally inappropriate

---

## Decision

**Chosen Option:** Option 2 — OAuth 2.0 Token Exchange (RFC 8693) for delegation chain tokens.

### Rationale

RFC 8693 is designed precisely for the delegation and impersonation patterns Orbit needs. The `act` claim solves the chained delegation representation problem correctly. Adopting an established standard avoids reinventing token semantics and reduces the security review surface. The subset of RFC 8693 required for Orbit's use case is small and well-understood.

---

## The Three Execution Flow Patterns

### Pattern 1: On Behalf Of (Delegated)

A human principal explicitly grants an agent authority to act within a bounded scope derived from the human's own permissions. The grant is time-limited and explicitly scoped.

**How it works:**

1. The human authenticates and holds a valid platform session token (ADR-0008)
2. The human initiates a delegation grant via Orbit's delegation API, specifying:
   - The agent principal ID being delegated to
   - The scope of authority being granted (a subset of the human's own permissions)
   - The expiry of the delegation grant
3. Orbit validates that the requested scope does not exceed the human's own permissions (no escalation)
4. Orbit issues a **delegation token** — an RFC 8693-structured JWT with:
   - `sub`: the human principal ID (whose authority is being delegated)
   - `act.sub`: the agent principal ID (who is acting under that authority)
   - `scope`: the explicitly granted scope
   - `exp`: the delegation expiry
   - `tenant_id`: tenant scope
   - `jti`: unique token ID for revocation
5. The agent attaches the delegation token to requests via the `X-Orbit-Delegation-Token` header — this is the one identity-layer concern the agent workload is explicitly aware of, because delegation is task-specific context tied to what the agent is doing, not who it is
6. The Envoy sidecar injects the platform session token (`Authorization: Bearer`) transparently — the agent workload does not manage or attach its own platform token
7. The policy engine sees the full delegation chain and evaluates against the combined context

**What the policy engine receives:**

```json
{
  "principal": {
    "id": "{agent-id}",
    "type": "AGENT",
    "tenant_id": "platform-team",
    "identity_source": "ATTESTED"
  },
  "delegation": {
    "present": true,
    "delegator": {
      "id": "{human-id}",
      "type": "HUMAN"
    },
    "scope": ["write:deployments", "read:config"],
    "expires_at": "2099-01-24T15:00:00Z"
  },
  "operation": "deployment.create"
}
```

**Audit record:** Every request in a delegated context records both the agent principal and the delegating human principal. The delegation token `jti` is recorded so the grant event can be correlated.

**Constraints:** An agent acting under delegation cannot exceed the granted scope, even if the agent's own registered permissions would otherwise allow it. The delegation scope is an upper bound on what the agent may do in this context. The agent also cannot re-delegate to another agent without explicit platform support for chained delegation (not in initial scope).

---

### Pattern 2: Unassisted

The agent acts entirely under its own registered principal with no human delegation context. This is the appropriate pattern for scheduled tasks, background processing, and operations the agent has been explicitly registered and approved to perform autonomously.

**How it works:**

1. The agent's Envoy sidecar handles the full authentication flow transparently: the SPIRE sidecar manages the SVID lifecycle, and Envoy exchanges the SVID for a platform session token via `POST /identity/exchange`, caches it, and injects it on all outbound requests. The agent workload is not involved in this process (see ADR-0010).
2. The agent makes plain HTTP calls — no token is attached by the agent workload. No delegation token is presented.
3. The policy engine evaluates the request against the agent's own registered permissions

**What the policy engine receives:**

```json
{
  "principal": {
    "id": "{agent-id}",
    "type": "AGENT",
    "tenant_id": "platform-team",
    "identity_source": "ATTESTED"
  },
  "delegation": {
    "present": false
  },
  "operation": "config.read"
}
```

The explicit `delegation.present: false` is important — the policy engine should not treat the absence of a delegation field as an error or ambiguity. Unassisted operation is a valid, expected, and common pattern. Policy rules may apply stricter constraints when delegation is absent, reflecting the higher autonomy burden on the agent's own registered permissions.

**Provenance requirement:** An agent approved for unassisted operation must have been registered by a human principal. That registration event — who approved this agent for autonomous operation, with what scope — is recorded as a principal action and is the human accountability anchor for unassisted flows.

---

### Pattern 3: Human Informed / Consulted (Escalation)

The agent encounters a decision or operation that falls outside its authorised scope, and rather than failing or proceeding, it pauses execution and escalates to a human for guidance or approval. This is the inverse of delegation — authority flows upward, not downward.

**How it works:**

1. The agent, operating in either delegated or unassisted mode, encounters an operation it is not permitted to perform unilaterally
2. The agent raises an escalation event via Orbit's escalation API, specifying:
   - The operation it wants to perform
   - The context it has (why it believes the operation is necessary)
   - The escalation target (a human principal or group — configuration of this target is deferred, but it must be pre-configured)
3. Orbit records the escalation event and notifies the escalation target
4. The agent's execution is suspended pending a human response
5. The human reviews the escalation, authenticates, and responds with either approval (and optionally a scope) or rejection
6. If approved, Orbit creates a **bounded delegation grant** — structurally identical to a Pattern 1 delegation token — scoped specifically to the requested operation, with a tight expiry
7. The agent's execution resumes with the bounded delegation token in context
8. The human's approval is recorded as a principal action — the human is accountable for what the agent does under that grant

**Identity implications:**

The escalation itself is an authenticated action — the human responding must present a valid platform session token, and their principal must match the expected escalation target. Orbit validates this before accepting the response.

The bounded delegation token issued after approval is indistinguishable in format from a Pattern 1 delegation token. The policy engine does not need to know whether a delegation was pre-granted or escalation-derived — it sees the same structure. The distinction is only relevant to audit and provenance.

**What the policy engine receives after escalation approval:**

```json
{
  "principal": {
    "id": "{agent-id}",
    "type": "AGENT",
    "tenant_id": "platform-team",
    "identity_source": "ATTESTED"
  },
  "delegation": {
    "present": true,
    "delegator": {
      "id": "{human-id}",
      "type": "HUMAN"
    },
    "scope": ["infra.restart:service-x"],
    "expires_at": "2099-01-24T14:35:00Z",
    "escalation_ref": "{escalation-event-id}"
  },
  "operation": "infra.restart"
}
```

The `escalation_ref` is included for audit correlation only, MUST be ignored by policy evaluation, and is intentionally omitted from the canonical execution context schema defined in the **Policy Engine Contract** section below.

---

## Delegation Token Revocation

Delegation tokens follow the same JTI blocklist approach established in ADR-0008:

- Every delegation token carries a unique `jti`
- Orbit maintains the JTI blocklist in Redis with TTL equal to the token's remaining lifetime
- A human may revoke a delegation grant they issued at any time via the delegation API — this adds the token's `jti` to the blocklist immediately
- The Layer 1 enforcement boundary (gateway/sidecar `ext_authz`) validates delegation token JTIs against the blocklist — not individual service endpoints
- Revocation propagation latency is bounded by Redis read latency — effectively immediate in practice

---

## Policy Engine Contract

The **execution context document** is assembled by Orbit's `ext_authz` service at the enforcement boundary — the Orbit Gateway for north-south requests and the Envoy sidecar for east-west requests (see ADR-0011). The `ext_authz` service constructs this document from the validated platform session token and, if present, the validated delegation token, then passes it to OPA for policy evaluation. Neither the destination service nor OPA itself is responsible for parsing raw tokens.

The policy engine (OPA) receives a consistent **execution context** document for every request, regardless of which flow pattern is in effect. The structure is:

```json
{
  "principal": { ... },
  "delegation": {
    "present": true | false,
    "delegator": { ... },
    "scope": [ ... ],
    "expires_at": "..."
  },
  "operation": "...",
  "resource": { ... },
  "tenant_id": "..."
}
```

Policy rules can interrogate any field. Examples:

```rego
# Only allow deployment creation if delegation is present and grants the write:deployments scope
allow {
    input.operation == "deployment.create"
    input.delegation.present == true
    "write:deployments" in input.delegation.scope
}

# Allow config reads for unassisted agents with the config.reader claim
allow {
    input.operation == "config.read"
    input.principal.type == "AGENT"
    input.delegation.present == false
    input.principal.claims.registered_role == "config.reader"
}
```

---

## Consequences

### Positive

- The RFC 8693 delegation chain structure is self-describing — the full authority chain is readable from the token without additional database lookups at request time
- The policy engine contract is stable across all three flow patterns — policy rules are written once and work regardless of whether a request is delegated or unassisted
- Escalation-derived delegation is structurally identical to pre-granted delegation — no special casing in downstream components
- Every delegation grant is explicitly scoped and time-limited — no ambient or implicit authority accumulation
- Agent workloads carry zero authentication infrastructure burden — no SVID management, no token exchange calls, no token rotation logic. The sidecar layer handles all of this transparently (ADR-0010). Agent developers write application code, not identity plumbing

### Negative

- Delegation grants require active management — humans must explicitly create, scope, and expire them. There is no "just use my permissions" shortcut for agents
- The suspended execution state required by Pattern 3 (escalation) is an infrastructure concern beyond the identity layer — this ADR defines the identity side but the execution pause/resume mechanism must be designed separately
- Chained delegation (agent A delegating to agent B within an existing delegation from human to agent A) is explicitly out of scope for the initial implementation — this limits some multi-agent workflow patterns

### Neutral

- Orbit implements a subset of RFC 8693, not the full specification — future implementers should be aware that not all RFC 8693 fields and flows are supported

---

## Implementation Notes

Orbit should expose:

- `POST /identity/delegation` — create a delegation grant (human-initiated; validates scope does not exceed grantor's permissions; issues RFC 8693 delegation token)
- `DELETE /identity/delegation/{jti}` — revoke a delegation grant
- `GET /identity/delegation` — list active delegation grants for the authenticated principal
- `POST /identity/escalation` — agent raises an escalation (records event, suspends relevant execution context, notifies target)
- `POST /identity/escalation/{id}/respond` — human approves or rejects an escalation (issues bounded delegation token on approval)

There is a deliberate asymmetry in how the two token types reach the enforcement boundary:

- **Platform session token** (`Authorization: Bearer`) — injected by the Envoy sidecar transparently. The agent workload never sees, holds, or attaches this token. The sidecar handles the full SVID-to-platform-token exchange and rotation lifecycle (ADR-0010).
- **Delegation token** (`X-Orbit-Delegation-Token`) — attached by the agent workload explicitly. Delegation is a task-specific, application-level concern: the agent knows it is operating under a human's delegated authority for a particular job. It is correct for this to be visible at the application layer.

This separation keeps the principal's own identity infrastructure fully invisible to the agent while ensuring delegation context — which is semantically meaningful to the agent's task — remains explicit.

Both the platform token and the delegation token are validated at the **Layer 1 enforcement boundary** (gateway or sidecar) before the request reaches the destination service. The `ext_authz` service validates signatures, expiry, JTI blocklist status, and scope constraints for both tokens in a single pre-routing evaluation. Services do not perform token validation themselves.

---

## Validation

### Success Metrics

- Policy engine receives a well-formed execution context document on 100% of requests — no missing or malformed delegation fields
- Delegation token scope is always a strict subset of the delegating principal's permissions at grant time — verified by integration tests
- Revoked delegation tokens are rejected on all subsequent requests within one Redis round-trip
- Escalation approval produces a delegation token that is structurally identical to a pre-granted delegation token — verified by shared schema tests

### Monitoring

- Delegation grants issued per tenant and per delegating principal
- Delegation token revocations
- Escalation events raised and response latency (time from escalation to human response)
- Policy evaluation outcomes broken down by flow pattern (delegated vs unassisted)
- Rejected requests due to insufficient delegation scope

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — The Principal abstraction and platform session token this ADR builds on
- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE workload attestation that establishes agent principal identity
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Gateway and sidecar enforcement topology that assembles the execution context document and invokes OPA policy evaluation at the enforcement boundary

---

## References

- [RFC 8693 — OAuth 2.0 Token Exchange](https://www.rfc-editor.org/rfc/rfc8693)
- [RFC 7519 — JSON Web Token](https://www.rfc-editor.org/rfc/rfc7519)
- [OPA Policy Language](https://www.openpolicyagent.org/docs/latest/policy-language/)

---

## Notes

The escalation target configuration — which human or group an agent escalates to, and how that is configured — is intentionally deferred from this ADR. It involves product decisions about agent registration UX, group/role escalation routing, and notification delivery that are not identity-layer concerns. When that design is formalised it should reference this ADR and extend the escalation model defined here.

**Last Updated:** 2026-03-03

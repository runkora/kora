# Unified Identity Model

**Status:** Accepted
**Date:** 2026-03-05
**Deciders:** Engineering Team
**Technical Story:** Establishing the foundational identity abstraction that supports both human and non-human principals across the Orbit platform.

---

## Context

Orbit is a control plane for autonomous agents. It must serve two fundamentally different categories of actor: **human operators** (engineers, administrators, and end-users who configure, monitor, and interact with agents) and **non-human principals** (agents, services, and system components that execute autonomously).

### Background

Most identity systems are designed primarily around human users, with non-human identities bolted on as service accounts or API keys. This approach breaks down in a platform like Orbit where non-human actors are first-class participants — they must be auditable, policy-governed, and revocable with the same rigour as human actors.

The two identity worlds Orbit must bridge are structurally incompatible:

- **Human identity** is established via federated SSO — OIDC or SAML against an external provider (Entra ID, Okta, etc.). This model is session-oriented, user-initiated, and assumes an interactive login flow.
- **Non-human / agent identity** must be established via workload attestation — cryptographically proving that a process is who it claims to be based on its runtime characteristics, without requiring pre-shared secrets or human involvement. SPIFFE/SPIRE is the leading open standard for this.

These two sources produce fundamentally different credential formats (OIDC ID tokens / SAML assertions vs SPIFFE SVIDs) and have different lifecycle properties. Downstream platform components — the policy engine, credential broker, event spine, audit log — should not need to understand this distinction. They need a single, consistent identity interface.

Orbit also supports multiple teams sharing a single instance (multi-tenancy). Identity must be tenant-scoped from the ground up.

### Problem Statement

We need an identity model that:
1. Treats human and non-human actors as first-class principals under a single abstraction
2. Accepts credentials from both federated SSO and SPIFFE workload attestation
3. Normalises both into a consistent platform token consumed by all downstream components
4. Scopes identity to tenants for multi-tenant isolation
5. Supports session lifecycle and revocation for both principal types

### Goals

- **Unified abstraction**: One `Principal` type that represents any actor in the platform
- **Source-agnostic downstream**: Policy engine, audit, and credential broker never branch on identity source
- **Tenant isolation**: All identity operations are scoped to a tenant
- **Federation-ready**: Support multiple upstream OIDC/SAML providers (one or more per tenant)
- **Workload attestation-ready**: Support SPIFFE/SPIRE as the non-human identity source
- **Clean session lifecycle**: Appropriate token lifetimes and renewal for each principal type
- **Revocable**: Delegation and session tokens can be invalidated before natural expiry

### Non-Goals

- Defining the SPIFFE/SPIRE deployment topology (covered in [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md))
- Defining delegation chain semantics and execution flow patterns (covered in [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md))
- Implementing authorisation or policy evaluation (separate concern, consumes the Principal — enforcement topology is defined in [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md))
- Supporting anonymous or unauthenticated access to platform APIs
- Defining escalation path configuration (deferred)

---

## Decision Drivers

* **Consistency**: Downstream components must not need to distinguish human from agent identity
* **Security**: Credentials must be verifiable, short-lived, and scoped to tenant and operation
* **Extensibility**: New identity sources (e.g. GitHub OIDC, cloud provider workload identity) should be addable without changing the Principal model
* **Multi-tenancy**: Identity must carry tenant scope as a first-class attribute
* **Auditability**: Every action must be attributable to a specific, verified principal
* **Operational simplicity**: The session lifecycle should match the natural properties of each principal type — not force agents into a human-oriented refresh model

---

## Considered Options

### Option 1: Separate identity models for human and non-human principals

**Description:** Maintain two distinct identity types — `HumanPrincipal` and `AgentPrincipal` — each with their own token format, session management, and API surface. Downstream components receive whichever type is relevant.

**Pros:**
- Each model can be precisely tailored to its identity source
- No compromise in the abstraction — human and agent fields don't pollute each other

**Cons:**
- Every downstream consumer (policy engine, audit, credential broker) must handle two types — this is a perpetual branching tax
- Cross-cutting concerns (e.g. audit log schema, policy rule format) become inconsistent
- Adding a third principal type (e.g. `SERVICE`) requires updating every consumer
- Delegation chains that span human and agent principals become structurally awkward

### Option 2: Unified `Principal` abstraction with discriminated type and source

**Description:** A single `Principal` struct used for all actors. A `type` field discriminates the kind of actor; an `identity_source` field records how identity was established. Source-specific verified attributes are carried in a `claims` map. A normalised platform session token carries the Principal and is the only credential downstream components see.

**Pros:**
- Downstream components operate against one interface — no branching on principal type
- New identity sources and principal types can be added without modifying consumers
- Policy rules, audit events, and delegation chains have a consistent subject format
- Tenant scope is always present, never optional

**Cons:**
- Some fields are only meaningful for certain types (mitigated by the `claims` map carrying type-specific detail rather than top-level fields)
- Requires Orbit to take on the normalisation responsibility — it must understand both OIDC and SPIFFE credential validation

### Option 3: Adopt an existing standard (e.g. OAuth 2.0 identity model wholesale)

**Description:** Model the Principal entirely on OAuth 2.0 concepts — every actor gets client credentials and an access token. Agents use the OAuth 2.0 client credentials flow.

**Pros:**
- Well-understood standard with broad tooling support
- Existing libraries handle token issuance and validation

**Cons:**
- OAuth 2.0 client credentials require pre-registered secrets — exactly what SPIFFE/SPIRE avoids
- Doesn't accommodate SVID-based attestation natively
- Loses the richness of workload identity (selector-based attestation, automatic rotation)
- Forces agent identity into a human-centric credential model

---

## Decision

**Chosen Option:** Option 2 — Unified `Principal` abstraction with discriminated type and identity source.

### Rationale

The unified model is the correct choice because Orbit's value proposition is precisely that it provides a consistent governance layer over heterogeneous actors. If the identity model bifurcates, that heterogeneity leaks into every component that consumes identity. The normalisation cost is paid once — in Orbit's identity service — and the benefit is realised everywhere else.

Option 3 was rejected because pre-shared secrets are an anti-pattern for dynamic, short-lived agent workloads. SPIFFE/SPIRE solves the workload identity bootstrapping problem correctly; we should not discard that by forcing agents through a client-credentials flow.

---

## The Principal Model

```go
// PrincipalType identifies the category of actor.
type PrincipalType string

const (
    PrincipalTypeHuman   PrincipalType = "HUMAN"
    PrincipalTypeAgent   PrincipalType = "AGENT"
    PrincipalTypeService PrincipalType = "SERVICE"
    PrincipalTypeSystem  PrincipalType = "SYSTEM"
)

// IdentitySource records how the principal's identity was established.
type IdentitySource string

const (
    IdentitySourceFederated IdentitySource = "FEDERATED" // OIDC / SAML via Entra, Okta, etc.
    IdentitySourceAttested  IdentitySource = "ATTESTED"  // SPIFFE/SPIRE workload attestation
    IdentitySourcePlatform  IdentitySource = "PLATFORM"  // Internally issued (e.g. SYSTEM principals)
)

// Principal is the unified identity abstraction for all actors in the Orbit platform.
// It is the subject of all policy decisions, audit events, and delegation chains.
type Principal struct {
    // ID is a globally unique, stable identifier for this principal.
    // For HUMAN principals this is derived from the upstream IdP subject claim.
    // For AGENT principals this is Orbit's own agent registry ID.
    ID string `json:"id"`

    // Type discriminates the category of actor.
    Type PrincipalType `json:"type"`

    // TenantID scopes this principal to a specific tenant on this Orbit instance.
    // All operations performed by this principal are implicitly scoped to this tenant.
    TenantID string `json:"tenant_id"`

    // DisplayName is a human-readable label for audit and UI purposes.
    DisplayName string `json:"display_name"`

    // IdentitySource records the authentication mechanism used to establish this identity.
    IdentitySource IdentitySource `json:"identity_source"`

    // Claims carries source-specific verified attributes.
    // For FEDERATED principals: email, groups, upstream subject, IdP issuer.
    // For ATTESTED principals: SPIFFE ID URI, SPIRE node attestation method.
    // For PLATFORM principals: internal role, bootstrap context.
    Claims map[string]string `json:"claims"`
}
```

### Why Autonomy Level is Not on the Principal

Autonomy is contextual, not intrinsic. The same agent may operate autonomously for low-risk operations (querying state, reading configuration) and require human consultation for high-risk ones (modifying infrastructure, sending external communications). The ceiling also shifts depending on whether the agent is acting under a human delegation context or unassisted.

Storing autonomy on the Principal would require updating the identity record whenever operational context changes — the wrong coupling. Autonomy level **emerges from the intersection of policy evaluation, delegation context, and the operation being attempted**. The identity layer's responsibility is to answer "who is this actor and how was their identity established?" The policy layer answers "what are they permitted to do in this context?"

---

## Orbit as Identity Attestor and Normaliser

Orbit sits between two identity worlds and normalises both into a single platform token:

```
Entra / Okta ──→ OIDC / SAML ──┐
                                 ├──→ Orbit Identity Service ──→ Platform Session Token (JWT)
SPIFFE / SPIRE ──→ SVID ────────┘
```

The **platform session token** is a short-lived, signed JWT issued by Orbit. It carries the normalised `Principal` as its subject claims. All downstream platform components — policy engine, credential broker, event spine, API handlers — validate the platform token and operate against the Principal it contains. They do not receive raw OIDC tokens, SAML assertions, or SVIDs.

This is the abstraction boundary that makes unified policy evaluation possible. Downstream components are written once against the Principal model, regardless of how many identity sources Orbit supports.

### Platform Session Token Claims

```json
{
  "iss": "https://orbit.{instance}",
  "sub": "{principal-id}",
  "aud": "orbit-platform",
  "exp": 1234567890,
  "iat": 1234567000,
  "jti": "{unique-token-id}",
  "orbit": {
    "principal_type": "AGENT",
    "tenant_id": "platform-team",
    "identity_source": "ATTESTED",
    "display_name": "ci-deploy-bot",
    "claims": {
      "spiffe_id": "spiffe://orbit.acme/tenant/platform-team/agent/ci-deploy-bot"
    }
  }
}
```

---

## Session Lifecycle

Human and agent principals have meaningfully different session lifecycle requirements. The platform token format is identical, but how it is obtained and renewed differs.

### Human Session (FEDERATED)

Human sessions are initiated via an interactive OIDC/SAML login flow against the tenant's configured upstream provider. Orbit validates the inbound credential, creates the Principal, and issues:

- A **short-lived platform session token** (15–30 minute TTL)
- A **refresh token** (longer-lived, opaque, stored server-side with the session record)

When the platform token expires, the client presents the refresh token. Orbit re-validates the upstream session (checking the upstream IdP has not revoked the user's session) before issuing a new platform token. This means Orbit's session is bounded by the upstream IdP's session — if a user is deprovisioned in Entra, the next refresh attempt fails.

### Agent Session (ATTESTED)

Agent sessions are not initiated interactively. The full exchange flow is handled transparently by the Envoy sidecar — the agent workload is not involved:

1. The SPIRE Agent on the agent's host attests the workload and issues an SVID (X.509 certificate, ≤1 hour TTL)
2. The SPIRE sidecar holds the current SVID and exposes it to the Envoy sidecar via the SPIFFE SDS API
3. The Envoy sidecar presents the SVID to Orbit's `/identity/exchange` endpoint on behalf of the agent (see [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) for the full flow)
4. Orbit validates the SVID against the SPIRE trust bundle and extracts the SPIFFE ID
5. Orbit looks up the corresponding agent principal in its registry
6. Orbit issues a short-lived platform session token (≤ SVID TTL, typically 15 minutes) and returns it to the Envoy sidecar
7. Envoy caches the token and injects it as `Authorization: Bearer` on all outbound requests from the agent workload. The token is never exposed to the agent.
8. When the SVID rotates (handled automatically by the SPIRE sidecar), Envoy detects the new SVID via SDS and re-exchanges it for a new platform token — again transparently

Agents do **not** use a refresh token mechanism. The SPIRE and Envoy sidecar pair handles the full credential rotation lifecycle. The agent workload makes plain HTTP calls with no knowledge of the underlying identity infrastructure. This keeps agent session lifecycle firmly in the SPIRE-managed rotation model and avoids coupling Orbit's session management to agent-specific refresh logic.

| Property | Human | Agent |
|---|---|---|
| Auth mechanism | OIDC / SAML | SVID (SPIFFE) |
| Token TTL | 15–30 minutes | ≤ SVID TTL (~15 min) |
| Renewal | Refresh token → re-validates upstream IdP | New SVID → token exchange |
| Revocation | JTI blocklist + upstream session termination | SPIRE registration entry deletion + JTI blocklist |
| Session lifetime | Full working session (hours) | Continuous, rotation-managed by SPIRE |

---

## Multi-Tenancy

Multiple teams may share a single Orbit instance. Tenant isolation is a first-class constraint in the identity model:

- Every Principal carries a `tenant_id`. It is non-nullable and set at authentication time.
- Each tenant may have its own OIDC/SAML federation configuration. Orbit supports multiple upstream IdP registrations, each scoped to a tenant.
- The SPIFFE trust domain for agent principals is structured to make tenant scope unambiguous: `spiffe://orbit.{instance-id}/tenant/{tenant-id}/...` (see [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) for full topology).
- Cross-tenant operations are prohibited by default. The policy engine may permit explicit cross-tenant grants, but these require intentional configuration.
- Escalation paths and delegation chains are tenant-scoped. A principal in one tenant cannot be a delegation target or escalation target for a principal in another tenant unless explicitly permitted.

---

## Revocation

Orbit uses a standard JWT revocation approach:

- Every issued platform session token carries a unique `jti` (JWT ID) claim.
- A **JTI blocklist** (backed by Redis with TTL matching the token's remaining lifetime) holds revoked token IDs.
- JTI validation is performed at the **Layer 1 enforcement boundary** — the Orbit Gateway (north-south) and Envoy sidecars (east-west) validate the `jti` against the blocklist before a request reaches any service (see [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)). Individual service endpoints do not need to perform this check themselves.
- For delegated tokens (RFC 8693 exchanges, see [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md)), the same blocklist is used.
- For agent principals, revocation of the SPIRE registration entry stops new SVIDs from being issued. The blocklist covers the gap between entry deletion and SVID natural expiry.
- For human principals, blocklisting the current JTI plus the refresh token record ensures the session cannot be renewed.

This is intentionally simple. Orbit does not implement a custom revocation protocol — the JTI blocklist pattern is well-understood, operationally straightforward, and correct for short-lived tokens.

---

## Consequences

### Positive

- All downstream components operate against a single, stable Principal model — no branching on identity source
- New identity sources (e.g. GitHub Actions OIDC, AWS IAM Roles Anywhere) can be added by implementing a new credential validator that produces a Principal — no downstream changes required
- Policy rules, audit records, and delegation chains all share the same subject format
- Multi-tenancy is enforced at the identity layer — tenant isolation cannot be accidentally bypassed by omission

### Negative

- Orbit takes on the responsibility of validating both OIDC tokens and SPIFFE SVIDs — it becomes a critical dependency in the authentication path
- The identity service must be highly available; it is now in the critical path for every platform operation
- Supporting multiple upstream OIDC/SAML configurations per tenant adds operational complexity to the federation layer

### Neutral

- The `claims` map allows source-specific attributes to be carried without polluting the Principal struct — but it is untyped, so consumers that need specific claims must know the key names for each identity source

---

## Implementation Notes

The identity service should expose:

- `POST /identity/session` — initiates a human session (validates OIDC/SAML credential, issues platform token + refresh token)
- `POST /identity/refresh` — refreshes a human session (validates refresh token, re-checks upstream IdP, issues new platform token)
- `POST /identity/exchange` — exchanges an SVID for an agent platform token
- `POST /identity/revoke` — revokes a session token or refresh token

The platform token signing key should be rotated regularly. The public key(s) should be discoverable via a standard JWKS endpoint (`GET /.well-known/jwks.json`) so enforcement points (gateway, sidecars) can validate tokens locally without calling the identity service on every request.

**Bootstrap exception — `/identity/exchange`:** This endpoint is called by the Envoy sidecar on behalf of the agent workload before any platform session token exists. It must be treated as a gateway-exempt bootstrap endpoint: the Orbit Gateway passes requests to this path directly to the identity service without requiring a platform token in the `Authorization` header. The SVID presented via mTLS is the authentication mechanism — it is the only Orbit API where the SVID substitutes for a platform token. All other identity service endpoints require a valid platform session token and are subject to full gateway enforcement ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)).

### Migration Path

Not applicable — this is a greenfield implementation establishing the foundational identity model.

---

## Validation

### Success Metrics

- Platform token is the only credential format accepted by downstream API handlers — no raw OIDC tokens or SVIDs reach the policy engine or credential broker
- Human and agent principals produce structurally identical platform tokens, verified by shared token validation tests
- Tenant isolation: a principal from tenant A cannot present a valid token scoped to tenant B
- Revoked tokens are rejected within one Redis round-trip of the revocation event

### Monitoring

- Token issuance rate per principal type and tenant
- Token validation failures (invalid signature, expired, blocklisted JTI)
- SVID exchange failures (SPIRE unavailable, unknown SPIFFE ID, mismatched tenant)
- Refresh token usage rate and failure rate per tenant

---

## Related Decisions

- [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — Delegation chains and execution flow patterns that build on this identity model
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE infrastructure implementing the ATTESTED identity source
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Gateway and sidecar enforcement topology that consumes the platform session token and enforces policy at all service boundaries

---

## References

- [SPIFFE Standard](https://spiffe.io/docs/latest/spiffe-about/overview/)
- [SPIRE Documentation](https://spiffe.io/docs/latest/spire-about/)
- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [RFC 7519 — JSON Web Token](https://www.rfc-editor.org/rfc/rfc7519)
- [RFC 7009 — OAuth 2.0 Token Revocation](https://www.rfc-editor.org/rfc/rfc7009)

---

## Notes

The `claims` map is intentionally flexible. As the platform matures and claim schemas stabilise for each identity source, we may introduce typed claim structs per source. For now, the map avoids premature schema ossification while the identity sources and their relevant attributes are being validated in practice.

**Last Updated:** 2026-03-03

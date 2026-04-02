# Policy Gateway — Product Requirements Document

**Status:** Approved  
**Created:** 2026-04-02  
**Last Updated:** 2026-04-02  
**Owner:** Orbit Core Team  
**Stakeholders:** Orbit Core Team, Agent Developers, Enterprise Platform Operators

---

## Executive Summary

The Policy Gateway is Orbit's universal authorisation layer. It governs all principal-to-resource communication across the platform — what agents can call, what humans can invoke, what services can reach other services, and what any delegated principal can do on behalf of another. It is a platform-wide concern, not a feature of any individual service. Every request to every resource passes through policy evaluation before it is fulfilled. The inference gateway, MCP servers, external service proxies, and platform APIs are all consumers of the same policy system.

---

## Problem Statement

### Current State

Orbit's identity layer is operational. Principals can authenticate and carry verified tokens. However there is no enforcement layer: any authenticated principal can reach any resource on the platform. The absence of policy enforcement means:

- An agent token grants access to every inference model, every MCP server, every platform API, and every external service the platform can reach
- The delegation model (ADR-0009) is structurally defined but not enforced — delegation tokens carry scope claims that are never evaluated against any access control
- Services have no mechanism to restrict which other services may call them
- There is no self-service mechanism for tenant administrators to configure what their agents are allowed to do

### User Pain Points

**For Platform Operators:**
- No enforcement boundary between principals and resources — any authenticated call succeeds
- No way to enforce delegation boundaries — an agent acting on behalf of a user can access anything, regardless of what the user intended to grant
- No per-tenant or per-agent access surface definition
- No mechanism to revoke an agent's access to a resource without destroying the token or the agent

**For Tenant Administrators:**
- Cannot restrict which agents access which resources — models, MCP tools, external services, platform APIs
- Cannot configure the access surface of an agent fleet without code changes or operator intervention
- No visibility into what agents are permitted to do vs what they are actually doing

**For Agent Developers:**
- No programmatic way to discover which resources an agent is allowed to use — services return their full resource sets regardless of the calling principal's permissions, causing runtime failures and LLM planning dead-ends
- No reliable way to reason about what a delegation grant actually permits

### Business Impact

- **Security exposure:** A compromised agent token grants unrestricted access to all platform resources within the tenant
- **Delegation integrity:** The delegation model cannot be enforced without a policy layer — delegation is currently nominal, not enforced
- **Operational risk:** Platform resources (inference capacity, external service quotas, MCP tools) cannot be governed per-agent or per-tenant
- **Compliance gap:** There is no auditable record of what any principal was permitted to do, or what access decisions were made

---

## Goals and Objectives

### Primary Goals

1. **Universal enforcement:** Every request to every platform resource passes through a policy evaluation point before the resource is reached. This applies to all resource types — inference models, MCP servers, external services, and platform APIs. No exceptions for owned or third-party services.

2. **Runtime configurability:** Tenant administrators configure permissions through an API without triggering deployments. Policy changes propagate to all enforcement points within a polling interval. Operational policy changes — granting or revoking an agent's access to a resource — require no code change, no deployment, and no operator involvement.

3. **Delegation integrity:** The delegation scope intersection model from ADR-0009 is enforced as a platform invariant. An agent acting under a delegation token can never exceed the intersection of the delegation grant, the delegator's own permissions, and the agent's configured ceiling. This is not configurable — it is always enforced.

4. **Developer transparency:** Services expose filtered views of their resources based on the calling principal's permissions. Agents never receive resource lists (model lists, tool lists, service catalogues) containing entries they cannot actually invoke.

5. **Zero implicit trust:** Service-to-service traffic within the platform is subject to the same policy evaluation as external inbound traffic. There is no trusted internal network.

### Success Metrics

- 100% of requests to platform resources pass through a policy evaluation point before the resource is reached
- Policy changes propagate to all enforcement points within the configured polling interval (target: ≤ 30 seconds)
- Resource listing endpoints (models, MCP tools, service catalogues) return only entries the calling principal has grants for
- Zero cross-tenant grant evaluations — tenant A's policy never influences tenant B's decisions
- Policy evaluation adds ≤ 10ms p95 to request latency (local OPA bundle evaluation, no synchronous policy service call on the request path)

### Non-Goals

- **Platform-level plan enforcement:** Restricting resource access based on subscription plan is deferred. Relevant to a SaaS deployment; out of scope for the open-source release.
- **Explicit deny rules:** The initial release is whitelist-only (default deny). Explicit deny rules — for carving exceptions from wildcard grants — are not implemented. The "except" pattern is handled by enumerating specific allows.
- **Fine-grained PAT policy:** PAT-to-resource access control is deferred. PATs inherit the issuing principal's permissions.
- **Cross-tenancy grants:** Cross-tenant resource access is architecturally disallowed in this release.
- **Audit event spine:** Policy decisions emit structured log lines but are not published to a durable event spine. The event contract will be designed alongside the spine infrastructure.
- **JTI blocklist in AuthMiddleware:** Wiring the Redis JTI blocklist into `AuthMiddleware` and the ext_authz token validation path is a follow-on hardening task — see Future Considerations.

---

## User Stories

### Tenant Administrator

**As a** tenant administrator,  
**I want to** configure which agents can access which resources on the platform,  
**So that** I can enforce least privilege across my agent fleet without deployments or operator tickets.

**Acceptance Criteria:**
- [ ] I can create a grant giving a named principal a specific narrow scope (e.g. `mcp:gmail:send_email:invoke`)
- [ ] I can create a grant giving a named principal a broad parent scope that implicitly covers a class of resources (e.g. `inference:anthropic:invoke` for all Anthropic models, `mcp:gmail:invoke` for all Gmail tools)
- [ ] I cannot create a grant referencing an unregistered scope — the API rejects it with a clear error
- [ ] I can delete a grant and have the principal lose access within the next polling interval
- [ ] I cannot create a grant that affects another tenant's principals or resources

---

**As a** tenant administrator,  
**I want to** understand the current access surface of any agent,  
**So that** I can audit and reason about my fleet's permissions.

**Acceptance Criteria:**
- [ ] I can retrieve all grants for a given agent
- [ ] I can retrieve all agents with access to a given resource

---

### Agent Developer

**As an** agent developer,  
**I want** platform services to return only resources my agent is permitted to use,  
**So that** agents never encounter 403s at invocation time for resources the platform presented as available.

**Acceptance Criteria:**
- [ ] Resource listing endpoints (inference models, MCP tools, service catalogues) return only entries the calling principal has grants for
- [ ] An attempt to invoke a resource outside the principal's grants returns a 403 before the resource executes
- [ ] A delegated agent request is evaluated against the intersection of delegation grant, delegator scopes, and agent ceiling

---

### Platform Operator

**As a** platform operator,  
**I want** every service request to pass through a policy evaluation point regardless of whether the service is owned by Orbit or third-party,  
**So that** I have a consistent, auditable enforcement boundary across the entire platform.

**Acceptance Criteria:**
- [ ] Envoy ext_authz intercepts all inbound requests at the platform edge before routing to any service
- [ ] Envoy sidecar intercepts all east-west service-to-service requests before the destination service receives them
- [ ] Third-party services receive the same enforcement guarantees as owned services without requiring code changes in the third-party service

---

## Proposed Solution

### High-Level Approach

The Policy Gateway follows the NIST ABAC reference model — three cooperating components:

**Policy Administration Point (PAP) — Policy Service**  
A database-backed Go service with a REST API for grant management. Tenant administrators create, read, and delete grants through this API or through the Orbit UI. On every write, the service recompiles the OPA policy bundle for the affected tenant and makes the updated bundle available for distribution. No deployment is required for policy changes.

**Policy Decision Point (PDP) — ext_authz Service**  
A Go binary serving two interfaces:
- A gRPC server implementing `envoy.service.auth.v3.Authorization` — used by Envoy at the edge and at every sidecar
- An HTTP server exposing `POST /v1/policy/check` and `POST /v1/policy/query` — used by owned services for Layer 2 in-application enforcement

The ext_authz service loads OPA policy bundles locally and evaluates them in-process. There is no synchronous call to the policy service on the request path. Token validation uses the existing `token.Verifier` from `internal/platform/token`, validating JWT signatures via the identity service's JWKS endpoint.

**Policy Enforcement Points (PEP)**  
- **Layer 1 — Envoy (edge and sidecar):** Intercepts all requests. The ext_authz gRPC interface handles path-level decisions — can this principal reach this service at this path? Returns 401 or 403 before the destination service receives the request.
- **Layer 2 — In-application (owned services):** Owned services call the ext_authz HTTP API for decisions that require payload inspection — a model ID in a request body, a tool name in an MCP call. This is also where resource listing endpoints call `POST /v1/policy/query` to filter their responses. The calling service provides full resource context (type, ID, attributes) in the check request — the policy service has no independent knowledge of resource catalogues.

### Policy Model

**Grants are the unit of policy.** A grant asserts that a named principal within a tenant holds a set of OAuth-style permission scopes. Default posture is deny — access requires an explicit grant. No explicit deny rules in this release. Grants cover all principal types: `AGENT`, `HUMAN`, and `SERVICE`.

```
Grant {
  tenant_id        // tenant scope — cross-tenant grants do not exist
  principal_type   // AGENT | HUMAN | SERVICE
  principal_id     // agent ID, user ID, or service SPIFFE ID
  scopes[]         // ["inference:anthropic:invoke", "mcp:gmail:invoke"]
}
```

Multiple grants for the same principal combine with OR. There is no update operation — modifications are expressed as delete + create.

### Scope Vocabulary

Permissions use a colon-separated OAuth scope format: `{resource_type}:{path_segment*}:{action}`. Examples:

```
inference:invoke                 all inference models, invoke
inference:anthropic:invoke       Anthropic models only
mcp:invoke                       all MCP tools, invoke
mcp:gmail:invoke                 Gmail MCP only
mcp:gmail:send_email:invoke      specific tool only
external:msgraph:mail:read       MS Graph mail, read only
platform:grants:write            manage grants
```

A broader scope implicitly covers narrower child scopes — `inference:invoke` covers `inference:anthropic:invoke`. No wildcards appear in scope strings; breadth is expressed by granting a higher-level scope. The vocabulary is a finite registered set managed by the Policy Service and compiled into every OPA bundle.

### Delegation Enforcement

The delegation scope intersection is a **platform invariant**, not a configurable rule. When a delegation token is present the evaluation engine always enforces:

```
effective_access = delegation_grant_scopes ∩ delegator_grants ∩ agent_grants
```

All three are expressed in the same OAuth scope vocabulary. The intersection is evaluated in OPA — for a delegated request the required scope must be covered by at least one scope from each of the three sets. An agent under delegation can never exceed the most restrictive of the three. When no delegation token is present (autonomous agent), only the agent's own grants apply.

### Two API Shapes

`POST /v1/policy/check` — binary allow/deny for a specific principal and resource. Used by Envoy (via gRPC) for path-level decisions and by owned services for pre-invocation checks.

`POST /v1/policy/query` — accepts the calling service's full candidate resource list and returns only those the principal is permitted to access. The calling service owns the resource catalogue; the policy service owns the grants. Used by any listing endpoint (model list, MCP tool list, service catalogue) before returning to the caller.

### Token Validation

The ext_authz service validates platform tokens using `token.Verifier` from `internal/platform/token` — JWKS-based, no call to the identity service on the request path. The validated `PlatformClaims` provide the `tenant_id`, `principal_type`, and principal identifier used to construct the OPA input document.

### Multi-Tenancy

Cross-tenancy is architecturally disallowed. Bundles are served **per tenant** — each enforcement point polls `GET /v1/policy/bundles/{tenant_id}` and receives a bundle containing only that tenant's grants and vocabulary. Cross-tenant data is never present in any bundle. Policy evaluation always scopes to the tenant extracted from the validated token — it cannot be overridden by the request.

---

## Scope

### In Scope

- Policy Service (PAP): grant CRUD API, OPA bundle compilation on write, bundle serving endpoint
- ext_authz Service (PDP): gRPC interface for Envoy, HTTP check/query API, in-process OPA evaluation, token validation via JWKS, delegation intersection computation
- OPA bundle structure: tenant-partitioned data, Rego evaluation rules, scope hierarchy evaluation, route-to-scope mapping
- Layer 1 enforcement: Envoy ext_authz at edge and sidecars for path-level decisions on all resource types
- Layer 2 enforcement: in-application check and query API integration for owned services (inference gateway, MCP servers, and any future owned service)
- Resource listing filtering: any service's listing endpoint can call `POST /v1/policy/query` to filter its response
- Whitelist-only grant model with default deny across all resource types

### Out of Scope

- Explicit deny rules
- Fine-grained PAT policy
- Cross-tenant grants
- Audit event spine publication (structured logs only)
- JTI blocklist in `AuthMiddleware` / ext_authz token validation (future hardening)
- Platform-level plan-based policy (SaaS concern)
- Credential-aware outbound gateway policy (ADR-0012 — separate feature)

### Future Considerations

- **JTI blocklist + push bundle invalidation (address together, JTI first):** The Redis JTI blocklist infrastructure exists but is not wired into ext_authz token validation. Separately, the polling model means grant deletions take up to 30s to propagate. These deferrals compound — a revoked token can be used for up to `token_ttl` regardless of blocklist state, and a deleted grant takes effect within the polling interval. JTI wiring (`JTIBlocklistRepository` lifted to `internal/platform/`, wired into ext_authz and `AuthMiddleware`) closes the larger gap first. Push-based bundle invalidation follows as the second hardening step.
- **Explicit deny rules:** The grant model should be extended to support a `deny` flag with deny-overrides-allow semantics once wildcard grants make carve-outs necessary.
- **Fine-grained PAT policy:** PATs should support scope selection at issuance time, constraining them to a subset of the issuing principal's scopes.
- **Audit event spine:** Policy decisions should be published as structured events once the spine infrastructure is established.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Grant CRUD: create, read, delete grants scoped to tenant, for AGENT / HUMAN / SERVICE principals
- [ ] OAuth scope vocabulary: register, list, and serve scope definitions with hierarchy metadata
- [ ] Scope validation at grant write time: reject grants referencing unregistered scopes
- [ ] Per-tenant OPA bundle compiled on grant write and served at `GET /v1/policy/bundles/{tenant_id}`
- [ ] ext_authz gRPC server implementing `envoy.service.auth.v3.Authorization` for Envoy
- [ ] ext_authz HTTP `POST /v1/policy/check` — binary allow/deny for any resource type and principal type
- [ ] ext_authz HTTP `POST /v1/policy/query` — accepts candidate resource list, returns allowed subset
- [ ] Token validation in ext_authz using `token.Verifier` (JWKS, signature, expiry)
- [ ] Delegation intersection enforced as platform invariant in OPA when delegation token is present
- [ ] Per-tenant bundle — cross-tenant data never present in any enforcement point's bundle
- [ ] OIDC role-based PAP access: `tenant-admin` role permits `platform:grants:write` without a bootstrap grant
- [ ] Layer 2 integration in the inference gateway: model listing filtered via `query`, pre-flight check via `check`
- [ ] Layer 2 integration in MCP servers: tool listing filtered via `query`, pre-invocation check via `check`

**Should Have (P1):**
- [ ] Grant listing by agent (all grants for a given agent)
- [ ] Grant listing by resource (all agents with a grant matching a given resource)
- [ ] Structured log emission on every policy decision with: `tenant_id`, `principal_id`, `principal_type`, `resource_type`, `resource_id`, `outcome`, `reason`, `duration_ms`

**Nice to Have (P2):**
- [ ] Policy dry-run API: evaluate a hypothetical check request against current policy without enforcing
- [ ] Grant validation at write time: warn if the resource pattern matches no known resources in the platform

### Non-Functional Requirements

**Performance:**
- Policy evaluation (local OPA bundle) adds ≤ 10ms p95 to request latency
- Bundle polling interval configurable; default 30 seconds
- Bundle compilation on grant write completes within 500ms

**Security:**
- Default deny — no access without an explicit grant
- Cross-tenant evaluation prevented by bundle partitioning and Rego scoping
- Token validation on every ext_authz call — no request proceeds without a valid, unexpired platform token
- Delegation intersection always enforced when delegation token is present — cannot be bypassed

**Reliability:**
- ext_authz service is stateless beyond the locally cached OPA bundle — horizontal scaling requires no coordination
- If the policy service bundle endpoint is temporarily unavailable, enforcement points continue operating against their last-loaded bundle

**Observability:**
- Every policy decision (allow and deny) emits a structured log line
- OPA bundle sync status emitted as metrics: last successful sync timestamp, bundle version per enforcement point

---

## Dependencies

### Internal Dependencies

- **Identity Service (ADR-0008):** Provides the JWKS endpoint for `token.Verifier`. Must be reachable from ext_authz at startup for initial key fetching.
- **`internal/platform/token`:** `token.Verifier` and `PlatformClaims` used directly by ext_authz for token validation and principal context extraction. No other identity service dependency on the request path.
- **Inference Gateway (ADR-0023):** Integrates with the ext_authz HTTP API for model listing and pre-flight checks. The integration point is already designed into the gateway architecture.
- **MCP servers (ADR-0013, ADR-0014):** Integrate with the ext_authz HTTP API for tool listing and pre-invocation checks.
- **Redis:** JTI blocklist infrastructure exists and is used by the identity service for revocation writes. Wiring the read side into ext_authz is deferred.

### External Dependencies

- **OPA (Open Policy Agent):** Embedded as a Go library in the ext_authz binary. Not a separate process or sidecar.
- **go-control-plane/envoy:** Already in `go.mod` at `v1.37.0`. Provides `envoy.service.auth.v3` proto types.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Bundle propagation delay exposes stale policy | Medium | Medium | Short polling interval (≤ 30s); document that grant deletion takes effect within one interval |
| ext_authz unavailability affects all platform requests | High | Low | Fail-closed by design — ext_authz must be multi-replica with fast startup. An outage blocks all platform traffic, which is the correct posture: policy is the enforcement boundary, not a convenience layer |
| Per-tenant bundle compilation throughput degrades under high write volume | Medium | Low | Bundles are compiled per-tenant on write — only the affected tenant's bundle is recompiled. Incremental compilation is a follow-on optimisation. |
| Delegation intersection logic incorrect | High | Low | Evaluated in OPA with unit-tested Rego; hierarchy resolution logic has explicit `go test` coverage via the scope hierarchy helpers |

---

## Open Questions

All product and architecture decisions are resolved. The following implementation-level questions remain open and should be resolved during the engineering design phase before coding begins:

1. **Scope hierarchy resolution in Rego vs pre-computation:** Should `scope_covers` be evaluated entirely in Rego using the `scope_vocabulary` data document, or should the ext_authz service pre-compute the set of covered scopes before calling OPA? Rego approach keeps all policy logic in the bundle; pre-computation reduces Rego complexity.

2. **ADR-0009 alignment:** The delegation token `scope` claim format must be updated in ADR-0009 to specify the Orbit OAuth scope vocabulary format. This is a cross-ADR dependency that must be resolved before the delegation intersection is implemented.

---

## Appendix

### Related Documents

- [ADR-0008](../adr/0008-unified-identity-model.md) — Unified principal model and platform session token
- [ADR-0009](../adr/0009-agent-execution-flow-patterns-and-delegation.md) — Delegation tokens and execution context
- [ADR-0011](../adr/0011-universal-gateway-policy-enforcement.md) — Universal gateway policy enforcement
- [ADR-0013](../adr/0013-mcp-server-registration-deployment-and-governance.md) — MCP server registration and governance
- [ADR-0014](../adr/0014-scope-gated-tool-exposure-via-mcp.md) — Scope-gated tool exposure via MCP
- [ADR-0021](../adr/0021-envoy-ext-proc-sidecar-token-injection.md) — Identity proxy ext_proc sidecar
- [ADR-0023](../adr/0023-inference-gateway-architecture.md) — Inference gateway architecture
- [ADR-0024](../adr/0024-provider-model-catalogue-and-alias-routing.md) — Model catalogue and alias routing

### Glossary

- **PAP (Policy Administration Point):** Where policy is authored and stored. In Orbit: the Policy Service.
- **PDP (Policy Decision Point):** Evaluates policy and returns allow/deny. In Orbit: the ext_authz service with embedded OPA.
- **PEP (Policy Enforcement Point):** Intercepts requests and enforces PDP decisions. In Orbit: Envoy (Layer 1) and in-application checks (Layer 2).
- **Grant:** A record asserting that a named principal holds a set of OAuth-style permission scopes. The unit of policy. Covers AGENT, HUMAN, and SERVICE principal types.
- **Scope:** An OAuth-style permission string in `type:path*:action` format (e.g. `inference:anthropic:invoke`). Parent scopes implicitly cover child scopes via the hierarchy rule.
- **Scope vocabulary:** The finite registered set of valid scope strings and their hierarchy metadata, managed by the Policy Service and compiled into every OPA bundle.
- **Agent ceiling:** The set of scopes an agent is maximally permitted to hold per its configured grants. One of three factors in delegation intersection.
- **Delegation intersection:** `delegation_grant_scopes ∩ delegator_grants ∩ agent_grants` — the effective permission set for a delegated agent. Enforced as a platform invariant in OPA.
- **Bundle:** A compiled OPA artifact containing Rego policy rules, the tenant's grants, the scope vocabulary, and role mappings. Served per-tenant by the Policy Service.

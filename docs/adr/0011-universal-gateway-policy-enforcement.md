# 0011. Universal Gateway as Policy Enforcement Point

**Status:** Accepted
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Defining how policy is enforced at service boundaries across the Orbit platform — for both agent-to-service (north-south) and service-to-service (east-west) traffic — and establishing the architectural rule that no service is directly reachable without passing through a policy enforcement point.

---

## Context

ADR-0008 established a unified principal model. ADR-0009 established delegation and execution flow patterns. ADR-0010 established workload attestation via SPIFFE/SPIRE. What those ADRs do not cover is the enforcement topology: where in the platform's request path are tokens validated, delegation chains evaluated, and OPA policy applied — and crucially, whether that enforcement is consistent across all services regardless of ownership.

### Background

Orbit deploys two classes of service:

1. **Owned services** — platform components whose code Orbit controls (identity service, policy engine, credential broker, workflow engine, etc.)
2. **Third-party services** — open-source or vendor components deployed into the platform but whose code Orbit does not own (MCP servers, open-source tooling, licensed components)

A naive approach would enforce policy inside owned services (via middleware) and apply a gateway pattern only for third-party services that cannot be instrumented. This creates two enforcement models for the same platform, which is an auditing, consistency, and security gap problem. Policy changes must be coordinated across in-application middleware deployments. If an owned service's middleware is misconfigured, there is no backstop.

The correct model is a **universal enforcement boundary** — all requests to all services, regardless of ownership or type, pass through a policy enforcement point before reaching the service. This is not a workaround for third-party code. It is the primary enforcement boundary for the entire platform.

### Problem Statement

We need to define:

1. The enforcement topology for north-south traffic (agent and human principals calling platform services)
2. The enforcement topology for east-west traffic (service-to-service calls within the platform)
3. The data plane technology used at each enforcement point
4. How OPA policy is distributed to and evaluated at enforcement points
5. The relationship between gateway enforcement (Layer 1) and optional in-application enforcement (Layer 2)
6. The architectural constraint that prevents services from being reachable without passing through enforcement

### Goals

- **Universal enforcement**: Every service receives only requests that have passed a policy enforcement point — no exceptions for owned services, third-party services, or internal calls
- **No implicit trust**: Service-to-service traffic within the platform is subject to the same enforcement model as external inbound traffic
- **Consistent policy model**: The OPA policy bundles, principal context document, and allow/deny semantics are identical regardless of where in the topology enforcement occurs
- **Unified data plane**: A single proxy technology across north-south and east-west enforcement, minimising operational surface area and engineering complexity
- **Third-party service support**: Services whose code Orbit does not own receive the same enforcement guarantees as owned services — without modification to the third-party code
- **Audit completeness**: All enforcement points emit structured audit events to the Orbit event spine, providing a complete picture of all access across the platform

### Non-Goals

- Defining the full service mesh control plane implementation (e.g. whether Istio is adopted — this is an open implementation question noted in consequences)
- Defining Layer 2 in-application enforcement patterns (fine-grained business-logic authz within owned services — a separate concern)
- Defining how escalation paths or human consultation flows are routed (covered in a future ADR)
- Defining response filtering or content-aware policy at the proxy layer
- Replacing SPIFFE/SPIRE as the workload identity mechanism (ADR-0010 is unchanged)

---

## Decision Drivers

* **Security**: No request should reach a service without identity validation and policy evaluation. Implicit internal trust is a known attack surface.
* **Consistency**: One enforcement model across the entire platform eliminates the class of vulnerabilities that arise from inconsistent middleware implementations across service teams
* **Operability for third-party services**: The enforcement model must work for services Orbit does not own without requiring code changes or custom plugin development in those services
* **Auditability**: The enforcement topology must produce a complete, trustworthy audit trail — which requires enforcement to happen at a point Orbit controls, not inside third-party code
* **Scalability**: The enforcement topology must not introduce a central bottleneck for all platform traffic — distributed enforcement is required for east-west traffic
* **Data plane convergence**: Operating two different proxy technologies (one for north-south, one for east-west) doubles the operational burden and creates two different OPA integration patterns. A single data plane technology across both directions is strongly preferred.

---

## Considered Options

### Option A: In-application middleware enforcement only

**Description:** Each service is responsible for its own policy enforcement via Orbit-provided middleware libraries. Owned services import the middleware. Third-party services use a thin wrapper or adapter.

**Pros:**
- No additional infrastructure components
- Enforcement logic is visible in the application codebase
- Familiar pattern for service developers

**Cons:**
- Third-party services cannot be instrumented — the adapter/wrapper model is fragile and non-standard
- Every owned service team must correctly configure and maintain the middleware — misconfiguration has no backstop
- Policy changes require coordinated redeployment of middleware across all services
- No single audit trail — events come from disparate application implementations
- Enforcement is inconsistent by construction — different services may run different middleware versions

### Option B: Centralised gateway for north-south, no east-west enforcement

**Description:** A centralised proxy sits at the platform boundary and enforces policy for all inbound traffic. East-west (service-to-service) traffic flows directly between services without enforcement.

**Pros:**
- Simpler topology — one enforcement point for external traffic
- No sidecar operational complexity

**Cons:**
- Creates implicit internal trust — a compromised internal service can call any other service freely
- East-west traffic is unaudited — the platform has no visibility into inter-service calls
- Violates zero-trust principles — the internal network is treated as trusted by definition
- Does not satisfy the security driver that all requests pass through a policy enforcement point

### Option C: Centralised gateway for all traffic (north-south and east-west physically routed through one process)

**Description:** A single centralised gateway proxy handles all traffic in the platform — agents call the gateway, and all service-to-service calls also physically route through the same gateway process.

**Pros:**
- One enforcement point — conceptually simple to reason about
- Complete audit trail from a single component

**Cons:**
- The gateway becomes a bottleneck for every inter-service call in the platform — at any reasonable scale, this is unacceptable
- Single point of failure for all platform communication, including internal coordination between platform components
- Adds a full round-trip network hop to every east-west call, compounding badly for chatty internal services
- Horizontal scaling of the gateway becomes a hard operational requirement for all platform traffic

### Option D: Envoy as unified data plane — centralised edge deployment for north-south, sidecar deployment for east-west

**Description:** Envoy is used as the data plane proxy for both traffic directions, but deployed differently for each:

- **North-south**: A centralised Envoy deployment (the "Orbit Gateway") sits at the platform boundary. All external inbound traffic — from agents and human principals — enters here. Envoy's `ext_authz` filter calls the Orbit policy service, which validates tokens and evaluates OPA policy before Envoy forwards the request to the destination service.
- **East-west**: An Envoy sidecar runs alongside each service instance. All inter-service traffic is intercepted by the sidecar. The sidecar enforces mTLS using SPIFFE SVIDs (ADR-0010) and evaluates OPA policy via the `ext_authz` filter against locally distributed policy bundles. The application process is unaware of the interception.

Both the edge Envoy and all sidecars consume the same OPA policy bundles distributed by the Orbit policy control plane. The enforcement model — principal context document shape, allow/deny semantics, audit event format — is identical across both deployment patterns.

**Pros:**
- Universal enforcement — all requests, all services, all traffic directions
- Single data plane technology — one operational model, one `ext_authz` integration pattern, one OPA input document shape, one audit event format
- Distributed east-west enforcement — no central bottleneck for inter-service traffic
- Third-party services receive full enforcement with zero code changes — the sidecar and edge proxy enforce without application involvement
- mTLS for east-west traffic is native to Envoy's SPIFFE/SDS integration (aligns with ADR-0010)
- Envoy is the data plane for all major service mesh implementations — tooling ecosystem, operational knowledge, and community support are mature
- Failure isolation for east-west — a sidecar failure affects one service, not all inter-service communication

**Cons:**
- Sidecar operational complexity — sidecar injection, lifecycle management, and config distribution to all sidecar instances requires a control plane (see open question below)
- Envoy configuration has a steeper learning curve than simpler edge proxies
- Two deployment patterns for the same technology require clear operational documentation and tooling

---

## Rejected Alternative: Traefik as Edge Router

During evaluation, Traefik was considered as an alternative edge proxy given its simpler operational model and strong Kubernetes IngressController integration.

Traefik is explicitly rejected for the following reasons:

1. **No meaningful sidecar deployment model.** Traefik was designed as a centralised edge router. It has no mechanism for transparent traffic interception in a sidecar pattern — there is no production sidecar deployment model for Traefik. This means it cannot be used for east-west enforcement, which is a hard requirement.

2. **Split data plane.** Using Traefik at the edge and Envoy in sidecars means operating two proxy technologies, two OPA integration patterns (Traefik's `ForwardAuth` vs Envoy's `ext_authz`), two config models, and two audit event formats. This doubles the operational burden and creates the inconsistency problem the unified data plane decision is specifically designed to avoid.

3. **`ForwardAuth` is not `ext_authz`.** Traefik's external auth integration (`ForwardAuth`) is an HTTP-based yes/no check. Envoy's `ext_authz` is an external authorization filter that supports both HTTP and gRPC authorization services; when used with a gRPC-based authorization service it carries rich request context, supports structured allow/deny responses, and can return additional headers to be forwarded downstream. The Orbit enforcement model — carrying principal context, delegation chain, execution context document — requires the richer integration that `ext_authz` provides.

4. **No SPIFFE/SDS integration.** Traefik has no native understanding of SPIFFE SVIDs or automatic SVID rotation via the SPIRE SDS API. mTLS with SPIFFE at the edge would require custom certificate management rather than the automated SVID lifecycle from ADR-0010.

---

## Decision

**Chosen Option:** Option D — Envoy as unified data plane, centralised edge deployment for north-south, sidecar deployment for east-west.

### Rationale

Option A (in-app middleware) is rejected because it cannot provide consistent universal enforcement and has no meaningful answer for third-party services.

Option B (gateway for north-south only) is rejected because it leaves east-west traffic without enforcement, creating implicit internal trust — a direct violation of the platform's zero-trust principle.

Option C (all traffic through one process) is rejected because it introduces an unacceptable central bottleneck and single point of failure for all platform communication at scale.

Option D satisfies all decision drivers. It delivers universal enforcement across all traffic directions, a single data plane technology that eliminates the split-operational-model problem, distributed east-west enforcement without a central bottleneck, and full enforcement for third-party services with zero code changes.

The rejection of Traefik as the edge proxy is an explicit design decision: the consistency benefits of running Envoy at both the edge and in sidecars outweigh the operational simplicity of a simpler edge router. Short-term configuration complexity at the edge is solvable via tooling and control plane investment; long-term data plane inconsistency is not.

---

## Enforcement Model Detail

### Layer 1 — Gateway / Sidecar (Universal)

Every request to every service passes through a Layer 1 enforcement point before reaching the service. This layer answers: **"Is this principal permitted to call this endpoint at all?"**

At the **edge (north-south)**, the Orbit Gateway (Envoy) performs:
1. TLS termination
2. Platform session token validation (signature, expiry, JTI blocklist check per ADR-0008)
3. Delegation token validation if present (chain integrity, scope, expiry per ADR-0009)
4. OPA policy evaluation via `ext_authz` — principal context document constructed from validated tokens, policy bundle evaluated locally
5. 401 returned if authentication fails; 403 returned if policy denies
6. Structured audit event emitted to the Orbit event spine regardless of outcome
7. Request forwarded to destination service only on explicit policy allow

At the **sidecar (east-west)**, the Envoy sidecar performs:
1. mTLS handshake validation using SPIFFE SVIDs — caller must present a valid SVID from the Orbit trust domain (ADR-0010)
2. Request rejected at the TLS layer if the caller has no valid SVID
3. OPA policy evaluation via `ext_authz` — workload identity context constructed from the caller's SVID, policy bundle evaluated locally
4. 403 returned if workload identity is valid but policy denies the call
5. Structured audit event emitted to the Orbit event spine
6. Request forwarded to the local application process only on explicit policy allow

### Layer 2 — In-Application (Owned Services Only, Optional)

Owned services may implement fine-grained business-logic authorization within the application process. This layer answers: **"Within this permitted request, what data can this principal see or modify?"**

This covers concerns the proxy layer cannot address:
- Row-level security (e.g. "this agent can query deployments but only for its own tenant's resources")
- Response field filtering (e.g. "strip credential values from responses to agents without the `credentials:read:values` scope")
- State-dependent rules (e.g. "this operation is permitted only if the resource is in `draft` state")

Layer 2 is **additive** — it runs only after Layer 1 has already allowed the request. It is not a substitute for Layer 1. Third-party services do not implement Layer 2; Layer 1 is the complete enforcement boundary for those services.

### Architectural Constraint

No service — owned or third-party — may expose a network interface that is reachable without passing through a Layer 1 enforcement point. This constraint is enforced topologically:

- For Kubernetes deployments: `NetworkPolicy` rules deny direct pod-to-pod traffic that bypasses the sidecar
- For the edge: services are not exposed on public or cluster-external interfaces — only the Orbit Gateway has external ingress
- Service registration with Orbit is required before a service receives routable SPIFFE identity or appears in gateway routing config — unregistered services are unreachable by construction

---

## OPA Policy Distribution

OPA policy is distributed to all enforcement points (edge Envoy and all sidecars) via **bundle distribution** — the OPA bundle server (managed by the Orbit policy control plane) compiles and serves policy bundles. Enforcement points poll for bundle updates on a configured interval.

**Remote OPA API (synchronous per-request policy calls) is explicitly rejected** for the following reasons:
- Adds a synchronous network hop to every request in the platform — the policy service becomes a hard latency and availability dependency for all traffic
- At scale, the policy API becomes a performance bottleneck
- Bundle distribution is the standard OPA production deployment pattern for exactly these reasons

**Eventual consistency trade-off:** Bundle distribution means policy changes propagate on the polling interval (typically 30–60 seconds), not immediately. This is acceptable for the general case. For time-critical revocation (compromised token, terminated agent), the JTI blocklist mechanism from ADR-0008 provides immediate effect — JTI validation is a fast local lookup against a distributed cache, not a policy evaluation. The two mechanisms are complementary.

---

## Consequences

### Positive

- All services — owned, third-party, MCP servers, any future service type — receive identical enforcement guarantees without code changes or Orbit-specific integration work in the service itself
- A single audit trail: all enforcement points emit structured events in the same format to the same event spine — complete platform-wide visibility into all access
- Zero implicit internal trust: service-to-service calls are subject to the same principal and policy validation model as external agent calls
- Third-party MCP servers and open-source components can be deployed into the platform with full enforcement confidence — the sidecar handles everything outside the application boundary
- One data plane technology (Envoy) means one operational model, one team skill set, one OPA integration pattern to maintain

### Negative

- Sidecar operational complexity is real: sidecar injection, config distribution to all sidecars, and sidecar lifecycle management require a control plane. How this is implemented (adopting Istio vs building Orbit-native sidecar management) is an open question — see below.
- Envoy configuration has a higher learning curve than simpler alternatives. Investment in tooling, config templates, and operational documentation is required.
- Every service deployment requires sidecar injection to be correctly configured — a misconfigured or missing sidecar breaks the enforcement model. Admission control (e.g. Kubernetes admission webhooks) should enforce sidecar presence.

### Neutral

- The `ext_authz` service (the Orbit component that Envoy calls for policy decisions) becomes a first-class platform component that must be designed, built, and operated with high availability
- Service registration with Orbit becomes a prerequisite for a service to receive routable identity and appear in gateway config — this is a required operational step for all service deployments

---

## Sidecar Control Plane — Resolved

This ADR establishes the principle and data plane technology. The question of how Envoy sidecar config is distributed and managed is resolved in **[ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md)**.

**Decision summary (ADR-0015):**
- **Istio** is adopted as the sidecar control plane. istiod handles sidecar injection via admission webhook and xDS config distribution to all Envoy sidecar instances and the edge gateway. Istio's per-pod sidecar model is fully compatible with the SPIRE CSI driver socket-mount model defined in ADR-0010 — `istio-proxy` reads the Workload API socket directly via `SPIFFE_ENDPOINT_SOCKET` with no intermediary sidecar required.
- **Cilium** is adopted as the CNI plugin. It operates at the eBPF kernel level underneath Istio, enforcing the "no direct pod-to-pod traffic that bypasses the sidecar" constraint from this ADR at the network layer — significantly harder to bypass than iptables-based NetworkPolicy. Cilium also enforces DNS-aware egress policy, ensuring agent pods cannot call external services directly and must route through the Credential-Aware Outbound Gateway (ADR-0012).

The Orbit-native xDS control plane option is deferred, not closed — it remains the correct long-term direction if Orbit's service registry model requires tighter Envoy config coupling. See ADR-0015 for full rationale.

---

## Implementation Notes

### Service Registration

All services deployed into the platform must be registered with Orbit before becoming routable. Registration produces:
- A SPIFFE registration entry (per ADR-0010)
- A gateway routing rule (for services reachable from outside the platform)
- An OPA policy scope definition (what operations the service exposes and what scopes are required)

Third-party services are registered by the Orbit operator at deployment time. The operator provides the service's endpoint manifest (what paths it exposes) and Orbit generates the policy scope mapping.

### ext_authz Service

The Orbit `ext_authz` service is the component called by Envoy (at both edge and sidecar) to perform policy decisions. It:
- Receives the request context from Envoy (method, path, headers, peer identity)
- Constructs the OPA input document (principal, delegation context, resource, operation)
- Evaluates the locally loaded OPA policy bundle
- Returns allow/deny with structured metadata (deny reason, audit context)
- Emits an audit event to the event spine regardless of outcome

This component must be co-located with each enforcement point for latency — either embedded in the gateway deployment or running as a second container alongside each sidecar.

### Kubernetes Enforcement

- Sidecar injection is mandatory for all service pods — enforced via admission webhook
- `NetworkPolicy` resources deny inter-pod traffic that does not flow through sidecars
- The Orbit Gateway is the only component with an externally routable `Service` of type `LoadBalancer` or `NodePort`
- Internal services use `ClusterIP` services only — not reachable from outside the cluster

---

## Validation

### Success Metrics

- Zero services reachable from inside or outside the cluster without passing through a Layer 1 enforcement point — validated by network policy audit and penetration testing
- 100% of audit events for requests to platform services captured in the event spine — no gap between enforcement points
- Third-party services (including MCP servers) successfully enforced with zero application code changes
- OPA policy bundle propagation latency is within the configured polling interval under normal operating conditions
- No single enforcement point failure causes platform-wide request failure (east-west failure isolation validated)

### Monitoring

- Envoy `ext_authz` metrics: allow rate, deny rate, evaluation latency per service
- OPA bundle sync metrics: last successful sync timestamp, bundle version per enforcement point
- Audit event completeness: cross-reference enforcement point event counts against service request counts
- JTI blocklist hit rate: distinguishes token revocation events from policy denials

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — Defines the principal model and platform session token that the gateway validates
- [ADR-0009](0009-agent-execution-flow-patterns-and-delegation.md) — Defines delegation tokens and execution context documents evaluated at enforcement points
- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — Defines SPIFFE SVIDs used for mTLS at east-west sidecar enforcement points
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Resolves the sidecar control plane open question: Istio as service mesh, Cilium as CNI-level network enforcement

---

## References

- [Envoy ext_authz filter documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter)
- [OPA Envoy plugin](https://www.openpolicyagent.org/docs/latest/envoy-introduction/)
- [OPA Bundle distribution](https://www.openpolicyagent.org/docs/latest/management-bundles/)
- [SPIFFE SDS integration with Envoy](https://spiffe.io/docs/latest/microservices/envoy/)
- [CNCF zero-trust networking principles](https://tag-security.cncf.io/blog/zero-trust/)

---

## Notes

This ADR establishes the enforcement topology principle. The specific Envoy configuration patterns, `ext_authz` service API contract, OPA input document schema, and audit event schema are implementation concerns to be documented in subsequent engineering specifications.

**Last Updated:** 2026-03-03

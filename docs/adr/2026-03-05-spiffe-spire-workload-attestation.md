# SPIFFE/SPIRE Workload Attestation

**Status:** Accepted
**Date:** 2026-03-05
**Deciders:** Engineering Team
**Technical Story:** Defining how non-human workloads (agents and services) establish cryptographic identity in the Orbit platform, implementing the ATTESTED identity source specified in [unified-identity-model](2026-03-05-unified-identity-model.md).

---

## Context

[unified-identity-model](2026-03-05-unified-identity-model.md) established that Orbit supports two identity sources: `FEDERATED` (OIDC/SAML for human principals) and `ATTESTED` (workload attestation for agent and service principals). This ADR covers the implementation of the `ATTESTED` source — how agents and other non-human workloads prove their identity to Orbit without pre-shared secrets.

### Background

Non-human workload identity is a hard problem. The naive approach — issuing API keys or client secrets to agents — has well-understood failure modes: secrets get stored in code or environment variables, they don't rotate automatically, they can't be tied to the specific workload that should use them, and revoking them is a manual operational event.

SPIFFE (Secure Production Identity Framework For Everyone) and its reference implementation SPIRE (SPIFFE Runtime Environment) solve this correctly. SPIFFE defines a standard for workload identity based on cryptographic attestation — a workload proves its identity by virtue of where it's running and what it is, not by knowing a secret. SPIRE automates the issuance and rotation of short-lived identity documents (SVIDs) to attested workloads with no human or application involvement in the rotation cycle.

SPIFFE/SPIRE is a CNCF graduated project and is production-proven at scale (Uber, Square, GitHub, and others run it at significant scale). It is the correct foundation for agent identity in Orbit rather than a problem to solve from scratch.

### Problem Statement

We need to define:
1. The SPIFFE trust domain topology for a multi-tenant Orbit instance
2. The SPIFFE ID naming convention for agents and services
3. How SPIRE Server and SPIRE Agent are deployed relative to Orbit
4. How agent registration in Orbit maps to SPIRE registration entries
5. How agents exchange SVIDs for Orbit platform session tokens
6. How deregistration and revocation are handled

### Goals

- **No pre-shared secrets**: Agent workloads receive cryptographic identity automatically, without secrets being provisioned at deployment time
- **Automatic rotation**: SVIDs rotate on short TTLs without any action from the agent workload or Orbit operators
- **Tenant isolation**: Agent identity is unambiguously scoped to a tenant within the SPIFFE ID itself
- **Tight coupling with agent registry**: A SPIRE registration entry must exist if and only if a corresponding agent principal is registered in Orbit
- **Platform token integration**: SVID exchange produces the standard platform session token from [unified-identity-model](2026-03-05-unified-identity-model.md) — the agent's experience downstream is identical to any other authenticated principal

### Non-Goals

- Replacing SPIFFE/SPIRE with a custom PKI (SPIRE is adopted, not reinvented)
- Supporting human authentication via SPIFFE (humans use OIDC/SAML per [unified-identity-model](2026-03-05-unified-identity-model.md))
- Defining per-tenant SPIRE Server deployments (addressed in the topology decision below)
- Covering the enforcement topology for mutual TLS and policy enforcement between internal platform services (defined in [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md))
- Defining SPIFFE federation with external organisations or trust domains

---

## Decision Drivers

* **Security**: No long-lived secrets. Identity tied to workload runtime characteristics, not possession of a credential
* **Operational automation**: SVID rotation must be fully automatic — zero-touch for the agent workload and for Orbit operators
* **Multi-tenancy**: Tenant scope must be unambiguous in the SPIFFE ID — policy cannot rely solely on the platform layer to enforce it
* **Operational simplicity**: The SPIRE deployment topology must be manageable by a small platform team, not require per-tenant infrastructure
* **Integration fidelity**: The SVID-to-platform-token exchange must produce a token structurally identical to a human session token (per [unified-identity-model](2026-03-05-unified-identity-model.md))
* **Revocation latency**: Agent identity revocation must be effective within a short, bounded window

---

## Considered Options

### Option A: Pre-shared API keys for agent identity

**Description:** Issue a long-lived API key to each registered agent at registration time. The agent presents the key on each request. Orbit validates the key against its registry.

**Pros:**
- Simple to implement — no new infrastructure
- Familiar pattern for engineers

**Cons:**
- Keys must be stored somewhere — environment variables, secrets managers, config files — all of which are attack surfaces
- Keys don't rotate automatically; rotation requires coordinated deployment
- A compromised key is valid until manually revoked — no attestation that the key is being used by the expected workload
- Does not scale to dynamic agent workloads that are created and destroyed frequently
- Directly contradicts the security principle of not relying on pre-shared secrets for workload identity

### Option B: SPIFFE/SPIRE with a single trust domain per Orbit instance, tenant-scoped by path

**Description:** Deploy one SPIRE Server per Orbit instance. All tenants share the same SPIFFE trust domain (`spiffe://orbit.{instance}`). Tenant scope is encoded in the SPIFFE ID path. SPIRE Agents run on every compute node where agent workloads execute. Orbit's identity service validates SVIDs against the shared trust bundle.

**Pros:**
- One SPIRE Server to operate per Orbit instance — manageable by a small platform team
- Tenant isolation is expressed unambiguously in the SPIFFE ID path — no ambiguity about which tenant an agent belongs to
- All tenants share the same cryptographic root of trust — cross-tenant verification (if ever explicitly permitted by policy) is straightforward
- Operationally, adding a new tenant requires only new registration entries, not new infrastructure
- Clear upgrade path: if a customer requires cryptographic tenant isolation (e.g. for compliance), per-tenant trust domains can be introduced later

**Cons:**
- Cryptographic isolation between tenants is at the policy layer, not the PKI layer — a compromised SPIRE Server would have visibility across all tenants
- Tenant isolation in the SPIFFE ID path is convention-enforced, not structurally enforced by the PKI

### Option C: SPIFFE/SPIRE with a separate trust domain per tenant

**Description:** Each tenant gets its own SPIFFE trust domain (`spiffe://{tenant-id}.orbit.{instance}`) backed by either a dedicated SPIRE Server or a SPIRE nested-server topology.

**Pros:**
- Cryptographic isolation at the CA boundary — a team-a SVID cannot be verified against team-b's trust bundle by construction
- Strongest possible tenant isolation model

**Cons:**
- Either N SPIRE Servers (one per tenant) — operationally very heavy, scales poorly
- Or SPIRE nested/federated topology — complex to configure, reason about, and operate
- For most multi-tenant control plane use cases, CA-level isolation between tenants is disproportionate — the policy layer is the correct enforcement boundary
- Migration path exists (Option B → Option C) if a customer requires it, so the conservative choice is Option B

---

## Decision

**Chosen Option:** Option B — SPIFFE/SPIRE with a single trust domain per Orbit instance, tenant-scoped by SPIFFE ID path.

### Rationale

Option A is rejected outright — pre-shared secrets are the wrong foundation for agent identity in a platform designed for autonomous workloads at scale.

Option B is preferred over Option C on operational grounds. Running per-tenant SPIRE infrastructure is a significant burden that does not pay off for the common multi-tenant case. The path convention in the SPIFFE ID makes tenant scope unambiguous and machine-readable. The policy layer — which all requests pass through — enforces tenant isolation. If a specific customer or deployment scenario requires CA-level tenant isolation in the future, the migration path from Option B to Option C is well-defined: introduce nested SPIRE Servers with per-tenant intermediate CAs, or federate separate SPIRE instances. This is a day-two problem.

---

## SPIFFE Trust Domain and ID Structure

### Trust Domain

```
spiffe://orbit.{instance-id}
```

`{instance-id}` is configured at Orbit deployment time. It should be a stable, human-readable identifier for the Orbit installation — typically a short slug derived from the organisation or environment (e.g. `orbit.acme`, `orbit.acme-prod`).

### SPIFFE ID Naming Convention

```
# Agent workloads (registered autonomous agents)
spiffe://orbit.{instance-id}/tenant/{tenant-id}/agent/{agent-id}

# Platform services (internal Orbit components)
spiffe://orbit.{instance-id}/system/{component-name}

# Tenant-scoped services (non-agent workloads registered to a tenant)
spiffe://orbit.{instance-id}/tenant/{tenant-id}/service/{service-name}
```

Concrete examples:

```
spiffe://orbit.acme/tenant/platform-team/agent/ci-deploy-bot
spiffe://orbit.acme/tenant/data-team/agent/pipeline-orchestrator
spiffe://orbit.acme/tenant/platform-team/service/webhook-receiver
spiffe://orbit.acme/system/policy-engine
spiffe://orbit.acme/system/credential-broker
```

**Conventions:**
- `{tenant-id}` matches the `tenant_id` field on the Orbit Principal — it is the stable identifier for the team/organisation unit, not a display name
- `{agent-id}` matches the agent's entry in Orbit's agent registry — it is Orbit's own assigned identifier, not a name that varies between environments
- `{component-name}` for system entries uses kebab-case matching the Orbit service name
- SPIFFE IDs are permanent for the lifetime of the registration — they do not change when an agent is redeployed, scaled, or updated

---

## SPIRE Deployment Architecture

### Components

**SPIRE Server**
- Deployed as a stateful workload adjacent to the Orbit control plane
- Manages the registration entry database (which SPIFFE IDs exist, which selectors qualify a workload for each)
- Operates as the certificate authority for the `spiffe://orbit.{instance-id}` trust domain
- Issues SVIDs to SPIRE Agents for distribution to attested workloads
- Exposes a gRPC API consumed by Orbit's identity service for registration entry management

**SPIRE Agent**
- Runs as a DaemonSet (Kubernetes) or system process on every compute node hosting agent workloads
- Performs **node attestation** on startup — proves to the SPIRE Server that it is running on a legitimate, expected node (using Kubernetes PSAT, cloud provider instance identity documents, or TPM attestation depending on deployment environment)
- Serves the **SPIFFE Workload API** on a Unix domain socket on the node
- Performs **workload attestation** — when a workload calls the Workload API, the SPIRE Agent inspects the calling process's attributes (namespace, service account, container image, UID, etc.) and matches them against registration entries
- Manages **SVID rotation** automatically — workloads receive new SVIDs before the current one expires, with no involvement from the workload

**SPIFFE Workload API (consumed by the Envoy sidecar via a CSI-mounted socket)**
- The SPIRE Agent exposes the Workload API on a Unix domain socket on each node
- The **SPIRE CSI driver** (a node-level DaemonSet — `spire-spiffe-csi-driver`) mounts the SPIRE Agent socket into each pod as a volume at `/run/secrets/workload-spiffe-uds/socket`
- The Istio-injected `istio-proxy` (Envoy) sidecar reads the socket directly via the `SPIFFE_ENDPOINT_SOCKET` environment variable — it has native SPIRE integration and obtains SVIDs this way without any intermediary sidecar
- The agent workload itself does not interact with the Workload API — SVID management is entirely below the application layer

> **Note — actual pod composition per agent pod:** An agent workload in Orbit's Kubernetes deployment has two running containers and one node-level component managing SPIFFE identity:
>
> 1. **SPIRE CSI driver** (node-level DaemonSet, not a pod sidecar) — mounts the SPIRE Agent's Workload API socket into pods as a volume (`spire-workload-socket`, read-only at `/run/secrets/workload-spiffe-uds/socket`). This is the mechanism by which pods access their SVID without a per-pod SPIFFE Helper container. The CSI driver runs once per node; all pods on that node share access to their node's SPIRE Agent socket through it.
>
> 2. **`istio-proxy` sidecar** ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md), [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md)) — the Istio-injected Envoy sidecar. Reads the SPIRE Workload API socket directly via `SPIFFE_ENDPOINT_SOCKET: unix:///run/secrets/workload-spiffe-uds/socket`. Enforces mTLS on all inbound and outbound traffic using the SVID obtained from the mounted socket. Will delegate outbound request header injection to the identity-proxy via the ext_proc filter once identity-proxy is deployed.
>
> 3. **identity-proxy sidecar** ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md), planned — not yet deployed) — a platform-owned Go binary that connects to the same mounted SPIRE Workload API socket via go-spiffe, exchanges the SVID for a platform JWT, caches the token with a shared-state refresh loop, and injects `Authorization: Bearer` on every outbound request via Envoy's ext_proc protocol. The agent workload never holds or manages tokens directly.
>
> There is **no SPIFFE Helper sidecar container** in any pod. The SPIFFE Helper role is fulfilled by the SPIRE CSI driver at the node level. Agent workload authors interact with none of these components — **the agent workload is fully identity-unaware: it makes plain HTTP calls and the platform layer handles everything below the surface.**

---

## Registration Entry Lifecycle

SPIRE registration entries are the link between Orbit's agent registry and SPIRE's identity model. A registration entry specifies:
- The **SPIFFE ID** to be issued to the matching workload
- The **parent ID** — which SPIRE Agent node is authorised to issue this (i.e. on which node the workload is expected to run)
- The **selectors** — workload attributes that must match for the SPIFFE ID to be issued (e.g. Kubernetes namespace + service account name, container image digest, or Linux process UID)

### Agent Registration (Two-Phase Write)

When an operator registers a new agent in Orbit:

1. Orbit creates the agent principal record in its own registry (tenant-scoped, with type `AGENT` and source `ATTESTED`)
2. Orbit calls the SPIRE Server gRPC API to create a corresponding registration entry:
   - SPIFFE ID: `spiffe://orbit.{instance-id}/tenant/{tenant-id}/agent/{agent-id}`
   - Parent ID: the SPIRE Agent node(s) authorised for this workload
   - Selectors: derived from the agent's deployment specification (namespace + service account for Kubernetes, or equivalent)
3. Both writes must succeed for registration to complete. If the SPIRE write fails, the Orbit agent record is rolled back.

**The SPIRE Server is a hard dependency of the agent registration path.** This is an intentional coupling — an agent with no SPIRE registration entry cannot receive a valid SVID and therefore cannot authenticate to Orbit. The two registries must stay in sync.

### Agent Deregistration and Revocation

When an agent is deregistered or administratively revoked:

1. Orbit deletes the SPIRE registration entry via the SPIRE Server API — SPIRE immediately stops issuing or renewing SVIDs for that SPIFFE ID
2. Orbit adds the agent's active platform token JTIs to the JTI blocklist ([unified-identity-model](2026-03-05-unified-identity-model.md) revocation model)
3. Orbit marks the agent principal record as revoked in its registry

Effective revocation is near-immediate:
- Any in-flight request using a blocklisted platform token JTI is rejected at the next API call
- The existing SVID will expire at its natural TTL (≤1 hour, typically 15 minutes) and no new SVID will be issued
- Any attempt to exchange the expired SVID for a new platform token will fail because the registration entry no longer exists

For immediate forced revocation (e.g. a compromised agent), the blocklist ensures all active platform tokens are invalidated without waiting for SVID expiry.

---

## SVID-to-Platform-Token Exchange Flow

### Why the exchange is necessary

SPIFFE SVIDs are workload identity certificates. An X.509 SVID carries exactly three things: the SPIFFE ID URI, the validity window, and the CA chain. **SVIDs cannot carry scope-bound principal claims** — no tenant policy, no registered permissions, no principal type, no display name. This is by design: SVIDs are an identity proof mechanism, not a capability token format.

Policy evaluation requires a richer, scope-bound credential. The OPA execution context ([agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md)) needs the full Principal with tenant scope, claims, and identity source. Assembling that from a raw SPIFFE ID requires a lookup against Orbit's agent registry — something only Orbit's identity service can perform.

The platform session token is the output of that enrichment: a short-lived, signed JWT carrying the normalised Principal that all downstream enforcement points evaluate. The `/identity/exchange` endpoint is the bridge between the two identity worlds. It cannot be eliminated — but it is made completely invisible to the agent workload.

### Who performs the exchange

The Envoy sidecar performs the exchange on behalf of the agent workload. The agent workload never calls `/identity/exchange` directly. The full sidecar-managed flow is:

```
1.  Agent workload starts on a compute node.

2.  SPIRE Agent on the node performs workload attestation:
    - Inspects the calling process attributes (namespace, service account, etc.)
    - Matches against registration entries in SPIRE Server
    - Issues an SVID: X.509 certificate with the SPIFFE ID as the Subject Alternative Name (SAN)
    - SVID TTL: configured per entry, typically 1 hour or less

3.  SPIRE sidecar holds the current SVID bundle (certificate + private key).
    - Fetched from the SPIRE Agent via the SPIFFE Workload API
    - Exposes the SVID to Envoy via the SPIFFE SDS (Secret Discovery Service) API
    - Rotates the SVID automatically before expiry — no agent workload involvement

4.  Envoy sidecar, on first outbound request (or proactively at startup), calls
    Orbit's identity exchange endpoint on behalf of the agent:

    POST /identity/exchange
    (mTLS using the SVID obtained from SPIRE sidecar via SDS)
    Content-Type: application/json
    {
      "svid_type": "x509"
    }

    Note: this endpoint is gateway-exempt — it is the bootstrap call made before
    any platform token exists. For X.509 SVID exchanges, the identity service uses
    the mTLS peer certificate as the SVID source of truth and does not require the
    SVID in the request body. For JWT-SVID exchanges, the SVID must be provided in
    the body as a bearer token (svid_type: "jwt", svid: "<JWT>").

5.  Orbit's identity service validates the SVID:
    a. For X.509 SVID exchanges: obtains the SVID from the mTLS peer certificate
       presented during the TLS handshake and verifies the certificate chain against
       the SPIRE trust bundle for this instance. If an X.509 SVID is also provided
       in the request body, it MUST match the mTLS peer certificate.
    b. Checks certificate validity period (not expired, not before)
    c. Extracts the SPIFFE ID from the certificate SAN
    d. Validates the SPIFFE ID path structure (correct trust domain, present tenant segment)

6.  Orbit looks up the agent principal:
    a. Parses tenant-id and agent-id from the SPIFFE ID path
    b. Queries the agent registry for a matching principal record
    c. Verifies the principal is in active (not revoked) status
    d. Verifies the tenant is active

7.  Orbit issues a platform session token ([unified-identity-model](2026-03-05-unified-identity-model.md)):
    - Subject: agent principal ID
    - Claims: principal type, tenant_id, identity_source=ATTESTED, display_name, spiffe_id claim
    - TTL: min(configured platform token TTL, remaining SVID validity) — typically 15 minutes
    - Signed with Orbit's platform token signing key

8.  Orbit returns the platform session token to the Envoy sidecar.
    The Envoy sidecar caches it internally. The token is never exposed to the agent workload.

9.  Agent workload makes a plain HTTP call inside the pod — no token attached:
    GET /some/resource

10. Envoy intercepts the outbound request and injects the platform token:
    Authorization: Bearer {platform-session-token}
    The request is forwarded to its destination with the token attached transparently.

11. When the SVID rotates (fully automated by the SPIRE sidecar):
    - Envoy detects the new SVID via SDS
    - Envoy proactively re-calls POST /identity/exchange with the new SVID
    - Orbit issues a new platform session token
    - Envoy updates its cached token
    - The agent workload continues operating without any interruption or awareness of the rotation
```

**The agent workload is completely identity-unaware.** It holds no SVIDs, manages no tokens, and makes no calls to any identity endpoint. It simply makes HTTP calls. The sidecar layer handles the full identity lifecycle below the surface.

### What the agent workload IS aware of: delegation context

Authentication (who is this workload?) is fully transparent. Delegation context (under whose authority am I acting?) is not — and intentionally so.

When an agent operates under a human delegation grant ([agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) Pattern 1), the delegation token is task-specific: it was issued for a particular scope, for a particular job. The agent workload must know it has been delegated to. It attaches the `X-Orbit-Delegation-Token` header explicitly when making requests that require delegated authority.

This is the correct split of concerns:
- **Authentication infrastructure** — invisible to the agent, owned by the sidecar layer
- **Delegation context** — visible to the agent, because delegation is an application-level concern tied to what the agent is doing, not who it is

---

## Node Attestation Strategy

SPIRE Agents must prove to the SPIRE Server that they are running on a legitimate node before they can issue SVIDs to workloads. The node attestation method depends on the deployment environment:

| Environment | Recommended Attestation Method |
|---|---|
| Kubernetes | Projected Service Account Token (PSAT) — SPIRE verifies the node's service account token against the Kubernetes API |
| AWS | EC2 Instance Identity Document (IID) — SPIRE verifies the instance's cryptographically signed identity document |
| GCP | GCE Instance Identity Token — similar to AWS IID |
| Azure | Azure MSI — managed identity attestation |
| Bare metal / local dev | Join token (acceptable for dev/test; not for production) |

The initial Orbit deployment targets Kubernetes, so **PSAT attestation** is the default. This means SPIRE Agents run with a Kubernetes service account and SPIRE Server verifies their identity via the Kubernetes token review API.

---

## Workload Selector Strategy (Kubernetes)

For workloads running in Kubernetes, the recommended selectors for SPIRE registration entries are:

```
k8s:ns:{namespace}           — Kubernetes namespace the workload runs in
k8s:sa:{service-account}     — Kubernetes service account assigned to the pod
```

These two selectors together provide strong workload identity — a workload must be running in the correct namespace with the correct service account to receive the SPIFFE ID. This maps naturally to Orbit's tenant model: each tenant's agents run in a dedicated namespace.

Optionally, for higher assurance environments:

```
k8s:container-image:{digest}  — Pins identity to a specific container image digest
```

This ensures that only a specific known image can receive the SPIFFE ID — useful for high-privilege agents where image integrity is critical.

---

## Consequences

### Positive

- Agents receive cryptographic identity with no pre-shared secrets — eliminates an entire category of credential management risk
- SVID rotation is fully automated by the SPIRE sidecar and the downstream platform token refresh is triggered automatically by Envoy — neither the agent workload nor Orbit operators are involved in any part of the credential rotation lifecycle
- The SPIFFE ID path encodes tenant scope directly — tenant isolation is machine-readable without additional context
- The SVID-to-platform-token exchange produces a standard platform session token — agents are indistinguishable from human principals to downstream platform components
- Short SVID TTLs (≤1 hour) mean the blast radius of a compromised credential is inherently bounded
- Agent workloads are pure HTTP clients — they hold no SVIDs, manage no tokens, and call no identity endpoints. Authentication infrastructure is entirely owned by the sidecar layer. Agent developers write application logic, not identity plumbing

### Negative

- SPIRE Server is a new critical infrastructure dependency — it must be highly available. A SPIRE Server outage does not invalidate existing SVIDs (they continue to be valid until expiry) but prevents new SVIDs from being issued, meaning agents whose SVIDs expire during an outage cannot authenticate
- SPIRE Server is in the critical path for agent registration — agent onboarding requires SPIRE availability
- SPIRE Agents must be deployed on every compute node hosting agent workloads — this is an operational requirement that must be factored into cluster provisioning
- Engineers unfamiliar with SPIFFE/SPIRE face a learning curve — the concepts (attestation, selectors, SVIDs) are not universal knowledge

### Neutral

- The SVID exchange endpoint (`POST /identity/exchange`) is a new Orbit API with no equivalent in the human identity flow — it is additive, not a change to existing interfaces. The caller is the Envoy sidecar, not the agent workload itself
- SPIRE's trust bundle (the CA certificate set) must be distributed to all Orbit identity service instances for SVID validation — this is handled automatically by SPIRE's bundle API

---

## Implementation Notes

### SPIRE Server Integration

Orbit's identity service interacts with SPIRE Server via its gRPC API (`github.com/spiffe/spire-api-sdk`). The key API surfaces used:

- `Entry` service (`spire.api.server.entry.v1`) — create, list, delete registration entries (called during agent register/deregister)
- `Bundle` service — fetch the current trust bundle for SVID validation
- `Agent` service — list attested SPIRE Agents (useful for health monitoring)

The identity service should cache the trust bundle locally and refresh it on a short interval (e.g. every 5 minutes) — SVID validation must not require a round-trip to SPIRE Server on every token exchange.

### SVID Validation

X.509 SVID validation in Go uses the `github.com/spiffe/go-spiffe/v2` library, which handles:
- Certificate chain verification against the trust bundle
- SPIFFE ID extraction from the SAN extension
- URI SAN format validation per the SPIFFE standard

### SPIRE CSI Driver — Workload API Socket Delivery

There is no SPIFFE Helper sidecar container in Orbit pods. Instead, the **SPIRE CSI driver** (`spiffe/spiffe-csi-driver`) runs as a node-level DaemonSet (`spire-spiffe-csi-driver`) and mounts the SPIRE Agent's Workload API socket into each pod as a read-only CSI volume:

- **Volume name**: `spire-workload-socket`
- **Mount path**: `/run/secrets/workload-spiffe-uds/socket` (read-only)
- **Socket env var**: `SPIFFE_ENDPOINT_SOCKET=unix:///run/secrets/workload-spiffe-uds/socket`

The Istio-injected `istio-proxy` (Envoy) sidecar reads the mounted socket directly — Istio has native SPIRE/SPIFFE integration and obtains SVIDs without an intermediary SDS container. The `identity-proxy` sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) also mounts the same volume to obtain its own SVID source for the platform token exchange.

The agent workload never consumes the SVID directly. SVID management is entirely below the application layer — handled by `istio-proxy` reading the CSI-mounted socket. Orbit provides a reference pod specification that wires the CSI volume mount, `istio-proxy`, and `identity-proxy` together correctly. Agent workload authors do not configure any of these components.

### Migration Path

For the initial implementation, node attestation uses Kubernetes PSAT. If Orbit is later deployed on non-Kubernetes infrastructure, the node attestation method is swappable without changing the SPIFFE ID structure or the SVID exchange flow — only the SPIRE Agent configuration changes.

If a future compliance requirement mandates per-tenant cryptographic isolation, the migration path is:
1. Introduce SPIRE nested server per tenant with a per-tenant intermediate CA
2. Update the trust domain to `spiffe://{tenant-id}.orbit.{instance-id}` for new tenants
3. Migrate existing tenants by re-issuing SVIDs under the new trust domain with a transition period

---

## Validation

### Success Metrics

- Agent workloads receive valid SVIDs within 30 seconds of startup on an attested node
- SVID rotation completes without agent workload restart or interruption — verified by continuous integration tests that run agents through multiple SVID rotation cycles
- Agent deregistration results in SVID non-renewal within one TTL period
- JTI blocklist revocation of a platform token is effective within one Redis round-trip — verified by revocation integration tests
- Zero pre-shared secrets in any agent deployment configuration — verified by static analysis of agent deployment manifests

### Monitoring

- SVID issuance rate and failure rate per SPIRE Agent node
- Token exchange request rate, latency, and failure rate (broken down by failure reason: invalid SVID, unknown agent, revoked principal, expired certificate)
- SPIRE Server availability and trust bundle refresh success
- Registration entry count per tenant (tracks agent fleet growth)
- Platform token issuance latency (end-to-end: SVID presented → token returned)

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — Defines the ATTESTED identity source and the platform session token this ADR implements
- [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — Delegation and execution flow patterns that build on the agent identity established here
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Defines how SPIFFE SVIDs are used by Envoy sidecars for mTLS enforcement on east-west traffic, and the Envoy SDS integration that consumes SVIDs issued by the SPIRE Agent
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — Resolves the open implementation question in this ADR: the concrete mechanism by which the sidecar layer performs the SVID-to-platform-token exchange and injects the resulting token on outbound requests, implemented as the identity-proxy Go binary using Envoy's ext_proc filter protocol

---

## References

- [SPIFFE Standard Overview](https://spiffe.io/docs/latest/spiffe-about/overview/)
- [SPIRE Documentation](https://spiffe.io/docs/latest/spire-about/)
- [SPIFFE X.509-SVID Specification](https://github.com/spiffe/spiffe/blob/main/standards/X509-SVID.md)
- [SPIFFE JWT-SVID Specification](https://github.com/spiffe/spiffe/blob/main/standards/JWT-SVID.md)
- [go-spiffe library](https://github.com/spiffe/go-spiffe)
- [spire-api-sdk](https://github.com/spiffe/spire-api-sdk)
- [SPIRE Controller Manager](https://github.com/spiffe/spire-controller-manager)

---

## Notes

JWT-SVIDs (as opposed to X.509 SVIDs) are an alternative SVID format that may be preferable in environments where mTLS is not the transport layer (e.g. HTTP-only internal networks). The exchange endpoint design is agnostic to SVID type — `svid_type` in the request body allows both X.509 and JWT SVIDs to be supported. The initial implementation targets X.509 SVIDs as these are the more common and better-supported format, but JWT-SVID support can be added without changing the exchange endpoint contract.

**Last Updated:** 2026-03-03

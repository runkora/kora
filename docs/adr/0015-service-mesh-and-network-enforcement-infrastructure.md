# 0015. Service Mesh and Network Enforcement Infrastructure

**Status:** Accepted
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Resolves the open sidecar control plane question from ADR-0011, selecting Istio as the service mesh control plane and Cilium as the CNI-level network enforcement layer, and documenting how the two work together to implement the universal enforcement model.

---

## Context

ADR-0011 established Envoy as the unified data plane for policy enforcement across all platform traffic — centralised edge deployment for north-south and per-pod sidecar deployment for east-west. It explicitly left one question open:

> *"How Envoy sidecar config is distributed and managed across the platform... [is] an open question — see below."*
>
> Two paths were identified:
> - **Path 1:** Adopt a service mesh (e.g. Istio) — istiod manages sidecar injection and Envoy config distribution
> - **Path 2:** Orbit as xDS control plane — Orbit implements the Envoy xDS APIs directly

This ADR resolves that question and additionally defines the CNI-level network enforcement layer that underpins the sidecar enforcement model.

### Background

The sidecar control plane decision is not a detail — it determines the entire operational model for how Envoy sidecars are injected, how they receive configuration, and how the per-pod SPIFFE identity flow (defined in ADR-0010) is wired to the per-pod Envoy sidecar.

ADR-0010 defines the per-pod identity model: each pod has a running workload container and an Istio-injected `istio-proxy` sidecar (Envoy). There is no SPIFFE Helper sidecar container. Instead, the **SPIRE CSI driver** (a node-level DaemonSet) mounts the SPIRE Agent's Workload API socket into each pod as a volume. The `istio-proxy` sidecar reads the socket directly via `SPIFFE_ENDPOINT_SOCKET` — Istio has native SPIRE/SPIFFE integration that does not require an intermediary sidecar.

The per-pod composition is therefore:
1. **Workload container** — the agent, MCP server, or platform service; fully identity-unaware
2. **`istio-proxy` sidecar** — Istio-injected Envoy; reads the CSI-mounted SPIRE socket for SVID-backed mTLS; will use the ext_proc filter for platform token injection once identity-proxy is deployed (ADR-0021)
3. **identity-proxy sidecar** (planned, ADR-0021) — platform-owned Go binary that exchanges the SVID for a platform JWT and injects it on outbound requests via ext_proc

This model requires a per-pod Envoy sidecar with access to the CSI-mounted SPIRE socket. The control plane decision must be compatible with this per-pod sidecar requirement.

### The Cilium Question

Cilium is an eBPF-based networking layer with its own service mesh story (Cilium Service Mesh) based on a shared per-node Envoy proxy rather than per-pod sidecars. This sidecar-free model is explicitly **incompatible** with ADR-0010's per-pod sidecar architecture:

- Cilium's per-node Envoy has no direct access to the CSI-mounted SPIRE Workload API socket, which is a per-pod volume
- There is no per-pod Envoy to read the socket and perform per-pod SVID-backed mTLS
- The transparent platform token injection model — which keeps agent workloads fully identity-unaware — breaks without a per-pod Envoy that can run the identity-proxy ext_proc filter

Cilium as a **service mesh control plane** is rejected for this reason. However, Cilium as a **CNI plugin** operates at a different layer entirely and is not in conflict with the per-pod sidecar model. Its role in the platform is addressed below.

### Core Requirement

The control plane decision must preserve the property that **agent workloads are fully identity-unaware** — they make plain HTTP calls and the sidecar layer handles SVID management, platform token exchange, mTLS, and credential injection transparently. This is non-negotiable; it is the primary goal of the per-pod sidecar model in ADR-0010 and the enforcement model in ADR-0011.

### Goals

- Select and document the sidecar control plane for Orbit's Kubernetes deployment
- Define how Envoy sidecar injection is managed for all service pods
- Define how Envoy configuration is distributed to sidecars and the edge gateway
- Define the network-level enforcement layer that enforces the sidecar-or-nothing constraint from ADR-0011
- Preserve full agent workload transparency — no identity or credential infrastructure visible to agent code

### Non-Goals

- Re-litigating the Envoy unified data plane decision (resolved in ADR-0011)
- Re-litigating the per-pod two-sidecar model (resolved in ADR-0010)
- Defining the edge Envoy gateway deployment (that is a separate operational concern from the sidecar control plane)
- Defining Istio-level traffic management features beyond what Orbit's enforcement model requires

---

## Decision Drivers

* **Agent transparency**: the control plane must support per-pod sidecar injection so that the Envoy sidecar can perform per-pod SVID-to-platform-token exchange invisibly to the agent workload
* **SPIFFE/SPIRE integration**: the control plane must support native SPIRE/SPIFFE integration with the Istio-injected Envoy sidecar — specifically, the ability to read SVIDs directly from the SPIRE CSI driver's mounted Workload API socket via `SPIFFE_ENDPOINT_SOCKET`, without requiring an intermediary SPIFFE Helper sidecar container
* **Operational maturity**: the control plane must be production-proven at scale; Orbit cannot afford to invest in a custom xDS implementation before the core platform is built
* **ext_authz + OPA support**: the control plane must support Envoy's `ext_authz` filter with OPA as the authorisation service — this is the foundation of ADR-0011's policy enforcement model
* **Sidecar injection automation**: sidecar injection must be automatic and enforced via admission webhook — manual sidecar configuration is not acceptable at scale
* **Network policy enforcement strength**: the architectural constraint in ADR-0011 that "no service is reachable without passing through a Layer 1 enforcement point" must be enforced at the network layer, not just by convention

---

## Considered Options

### Option A: Istio as service mesh control plane

**Description:** Adopt Istio as the service mesh. Istio's control plane (`istiod`) manages Envoy sidecar injection via an admission webhook and distributes Envoy configuration to all sidecars and the edge gateway via the xDS protocol. Istio's integration with SPIFFE/SPIRE for workload identity is mature and well-documented.

**Pros:**
- istiod handles sidecar injection automatically — no manual Envoy config per pod
- xDS config distribution to all sidecar instances is built-in — Orbit does not need to implement this
- Mature, production-proven SPIFFE/SPIRE integration — Istio can consume SVIDs from SPIRE via the SPIRE Controller Manager and distribute them to Envoy sidecars for mTLS
- `ext_authz` + OPA on Envoy is a standard, well-documented production pattern on Istio
- Strong community, extensive operational documentation, broad Kubernetes ecosystem support
- Per-pod sidecar model is Istio's native deployment model — fully compatible with ADR-0010's two-sidecar architecture

**Cons:**
- Significant operational commitment — Istio has a learning curve and adds cluster complexity
- istiod is a new critical dependency — must be highly available
- Some Istio defaults (mutual TLS modes, traffic management) require careful configuration to align with Orbit's model

### Option B: Orbit as xDS control plane

**Description:** Orbit's own control plane implements the Envoy xDS APIs. When a service is registered with Orbit, Orbit generates and pushes Envoy configuration to the affected sidecar instances. Sidecar injection is handled by a custom Kubernetes admission webhook.

**Pros:**
- Tight integration between service registration in Orbit and Envoy configuration — no translation layer
- Orbit controls every aspect of Envoy config — no Istio defaults to work around
- Long-term, enables Orbit to drive enforcement config directly from its own service registry

**Cons:**
- Requires building and operating a production-grade xDS server before the core platform is complete — significant engineering investment at the wrong time
- Custom admission webhook for sidecar injection must be built, tested, and maintained
- Operational maturity will lag behind Istio significantly in the near term
- High risk — xDS is complex; implementation bugs could silently misconfigure enforcement

### Option C: Cilium Service Mesh (sidecar-free)

**Description:** Use Cilium's service mesh with its shared per-node Envoy proxy model.

**Rejected** — see Context section. Cilium's per-node Envoy is structurally incompatible with the per-pod identity model required by ADR-0010. A shared per-node Envoy proxy cannot hold per-pod SVID context or run per-pod ext_proc filters (ADR-0021). This model cannot deliver agent workload identity transparency without fundamental changes to the per-pod architecture.

---

## Decision

**Chosen Option:** Option A — Istio as service mesh control plane, with Cilium as CNI for network-level enforcement.

### Rationale

**Istio** is selected as the sidecar control plane. The two-sidecar per-pod model in ADR-0010 is Istio's native deployment pattern. istiod handles sidecar injection and xDS config distribution, which are the exact two problems the open question in ADR-0011 was asking Orbit to solve. Istio's SPIFFE/SPIRE integration is production-proven (the SPIRE Controller Manager project exists specifically for this), and `ext_authz` + OPA on Envoy under Istio is a standard production deployment. The operational investment is real but it is a known quantity — the alternative of building Orbit's own xDS control plane before the core platform exists is the wrong sequencing. That investment can be made later if Orbit's service registration model demands tighter Envoy config coupling.

Option B is explicitly deferred, not rejected. If Orbit's service registry model eventually requires driving Envoy config from first-party sources rather than Istio's config model, the migration path is available. Today is not that time.

**Cilium** is adopted as the CNI plugin. It operates at a completely different layer from Istio — eBPF-level kernel networking rather than sidecar-level L7 proxy management. Cilium and Istio are complementary and are a well-established production combination. Cilium's role is specifically to harden the network-level enforcement of the "no direct pod-to-pod traffic that bypasses the sidecar" constraint from ADR-0011. Standard Kubernetes NetworkPolicy is iptables-based with known bypass vectors; Cilium's eBPF enforcement operates at the kernel level and is significantly harder to circumvent.

---

## Infrastructure Stack

```
┌─────────────────────────────────────────────────────────┐
│  Istio (istiod)                                         │
│  Sidecar injection · xDS config distribution            │
│  SPIRE Controller Manager integration                   │
├─────────────────────────────────────────────────────────┤
│  Envoy sidecars / istio-proxy (per-pod)                 │
│  mTLS · ext_authz → OPA · platform token injection      │
│  SVID read from SPIRE CSI socket (SPIFFE_ENDPOINT_SOCKET)│
├─────────────────────────────────────────────────────────┤
│  SPIRE CSI Driver (per-node DaemonSet)                  │
│  Mounts SPIRE Workload API socket into pods as volume   │
│  No per-pod sidecar — node-level socket delivery only   │
├─────────────────────────────────────────────────────────┤
│  SPIRE Agent (per-node DaemonSet)                       │
│  Workload attestation · SVID issuance                   │
├─────────────────────────────────────────────────────────┤
│  Cilium (CNI · eBPF)                                    │
│  NetworkPolicy enforcement · DNS egress control         │
│  SPIFFE-identity-aware network policy                   │
│  Blocks direct pod-to-pod traffic bypassing sidecars    │
└─────────────────────────────────────────────────────────┘
```

### Responsibility boundaries

| Component | Responsibility |
|---|---|
| **Istio / istiod** | Sidecar injection, xDS config distribution to Envoy sidecars and edge gateway |
| **Envoy sidecar** | mTLS termination, `ext_authz` policy evaluation, platform token injection, outbound request enrichment |
| **SPIRE CSI Driver (DaemonSet)** | Mounts SPIRE Workload API socket into pods as a CSI volume (`spire-workload-socket` at `/run/secrets/workload-spiffe-uds/socket`); no per-pod sidecar container |
| **SPIRE Agent (DaemonSet)** | Node attestation, workload attestation, SVID issuance |
| **SPIRE Server** | Trust domain CA, registration entry management |
| **Cilium** | eBPF-level NetworkPolicy, DNS egress control, SPIFFE-aware network-level policy |

### Cilium's specific enforcement role

Cilium enforces three constraints that harden the ADR-0011 sidecar model:

1. **Intra-pod direct traffic blocked**: `CiliumNetworkPolicy` rules deny pod-to-pod traffic that does not flow through the Envoy sidecar, closing bypass routes that standard NetworkPolicy cannot reliably block
2. **External egress via gateway only**: DNS-aware egress policy blocks direct outbound calls from agent pods to external services (graph.microsoft.com, salesforce.com, etc.) — all external traffic must route through the Credential-Aware Outbound Gateway (ADR-0012), enforced at the packet level
3. **SPIFFE identity-based network policy**: Cilium can reference SPIFFE IDs in network policy rules — a backstop layer underneath Istio's mTLS enforcement

---

## Consequences

### Positive

- Agent workloads remain fully identity-unaware: sidecar injection is automatic via Istio's admission webhook, Envoy config is distributed by istiod, and the SPIRE CSI driver delivers the Workload API socket to each pod with zero agent workload configuration
- The operational complexity of xDS config distribution is handled by a mature, production-proven system rather than Orbit building it from scratch
- Cilium adds a kernel-level enforcement backstop that makes the "no bypass" constraint from ADR-0011 genuinely hard rather than conventionally enforced
- DNS egress control in Cilium structurally prevents agents from calling external services directly, reinforcing the Credential-Aware Outbound Gateway model in ADR-0012
- SPIFFE/SPIRE + Istio + Cilium is a well-documented, production-proven combination — operational knowledge and community support exist

### Negative

- Istio adds cluster complexity and a new critical infrastructure dependency (istiod must be highly available)
- The platform team must develop Istio operational expertise — this is a non-trivial learning investment
- Some Istio defaults conflict with Orbit's model and require explicit configuration (e.g. Istio's own mTLS mode must be configured to not conflict with the SPIRE-issued SVID mTLS model)
- Cilium as CNI must be provisioned at cluster creation time — it cannot be added to an existing cluster with a different CNI without re-provisioning

### Neutral

- The Orbit-native xDS control plane option (Path 2 from ADR-0011) is deferred, not closed — if Orbit's service registration model later requires driving Envoy config from first-party sources, the migration is possible. This should be revisited as a day-two decision
- The SPIRE Controller Manager (`github.com/spiffe/spire-controller-manager`) is the recommended integration point between SPIRE Server and Istio — it automates the creation of SPIRE registration entries when Kubernetes workloads are created, maintaining the tight coupling between Orbit's agent registry and SPIRE defined in ADR-0010

---

## Implementation Notes

### Istio Configuration Alignment

When configuring Istio for the Orbit platform, the following must be set explicitly:

- **mTLS mode**: configured to use SPIRE-issued SVIDs rather than Istio's own CA (Citadel). Istio supports pluggable CA backends; SPIRE is integrated via the SPIRE Controller Manager which provisions SVIDs to Istio workloads
- **Sidecar injection**: enabled via namespace labels for all Orbit service namespaces; enforced at cluster level via admission webhook
- **ext_authz filter**: configured globally for all sidecars, pointing at the co-located OPA instance for policy evaluation
- **Egress control**: Istio's egress gateway is not used — outbound routing to external services is handled by the Credential-Aware Outbound Gateway (ADR-0012), enforced at the Cilium level

### Cilium Configuration Alignment

- Deployed as the cluster CNI at provisioning time
- `CiliumNetworkPolicy` resources are generated by Orbit's control plane alongside SPIRE registration entries and gateway routing rules when a service is registered
- DNS-aware egress policy configured to allow outbound traffic only to the Credential-Aware Outbound Gateway cluster IP, blocking direct external calls from agent pods
- SPIFFE-identity-aware policy configured per tenant namespace

### SPIRE Controller Manager

The SPIRE Controller Manager automates the synchronisation between Kubernetes workloads and SPIRE registration entries. When Orbit registers a new agent and the corresponding Kubernetes workload is created, the Controller Manager creates the SPIRE registration entry automatically. This removes the manual two-phase write described in ADR-0010 and keeps the registries in sync without Orbit needing to call the SPIRE Server API directly for every registration event. Orbit's agent registration flow should be updated to leverage this.

---

## Validation

### Success Metrics

- All service pods receive correctly configured Envoy sidecars via Istio injection with zero manual configuration by service or agent authors
- SVID-to-platform-token exchange operates correctly: `istio-proxy` reads the CSI-mounted Workload API socket, identity-proxy (ADR-0021) performs the exchange, and platform tokens are injected transparently on outbound requests
- Direct pod-to-pod traffic bypassing sidecars is blocked at the Cilium level — verified by network policy audit and penetration testing
- Direct external egress from agent pods is blocked by Cilium DNS egress policy — all external calls route through the Credential-Aware Outbound Gateway

### Monitoring

- istiod health and xDS sync latency to sidecar fleet
- Cilium NetworkPolicy drop rate (blocked bypass attempts)
- Cilium DNS egress policy violations (attempted direct external calls from agent pods)
- SPIRE Controller Manager sync events and registration entry creation rate

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — Defines the two-sidecar per-pod model that the Istio control plane manages
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Defines the universal enforcement model that this ADR operationalises; the open sidecar control plane question in that ADR is resolved here
- [ADR-0012](0012-credential-aware-outbound-gateway.md) — The Credential-Aware Outbound Gateway that Cilium DNS egress policy enforces as the sole external egress point for agent pods

---

## References

- [Istio Documentation](https://istio.io/latest/docs/)
- [SPIRE Controller Manager](https://github.com/spiffe/spire-controller-manager)
- [Istio + SPIRE Integration Guide](https://istio.io/latest/docs/ops/integrations/spire/)
- [Cilium Documentation](https://docs.cilium.io)
- [Cilium NetworkPolicy](https://docs.cilium.io/en/stable/security/policy/)
- [Cilium + Istio](https://docs.cilium.io/en/stable/network/servicemesh/istio/)
- [OPA Envoy Plugin](https://www.openpolicyagent.org/docs/latest/envoy-introduction/)

---

## Notes

The combination of Istio (sidecar control plane) and Cilium (CNI/network enforcement) gives Orbit a defence-in-depth posture at the infrastructure layer: Istio enforces policy at the L7 proxy level, Cilium enforces it at the eBPF kernel level. A misconfigured or missing sidecar cannot yield a bypass route because Cilium blocks the underlying network path. This is the correct security model for a platform where enforcement must be universal and unconditional.

**Last Updated:** 2026-03-03

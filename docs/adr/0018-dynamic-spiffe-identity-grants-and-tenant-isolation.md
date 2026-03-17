# 0018. Dynamic SPIFFE Identity Grants and Tenant Isolation

**Status:** Proposed
**Date:** 2026-03-17
**Deciders:** Engineering Team
**Technical Story:** PR #10 - `chore(k3d): rework k3d setup`

---

## Context

ADR-0010 defined Orbit's workload identity model and established three SPIFFE path shapes:

```text
spiffe://orbit.{instance-id}/system/{component-name}
spiffe://orbit.{instance-id}/tenant/{tenant-id}/service/{service-name}
spiffe://orbit.{instance-id}/tenant/{tenant-id}/agent/{agent-id}
```

The original infrastructure model granted platform service identities through explicit, per-service manifests. Each service that needed an SVID carried a dedicated `ClusterSPIFFEID` manifest under the infrastructure tree. That made the grant registry easy to read in Git, but it also created friction: every new service required boilerplate infrastructure edits even when it fit an already-understood naming pattern.

PR #10 introduces a different approach in `infrastructure/base/spiffe.yaml`: shared `ClusterSPIFFEID` resources mint identities dynamically from pod metadata. Today that means:

- System services use `spiffe.io/component` to produce `/system/{component-name}`
- Tenant services use `spiffe.io/tenant` and `spiffe.io/service` to produce `/tenant/{tenant-id}/service/{service-name}`
- Tenant agents use `spiffe.io/tenant` and `spiffe.io/agent` to produce `/tenant/{tenant-id}/agent/{agent-id}`

This is the correct direction for operational scale, but it turns the workload metadata schema into a foundational identity boundary. The platform needs an explicit architectural decision for:

- How dynamic grants replace the old per-service registry model
- Which labels and namespace attributes are allowed to shape SPIFFE IDs
- Whether tenant identity is derived from workload labels or infrastructure-controlled namespace boundaries
- What controls prevent a workload from claiming an unauthorised system or tenant identity

### Background

Orbit treats workload identity as part of the platform nervous system. Identity grants are not a convenience feature; they are the substrate used by policy enforcement, credential exchange, and auditability. A change in how SPIFFE IDs are minted is therefore architectural, not just implementation detail.

Kubernetes labels are easy to apply and are a good fit for component and workload naming. They are not, by themselves, a strong multi-tenant isolation boundary in a shared cluster. Namespace placement, by contrast, is infrastructure-controlled and already participates in scheduling, RBAC, network policy, and admission control. That makes namespace the safer primitive for tenant identity.

This label model also aligns well with Cilium's identity and policy model from ADR-0015. Cilium identifies workloads from Kubernetes metadata and applies network policy using label selectors. Reusing the same reserved Orbit labels for SPIFFE minting and for Cilium policy matching lets the platform describe workload identity, authorisation intent, and network reachability with one coherent metadata vocabulary instead of parallel schemes.

### Problem Statement

Orbit needs a dynamic SPIFFE grant model that preserves the naming structure from ADR-0010 while making an explicit decision about tenant isolation and the enforcement controls required around metadata-derived identities.

### Goals

- Replace per-service `ClusterSPIFFEID` manifests with a reusable dynamic grant model
- Preserve the three SPIFFE path shapes defined in ADR-0010
- Make tenant identity derivation safe for shared-cluster multi-tenancy
- Define the label schema and admission boundaries required to prevent identity spoofing

### Non-Goals

- Implementing the full tenant workload runtime in this ADR
- Selecting a specific admission controller product or policy engine implementation
- Replacing SPIRE Controller Manager or changing the trust-domain decision from ADR-0010

---

## Decision Drivers

* Security of the workload identity boundary
* Operational scalability as new services and agents are added
* Alignment with ADR-0010's SPIFFE naming model
* Consistency with ADR-0015's Cilium-based workload identification and policy enforcement
* Clear Git- and policy-level governance over who may mint which identities
* A migration path from today's local-dev infrastructure to future shared-cluster tenancy

---

## Considered Options

### Option 1: Keep explicit per-service `ClusterSPIFFEID` manifests

**Description:** Continue defining one `ClusterSPIFFEID` per service or workload class, with each SPIFFE grant represented as a dedicated manifest in Git.

**Pros:**
- Very explicit registry of granted identities in the infrastructure tree
- Strong review visibility for each newly granted identity

**Cons:**
- High operational friction for every new service
- Repeats the same template logic across many manifests
- Does not scale well once tenant services and agents become dynamic platform objects

### Option 2: Use labels for all dynamic path segments, including tenant ID

**Description:** Use shared `ClusterSPIFFEID` resources and derive all SPIFFE path segments directly from pod labels such as `spiffe.io/component`, `spiffe.io/tenant`, `spiffe.io/service`, and `spiffe.io/agent`.

**Pros:**
- Very flexible and easy to automate
- Minimal infrastructure ceremony when new workloads are introduced

**Cons:**
- Tenant identity becomes workload-declared rather than infrastructure-bound
- In a shared cluster, any workload able to set `spiffe.io/tenant=<tenant-id>` can attempt to claim another tenant's identity
- Requires stronger compensating controls to avoid a confused-deputy or spoofing class of failure

### Option 3: Use dynamic `ClusterSPIFFEID` grants, but derive tenant ID from namespace

**Description:** Use shared `ClusterSPIFFEID` resources, keep labels for workload-specific names, and derive tenant identity from the Kubernetes namespace boundary. System identities remain label-driven inside the reserved `orbit-system` namespace. Tenant identities use one namespace per tenant, with namespace name mapped to `{tenant-id}` and labels used only for `{service-name}` or `{agent-id}`.

**Pros:**
- Preserves the operational benefits of dynamic grants
- Uses an infrastructure-controlled boundary for tenant isolation
- Aligns identity with other Kubernetes isolation primitives such as RBAC and NetworkPolicy
- Reduces the chance that a workload can self-assert another tenant's identity

**Cons:**
- Requires a tenant namespace strategy before tenant workloads are introduced
- Slightly reduces flexibility compared with pure label-derived identities
- Requires migration away from any interim `spiffe.io/tenant` label-based templates before shared-cluster tenancy is supported

---

## Decision

Orbit will adopt **dynamic `ClusterSPIFFEID` grants** as the default workload identity model, but **tenant identity must be derived from namespace, not from a pod label**, for any environment that supports shared-cluster multi-tenancy.

**Chosen Option:** Option 3 - Dynamic `ClusterSPIFFEID` grants with namespace-derived tenant isolation

### Rationale

The per-service manifest model is too static for Orbit's target shape. Services, agents, and future tool-facing workloads must be able to receive identities through reusable templates rather than one manifest per principal class.

However, the fully label-driven model is too weak at the tenant boundary. Labels are appropriate for naming a component, service, or agent within an already-authorised scope. They are not sufficient to establish tenancy on a shared cluster. Namespace is the correct boundary because it is created and governed by platform infrastructure, can be bound to RBAC and policy, and is substantially harder for an arbitrary workload author to spoof.

This splits the model cleanly:

- `/system/{component-name}` remains label-driven, but only inside the reserved `orbit-system` namespace
- `/tenant/{tenant-id}/service/{service-name}` derives `{tenant-id}` from namespace and `{service-name}` from `spiffe.io/service`
- `/tenant/{tenant-id}/agent/{agent-id}` derives `{tenant-id}` from namespace and `{agent-id}` from `spiffe.io/agent`

The current `spiffe.io/tenant` label pattern may remain as a temporary local-development convenience while there are no shared-cluster tenant workloads, but it is not the accepted long-term tenancy model and must not be relied on for production isolation.

### Identity Grant Schema

The accepted metadata model is:

```text
System services
  Namespace: orbit-system
  Label:     spiffe.io/component={component-name}
  SPIFFE ID: spiffe://orbit.{instance-id}/system/{component-name}

Tenant services
  Namespace: {tenant-id}
  Label:     spiffe.io/service={service-name}
  SPIFFE ID: spiffe://orbit.{instance-id}/tenant/{namespace}/service/{service-name}

Tenant agents
  Namespace: {tenant-id}
  Label:     spiffe.io/agent={agent-id}
  SPIFFE ID: spiffe://orbit.{instance-id}/tenant/{namespace}/agent/{agent-id}
```

`spiffe.io/tenant` is not part of the accepted steady-state schema for shared-cluster tenancy.

### Cilium Alignment

The same metadata schema is also the canonical policy vocabulary for Cilium-backed network enforcement:

- `spiffe.io/component` identifies platform workloads inside `orbit-system`
- `spiffe.io/service` identifies tenant-scoped service workloads inside a tenant namespace
- `spiffe.io/agent` identifies tenant-scoped agent workloads inside a tenant namespace
- Namespace provides tenant scope, which Cilium can also use as part of policy selection

This creates a consistent control model across layers:

- SPIRE uses the reserved labels and namespace context to mint the correct SPIFFE ID
- Orbit authn/authz maps the resulting SPIFFE ID back to the same component, service, or agent identity used by the platform policy model
- Cilium policy selects workloads using the same namespace and reserved labels, so network policy is attached to the same principal shape used by identity and application-layer authorisation

The practical outcome is that Orbit does not need one naming scheme for SPIFFE, a second scheme for service policy, and a third scheme for network policy. A single, reserved label contract becomes the shared identity vocabulary for workload attestation, authorisation, and network isolation.

### Threat Model

The primary threat considered here is **identity spoofing by metadata manipulation**:

- A workload claims a privileged system identity by setting `spiffe.io/component=policy-engine`
- A workload claims another tenant's identity by setting `spiffe.io/tenant=acme`
- A workload in one tenant namespace claims an agent or service name that should not exist in that scope

The decision is to trust labels only after the workload has already been admitted into an authorised namespace and has passed namespace- and label-level policy checks. Namespace establishes tenant scope. Labels select the identity name within that scope.

---

## Consequences

### Positive

- New system services, tenant services, and tenant agents can be granted identities through reusable templates rather than one-off manifests
- Tenant identity is anchored to a stronger infrastructure boundary than a mutable workload label
- The model stays aligned with ADR-0010 while remaining operable at scale
- The same reserved labels can be consumed by SPIRE, Orbit policy, and Cilium, reducing drift between identity and network enforcement

### Negative

- Tenant workloads will require namespace lifecycle management as part of the platform design
- The current local-dev templates in `infrastructure/base/spiffe.yaml` will need follow-up changes before shared-cluster tenant workloads are introduced
- Admission policy becomes mandatory infrastructure, not optional hardening

### Neutral

- Git is no longer the sole registry of every individual SPIFFE grant; Git now governs the grant templates and policy boundaries instead
- The review focus shifts from "was a new manifest added?" to "can this workload legally use the reserved namespace and labels?"

---

## Implementation

The implementation model is:

1. Keep reusable `ClusterSPIFFEID` resources for each identity class rather than per-service manifests
2. Reserve `orbit-system` for platform-owned system workloads
3. Introduce one namespace per tenant before tenant service or agent workloads are supported in shared clusters
4. Update tenant `ClusterSPIFFEID` templates to read tenant scope from namespace and workload name from reserved labels
5. Enforce the reserved label schema and namespace ownership through admission control
6. Reuse the same reserved labels in `CiliumNetworkPolicy` generation so network policy is bound to the same workload identity contract

### Migration Path

- Short term: keep the dynamic grant model introduced in PR #10
- Before tenant workloads are supported beyond local experimentation: change tenant templates away from `spiffe.io/tenant` and onto namespace-derived tenant scope
- Add admission rules that reject reserved SPIFFE labels outside approved namespaces or without the required owning controller/service account
- Standardise policy generation so Cilium selectors use the same reserved labels that drive SPIFFE ID minting
- Document namespace naming conventions and tenant namespace provisioning in the runtime/platform ADRs that introduce tenant execution

### Timeline

- This ADR is required before PR #10 merges
- Template and admission-control follow-up should happen before ADR-0010 tenant service or tenant agent path shapes are exercised in a shared cluster

---

## Validation

The decision is correct if Orbit can grant identities dynamically without allowing workloads to mint identities outside their authorised scope.

### Success Metrics

- New platform services can receive `/system/{component}` identities without adding per-service SPIFFE manifests
- Tenant workloads can only receive identities whose tenant segment matches their namespace
- Admission policy rejects unauthorised use of reserved `spiffe.io/*` labels or reserved namespaces
- Cilium policies can be authored from the same reserved labels used for SPIFFE identity, without requiring a second workload-classification scheme

### Monitoring

- Audit rejected admissions for reserved SPIFFE labels and reserved namespaces
- Inspect generated SPIRE registration entries and issued SPIFFE IDs for namespace-to-tenant consistency
- Monitor for duplicate or conflicting identity names within a namespace
- Audit Cilium policy matches and drops against the reserved label set to detect mismatches between network policy and SPIFFE identity intent

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) - SPIFFE trust domain and path structure
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) - Policy enforcement architecture that consumes workload identity
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) - Cilium as the network enforcement layer
- [ADR-0016](0016-local-development-environment.md) - Local SPIRE and cluster environment

---

## References

- [PR #10 comment requiring this ADR](https://github.com/clear-route/orbit/pull/10#issuecomment-4065238271)
- [SPIFFE/SPIRE Workload Attestation ADR](0010-spiffe-spire-workload-attestation.md)

---

## Notes

This ADR intentionally separates two questions that are easy to conflate:

- Dynamic grants versus per-service manifests
- Label-derived naming versus infrastructure-derived tenant isolation

Orbit accepts dynamic grants. Orbit does not accept label-derived tenant scope as the long-term isolation boundary for shared clusters.

**Last Updated:** 2026-03-17

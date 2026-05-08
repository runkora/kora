# 51. Agent Execution Infrastructure

**Status:** Proposed  
**Date:** 2026-05-08  
**Deciders:** Platform Engineering  
**Technical Story:** Managed agent workloads must run in tenant namespaces with stable identity, governed LLM access, and efficient resource utilisation. This ADR decides how agent pods are deployed, scaled, and addressed.

---

## Context

### Background

Each managed agent type needs a running workload in the tenant namespace. That workload must: hold a stable SPIFFE identity, receive a platform JWT for outbound LLM calls, support multiple concurrent sessions, and scale based on resource demand. The existing identity-proxy and Envoy sidecars (ADR-0021, ADR-0010) are available in any pod that is SPIRE-attested and Istio-injected.

### Problem Statement

The choice of Kubernetes workload type and pod composition directly determines: routing complexity, cold start latency, resource efficiency, session isolation, and SPIRE entry management overhead. The wrong choice makes the system fragile or expensive.

### Goals

- One workload per agent type per tenant namespace
- Pods receive a stable SPIFFE identity without per-pod SPIRE entry registration
- Multiple concurrent sessions per pod — resource-driven scaling, not session-count driven
- Session-level workspace isolation within each pod
- `orbit-agents` can route to a specific pod without querying pod IPs

### Non-Goals

- Session-manager implementation details (ADR-0049)
- Run and session lifecycle state machine (ADR-0053)
- SPIRE entry registration process (ADR-0057)

---

## Decision Drivers

* Per-pod overhead is high: Envoy sidecar + identity-proxy sidecar + SPIRE SVID acquisition. Spinning up this stack per session is prohibitively expensive.
* Session routing must target a specific pod (session state — subprocess, worktree — is local to the pod). This requires stable, predictable pod addresses.
* Standard k8s primitives are preferred over custom controllers.
* HPA on resource metrics is simpler and more maintainable than custom session-count-based scaling.

---

## Considered Options

### Option A: Deployment + HPA

**Description:** One Deployment per agent type. Pods get random names and ephemeral IPs. HPA scales on CPU/memory. Pod IPs tracked by `orbit-agents`.

**Pros:**
- Simplest k8s primitive
- HPA is standard and well-understood

**Cons:**
- Pods have no stable addresses — routing requires tracking ephemeral IPs, which change on reschedule
- Pod rescheduling silently breaks session routing without a recovery mechanism
- Not semantically matched to long-lived, identity-stable workloads

### Option B: StatefulSet + HPA

**Description:** One StatefulSet per agent type. Pods get stable ordinal names and stable DNS: `{agent}-{n}.{agent}.{namespace}.svc.cluster.local`. HPA scales on CPU/memory. `orbit-agents` routes by ordinal.

**Pros:**
- Stable pod DNS names — routing by ordinal, no IP tracking
- Pod rescheduling preserves the same DNS name — routing survives pod restarts
- StatefulSet semantics match the workload: long-lived, identity-stable, ordered scale-down
- SPIRE entry keyed to pod selector (not pod name) — no per-pod entry registration
- Scale-down is ordered — highest ordinals terminated first, controllable with PodDisruptionBudget

**Cons:**
- StatefulSet is slightly more complex than Deployment
- Requires a headless Service for stable DNS

### Option C: Pod-per-session (on-demand)

**Description:** Spin up a new pod for each session; tear it down when the session ends.

**Pros:**
- Perfect session isolation — fresh pod per session

**Cons:**
- Three-container pod startup (session-manager + identity-proxy + Envoy) is too slow for session start latency targets
- SPIRE entry must be registered per pod — significant orchestration overhead
- Cold start (image pull, scheduler, SPIRE attestation, SVID acquisition, token exchange) is incompatible with 5-second time-to-first-token target

---

## Decision

**Chosen Option: B — StatefulSet + HPA**

### Pod Composition

Each pod in the StatefulSet contains three containers:

| Container | Role |
|---|---|
| `session-manager` | Orbit-built main container; manages concurrent sessions, subprocesses, worktrees |
| `identity-proxy` | Existing Orbit sidecar; SVID → platform JWT; injects JWT on outbound LLM calls via ext_proc |
| `envoy` | Istio-injected; mTLS for all pod-to-pod traffic; runs ext_proc for outbound token injection |

### Naming and Routing

StatefulSet name per agent type: `{agent-type-id}` (slugified)  
Headless Service name: `{agent-type-id}`  
Namespace: `orbit-tenant-{tenant-id}`  
Pod DNS pattern: `{agent-type-id}-{n}.{agent-type-id}.orbit-tenant-{tenant-id}.svc.cluster.local`

`orbit-agents` stores `run_id → pod_ordinal` and resolves to the stable DNS name at route time. No pod IP tracking required.

### Scaling

HPA targets CPU utilisation at 70% and memory utilisation at 80%. When all pods are fully loaded with concurrent sessions, resource pressure triggers scale-up. Scale-down is ordered (StatefulSet terminates highest ordinals first) and protected by a PodDisruptionBudget.

The PDB uses `minAvailable: 1` as a floor, but the primary protection against evicting pods with active runs is a different mechanism: the session-manager patches the pod with a label `orbit.io/active-runs={count}` whenever the count changes. The StatefulSet's scale-down controller will skip a pod if its own pod disruption policy permits — but Kubernetes PDB does not directly inspect custom labels. Instead, `orbit-agents`' drift detection loop monitors for `Eviction` events on pods with active runs and preemptively cordons those pods (adds a taint `orbit.io/do-not-evict=true:NoSchedule`) until all runs on that pod reach `completed` or `failed`. This is the enforcement mechanism — the PDB provides a coarse backstop only.

### SPIRE Identity

All pods in the StatefulSet carry the following labels on the pod template:
- `spiffe.io/agent={agent-type-id}` — triggers the `orbit-tenant-agent` ClusterSPIFFEID to issue the SVID
- `orbit.io/managed-agent: "true"` — distinguishes managed agent pods from BYOC pods in the discovery loop (ADR-0056) The existing `orbit-tenant-agent` ClusterSPIFFEID (ADR-0010, ADR-0018) automatically issues SVIDs to matching pods:

```
spiffe://{trust-domain}/tenant/orbit-tenant-{tenant-id}/agent/{agent-type-id}
```

No SPIRE entries are registered programmatically — identity is entirely label-driven. All pods in the StatefulSet share the same SPIFFE URI. The identity-proxy sidecar handles SVID acquisition and platform JWT exchange — no per-pod configuration required.

The namespace must carry `orbit.io/role: tenant` (already true for all tenant namespaces) for the ClusterSPIFFEID namespace selector to match.

### Cross-Namespace Routing

`orbit-agents` runs in `orbit-system`. Agent pods run in `orbit-tenant-{id}`. Istio mTLS is enforced on all cross-namespace pod-to-pod calls. An Istio `AuthorizationPolicy` in each tenant namespace explicitly permits inbound connections from `orbit-agents`' service account.

### Rationale

StatefulSet is the only standard k8s primitive that provides stable pod DNS without custom controllers. The stable DNS name is the key enabler for session routing — `orbit-agents` routes by ordinal without tracking pod IPs or handling rescheduling events. HPA on resource metrics is simpler than custom session-count metrics and correctly reflects actual pod load. PodDisruptionBudget prevents the scale-down race condition where an active session's pod is evicted.

---

## Consequences

### Positive

- Stable routing: pod DNS names survive restarts; no IP tracking in `orbit-agents`
- SPIRE entry is one-per-agent-type: simple lifecycle, no per-pod entry management
- HPA handles scaling transparently: no custom pool controller required
- Sidecar cost amortised: identity-proxy and Envoy startup cost is paid once per pod, not per session

### Negative

- StatefulSet scale-down is ordered and slower than Deployment scale-down — acceptable given workload semantics
- Headless Service required alongside StatefulSet — minor additional resource

### Neutral

- Each agent type has a StatefulSet + headless Service + HPA + PodDisruptionBudget — four k8s resources per agent type

---

## Implementation

### Resources Per Agent Type

On agent type creation, `orbit-agents` provisions:

1. `StatefulSet/{agent-type-id}` in `orbit-tenant-{id}` — 1 replica minimum
2. `Service/{agent-type-id}` (headless, `clusterIP: None`) in `orbit-tenant-{id}`
3. `HorizontalPodAutoscaler/{agent-type-id}` targeting the StatefulSet
4. `PodDisruptionBudget/{agent-type-id}` — `minAvailable: 1` (adjusted for active runs)
5. `ServiceAccount/{agent-type-id}` in `orbit-tenant-{id}` — used for SPIRE attestation
6. `AuthorizationPolicy` allowing inbound from `orbit-agents` service account

On agent type deletion, all resources are removed after active runs drain.

---

## Validation

### Success Metrics

- StatefulSet pod reaches Running state within 60 seconds of agent type creation
- Pod DNS name resolves correctly from `orbit-system` namespace
- SVID delivered to pod within 30 seconds of Running state
- HPA scales up within 2 minutes of sustained CPU/memory pressure

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE
- [ADR-0021](0021-envoy-ext-proc-sidecar-token-injection.md) — identity-proxy sidecar
- [ADR-0049](0049-agent-cli-framework-and-transform-layer.md) — session-manager (main container)
- [ADR-0052](0052-orbit-agents-service-architecture.md) — service that provisions these resources
- [ADR-0057](0057-agent-identity-and-policy-provisioning.md) — SPIRE entry and grant lifecycle

---

**Last Updated:** 2026-05-08

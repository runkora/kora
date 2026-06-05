# BYOC Agent Discovery & Observability

**Status:** Accepted
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** Tenant-deployed (BYOC) agents already receive platform identity and LLM access via existing SPIFFE/SPIRE and inference-gateway mechanisms. The platform should surface them in the agent portal without requiring tenant teams to register them manually.

---

## Context

### Background

BYOC agents are containers built and deployed by tenant teams in their own tenant namespaces. They receive SPIFFE SVIDs automatically ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md)), exchange them for platform JWTs via the identity-proxy sidecar ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)), and make LLM calls through the inference-gateway which attributes usage to their principal ([inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md)). All of this already works. The missing piece is surfacing these agents in the Orbit agent portal.

### Problem Statement

BYOC agents are invisible to the platform control plane beyond their usage records in the inference-gateway. There is no unified view of which agents are running, in which namespace, with what health status. Operators cannot see the full picture of agent activity across managed and BYOC workloads without checking k8s directly.

### Goals

- Auto-discover BYOC agents running in tenant namespaces without manual registration
- Surface BYOC agents in the agent portal alongside managed agents
- Show usage data for BYOC agents derived from existing inference-gateway attribution
- Avoid any changes to BYOC agent containers or deployment manifests

### Non-Goals

- Chat interaction with BYOC agents from the Orbit UI (BYOC is tenant-managed)
- Lifecycle management of BYOC agents (start, stop, restart) from the platform
- Detailed run observability for BYOC (no event log — BYOC agents don't use the session-manager)

---

## Decision Drivers

* BYOC agents must not need to register themselves — zero changes to tenant deployment manifests
* The SPIFFE identity is already present on qualifying pods — it is the right signal for "this is an Orbit agent"
* Usage is already attributed in inference-gateway logs — no additional instrumentation needed
* The portal must show both managed and BYOC agents in a single unified view

---

## Considered Options

### Option A: Manual BYOC registration via API

**Description:** Tenants register their BYOC agents by calling an `orbit-agents` API endpoint with metadata (name, description, namespace, pod selector).

**Pros:**
- Explicit — platform knows exactly what is registered
**Cons:**
- Requires tenant teams to change their deployment process
- Registration drifts from reality if pods are deleted and recreated
- Defeats the goal of zero-change discovery

### Option B: k8s pod watch + SPIFFE identity as the discovery signal

**Description:** `orbit-agents` watches pod events across all `orbit-tenant-*` namespaces. Any pod that carries the SPIFFE workload annotation (set by the SPIRE CSI driver on attestation) is treated as a BYOC agent and surfaced in the portal. No registration required.

**Pros:**
- Zero changes to BYOC deployments — automatic
- Real-time: pod appears in portal within seconds of reaching Running state
- SPIFFE identity is the correct signal — only pods with platform identity are surfaced
- Pod deletion removes the agent from the portal automatically

**Cons:**
- May surface pods that are not "agents" in the user's mental model (e.g. internal service pods in tenant namespaces)
- Requires `orbit-agents` to hold k8s watch permissions across all tenant namespaces

### Option C: Opt-in label convention

**Description:** BYOC agents add a label `orbit.io/byoc-agent: "true"` to their pod spec. `orbit-agents` watches for this label.

**Pros:**
- Explicit opt-in — only pods that declare themselves agents are surfaced
- Reduces noise from non-agent pods in tenant namespaces
**Cons:**
- Requires a change to tenant deployment manifests (one label, but still a change)
- Tenants may forget the label and wonder why their agent is not in the portal

---

## Decision

**Chosen Option: B — k8s pod watch + SPIFFE identity label as discovery signal, with opt-in label for display name**

A pod in a tenant namespace is treated as a BYOC agent if it carries the `spiffe.io/agent` label — the same label that causes the `orbit-tenant-agent` ClusterSPIFFEID to issue a SPIFFE identity ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md), [dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md)). This is the correct signal: any pod that has opted into platform identity is surfaced in the portal. Pods with `spiffe.io/service` labels are tenant services, not agents, and are excluded.

To reduce noise, a label convention is used for display metadata but is not required for discovery:

```
orbit.io/agent-name: "My Coding Agent"       # optional — display name in portal
orbit.io/agent-description: "..."            # optional — description
```

Without these labels, the portal shows the pod name and namespace as the agent name. The BYOC agent's SPIFFE ID is derived from the ClusterSPIFFEID template:

```
spiffe://{trust-domain}/tenant/{namespace}/agent/{spiffe.io/agent label value}
```

### Discovery Mechanism

`orbit-agents` maintains a pod informer (k8s SharedInformer) on all `orbit-tenant-*` namespaces filtered to pods with `spiffe.io/agent` label present. On pod add/update/delete events:

- **Pod reaches Running + has `spiffe.io/agent` label**: upsert BYOC agent record in `byoc_agents` table
- **Pod deleted or terminal**: mark BYOC agent record as offline

Managed agent pods also carry `spiffe.io/agent` — they are excluded from `byoc_agents` by the label `orbit.io/managed-agent: "true"` set on their pod template by orbit-agents at StatefulSet creation time. The BYOC discovery loop skips any pod carrying this label.

### BYOC Agent Data Model

```
byoc_agents
├── id                  uuid, primary key
├── tenant_id           text, non-nullable
├── pod_name            text
├── namespace           text
├── spiffe_id           text — from SPIFFE annotation
├── display_name        text — from orbit.io/agent-name label or pod name
├── description         text — from orbit.io/agent-description label
├── status              text — enum: online | offline
├── last_seen_at        timestamptz
├── created_at          timestamptz
└── updated_at          timestamptz
```

### Usage Data

BYOC agent usage is derived from existing inference-gateway `inference_usage` records. The `principal_id` in usage records matches the SPIFFE URI of the pod (extracted from the platform JWT). `orbit-agents` joins `byoc_agents.spiffe_id` with `inference_usage.principal_id` to surface usage stats in the portal.

No additional instrumentation is required in the BYOC agent or inference-gateway.

### Portal Surfacing

BYOC agents appear in the same agent portal view as managed agents. The portal distinguishes them with:
- A "BYOC" badge
- No chat, start, stop, or workspace controls
- Usage stats: total LLM calls, token count, estimated cost (from inference_usage)
- Status: Online / Offline (based on pod Running state)
- Last active: timestamp of most recent inference call

---

## Consequences

### Positive

- Zero changes required to BYOC deployments — automatic discovery
- Unified portal view for operators and tenant users
- Usage data available without additional instrumentation

### Negative

- Non-agent pods in tenant namespaces with SPIFFE identities may appear in the portal — mitigated by the label convention for naming and future filtering options
- k8s watch permissions required across all tenant namespaces — scope documented in [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md)

### Neutral

- BYOC agents appear and disappear with pod lifecycle — not persistent records like managed agent types

---

## Validation

### Success Metrics

- BYOC agent appears in portal within 30 seconds of pod reaching Running state with SPIFFE annotation
- BYOC agent disappears from portal within 30 seconds of pod termination
- Usage stats for BYOC agents match inference-gateway records with 100% accuracy

---

## Related Decisions

- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE identity that signals BYOC agent presence
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — Token injection that enables BYOC LLM access
- [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md) — Service that runs the pod watch
- [agent-portal-and-chat-ui](2026-05-11-agent-portal-and-chat-ui.md) — Portal UI that surfaces BYOC agents

---

**Last Updated:** 2026-05-08

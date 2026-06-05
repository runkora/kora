# Agent Identity & Policy Provisioning

**Status:** Accepted
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** When a managed agent type is created, the platform must ensure its pods receive a SPIFFE identity and that grants are provisioned in the policy service. This ADR defines the provisioning model, lifecycle, and the identity path the platform assigns to agent workloads.

---

## Context

### Background

The Orbit identity and policy model ([unified-identity-model](2026-03-05-unified-identity-model.md), [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md), [dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md), [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md)) supports agents as first-class principals. SPIFFE attestation is handled cluster-wide by the SPIRE controller manager via `ClusterSPIFFEID` resources ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md)). The `orbit-tenant-agent` ClusterSPIFFEID already exists and automatically issues SVIDs to any pod in a tenant namespace carrying the label `spiffe.io/agent={value}`. The identity-service's `XFCCPrincipalExtractor` already derives the full principal (type, tenant, identity) directly from the SPIFFE path — no runtime mapping table is required.

What is missing is the provisioning lifecycle for managed agents: ensuring the StatefulSet pod template carries the correct label (so the ClusterSPIFFEID issues the right SVID), and that grants are created in the policy-service at agent type creation time.

### Problem Statement

Creating a managed agent type requires two things:
1. Agent pods must carry the `spiffe.io/agent` label so the existing ClusterSPIFFEID assigns a SPIFFE identity
2. Grants must be created in the policy-service so the OPA bundle governs which LLM models the agent can call

Both must be provisioned automatically and cleaned up on deletion. Neither requires direct SPIRE API access.

### Goals

- Agent pods receive a SPIFFE identity automatically via the existing ClusterSPIFFEID — no manual SPIRE configuration
- Grants created automatically from the agent type's `tool_scopes`
- Both cleaned up automatically on agent type deletion
- Least-privilege: agents only access what their `tool_scopes` specify
- No manual SPIRE or policy configuration required from tenant users

### Non-Goals

- Per-session or per-run identity changes (identity is per agent type, not per run)
- Delegation / act-on-behalf tokens (deferred)
- MCP tool access provisioning (deferred)

---

## Decision Drivers

* The `orbit-tenant-agent` ClusterSPIFFEID already handles SVID issuance for any pod in a tenant namespace with `spiffe.io/agent={value}` — there is nothing to provision at the SPIRE layer
* `principal_extractor.go` derives `principal_type`, `tenant_id`, and `principal_id` entirely from the SPIFFE URI path — no external mapping table is needed
* Grants must still be explicitly provisioned; the policy-service does not auto-derive grants from SPIFFE identity
* `orbit-agents`' own SPIFFE identity is `spiffe://{trust-domain}/system/orbit-agents` (via the `orbit-system-component` ClusterSPIFFEID and pod label `spiffe.io/component=orbit-agents`)

---

## Considered Options

### Option A: Operator manages SPIRE entries via ClusterSPIFFEID extension

**Description:** The operator creates a new `ClusterSPIFFEID` per agent type on agent type creation and deletes it on deletion.

**Pros:**
- SPIRE management stays in the operator

**Cons:**
- Dynamic ClusterSPIFFEID creation per agent type is unnecessary — the existing `orbit-tenant-agent` ClusterSPIFFEID already covers any pod with `spiffe.io/agent={value}` in a tenant namespace
- Requires operator involvement for what is a data-plane label, not an infrastructure change
- Splits agent lifecycle management between operator and orbit-agents

### Option B: orbit-agents calls SPIRE registration gRPC API directly

**Description:** `orbit-agents` registers SPIRE workload entries programmatically via the SPIRE Server gRPC API.

**Pros:**
- orbit-agents owns the full provisioning lifecycle

**Cons:**
- Not how the Orbit platform works: the platform uses ClusterSPIFFEID resources managed by the SPIRE controller manager, not direct SPIRE registration API calls
- Requires orbit-agents to hold SPIRE admin registration rights and manage per-node parentID entries — significant complexity
- The existing ClusterSPIFFEID mechanism already handles this correctly with zero additional infrastructure

### Option C: Label-driven identity via existing ClusterSPIFFEID (chosen)

**Description:** `orbit-agents` ensures each agent StatefulSet pod template includes the label `spiffe.io/agent={agent_type_id}`. The existing `orbit-tenant-agent` ClusterSPIFFEID automatically issues SVIDs to matching pods. No SPIRE management code is needed in `orbit-agents`.

**Pros:**
- Zero additional SPIRE infrastructure — reuses existing ClusterSPIFFEID
- Consistent with how all other tenant workloads receive identity on the platform
- No SPIRE admin rights required for orbit-agents
- Node scheduling, pod restarts, and scale events are all handled transparently

**Cons:**
- orbit-agents must ensure the label is set correctly on the StatefulSet pod template — if the label is wrong, the pod receives no SVID (caught by SVID presence health check on pod startup)

---

## Decision

**Chosen Option: C — Label-driven identity via existing ClusterSPIFFEID**

### SPIFFE Identity for Managed Agents

The SPIFFE ID assigned to managed agent pods follows the platform's tenant agent path ([dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md)):

```
spiffe://{trust-domain}/tenant/{namespace}/agent/{agent_type_id}
```

For a managed agent type with ID `{agent_type_id}` in tenant namespace `orbit-tenant-{tenant_id}`:

```
spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{agent_type_id}
```

This is issued automatically by the `orbit-tenant-agent` ClusterSPIFFEID when the pod carries:
- Label: `spiffe.io/agent={agent_type_id}`
- Namespace label: `orbit.io/role: tenant` (already set on all tenant namespaces)

`orbit-agents` sets this label in the StatefulSet pod template spec at agent type creation time. No SPIRE registration code is required.

### Principal Resolution

The `XFCCPrincipalExtractor` (identity-service) parses the SPIFFE URI at JWT exchange time:

| Path segment | Value |
|---|---|
| `/tenant/{namespace}` → `TenantID` | `orbit-tenant-{tenant_id}` |
| `/agent/{agent_type_id}` → `ID` | full SPIFFE URI |
| `principal_type` | `AGENT` |

The `principal_id` used throughout the platform (grants, audit logs, usage attribution) is the **full SPIFFE URI**: `spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{agent_type_id}`. This is deterministic from the agent type ID and tenant ID.

No identity-service mapping registration is needed — the SPIFFE path encodes all required information.

### Grant Provisioning

When an agent type is created, `orbit-agents` calls the policy-service to create a grant:

```json
{
  "tenant_id":      "{tenant_id}",
  "principal_type": "AGENT",
  "principal_id":   "spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{agent_type_id}",
  "scopes":         ["{scope1}", "{scope2}", ...]
}
```

`tool_scopes` from the agent type definition map directly to grant scopes. The policy-service validates each scope against the scope vocabulary ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)).

`orbit-agents` is a SYSTEM principal — `resolveTenantID` in the policy-service passes the supplied `tenant_id` through directly for SYSTEM callers rather than deriving it from the JWT. orbit-agents must therefore explicitly supply the agent type's tenant identifier.

On agent type deletion, the grant is deleted from the policy-service by `principal_id`.

### Provisioning Order (on Create)

1. Create grant in policy-service
2. Provision StatefulSet with `spiffe.io/agent={agent_type_id}` label on pod template ([agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md))
3. SPIRE controller manager automatically issues SVID to pods as they reach Running
4. Mark agent type `active`

### Teardown Order (on Delete)

1. Drain active runs
2. Delete StatefulSet and all k8s resources
3. Delete grant from policy-service

The grant is deleted last — ensuring the agent cannot initiate new LLM calls via an orphaned JWT after the StatefulSet is torn down. There is no SPIRE entry to delete; the SVID ceases to be issued automatically once the pod is gone (the ClusterSPIFFEID selector no longer matches).

---

## Consequences

### Positive

- Zero additional SPIRE infrastructure — fully reuses the existing ClusterSPIFFEID mechanism
- orbit-agents requires no SPIRE admin rights
- Identity is consistent with all other platform tenant workloads — same attestation path, same JWT structure
- Pod restarts, node changes, and StatefulSet scaling all handled transparently
- Simpler provisioning: grant creation is the only external call on agent type create

### Negative

- If `spiffe.io/agent` label is missing or wrong on the pod template, the pod will not receive a SVID and LLM calls will fail at the JWT exchange step — requires a SVID presence health check in the session-manager startup probe

### Neutral

- SVID TTL and rotation are managed by the SPIRE controller manager — no configuration needed in orbit-agents

---

## Validation

### Success Metrics

- Pod receives SVID within 30 seconds of StatefulSet pod reaching Running state
- Grant appears in policy-service for the agent's full SPIFFE URI within 10 seconds of agent type creation
- All grants removed within 30 seconds of agent type deletion
- Session-manager startup probe confirms SVID presence before accepting run assignments

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — AGENT principal type and JWT structure
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE infrastructure and ClusterSPIFFEID
- [dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — SPIFFE path scheme; `orbit-tenant-agent` ClusterSPIFFEID definition
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Grant model
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Scope vocabulary
- [agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md) — Agent type definition that drives provisioning
- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) — StatefulSet pod template where the label is set
- [orbit-agents-service-architecture](2026-05-11-orbit-agents-service-architecture.md) — Service that performs provisioning

---

**Last Updated:** 2026-05-08

# Agent Identity & Policy Provisioning

**Status:** Accepted (Amended 2026-09-29)
**Date:** 2026-05-11
**Deciders:** Platform Engineering  
**Technical Story:** When a managed agent type is created, the platform must ensure its pods receive a SPIFFE identity and that grants are provisioned in the policy service. This ADR defines the provisioning model, lifecycle, and the identity path the platform assigns to agent workloads.

---

## Context

### Background

The kora identity and policy model ([unified-identity-model](2026-03-05-unified-identity-model.md), [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md), [dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md), [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md)) supports agents as first-class principals. SPIFFE attestation is handled cluster-wide by the SPIRE controller manager via `ClusterSPIFFEID` resources ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md)). The `orbit-tenant-agent` ClusterSPIFFEID already exists and automatically issues SVIDs to any pod in a tenant namespace carrying the label `spiffe.io/agent={value}`. The identity-service's `XFCCPrincipalExtractor` already derives the full principal (type, tenant, identity) directly from the SPIFFE path — no runtime mapping table is required.

What is missing is the provisioning lifecycle for managed agents: ensuring the StatefulSet pod template carries the correct label (so the ClusterSPIFFEID issues the right SVID), and that grants are created in the policy-service at agent type creation time.

### Problem Statement

Creating a managed agent type requires two things:
1. Agent pods must carry the `spiffe.io/agent` label so the existing ClusterSPIFFEID assigns a SPIFFE identity
2. Grants must be created in the policy-service so the OPA bundle governs which LLM models the agent can call

Both must be provisioned automatically and cleaned up on deletion. Neither requires direct SPIRE API access.

### Goals

- Agent pods receive a SPIFFE identity automatically via the existing ClusterSPIFFEID — no manual SPIRE configuration
- Grants created automatically from the agent type definition
- Both cleaned up automatically on agent type deletion
- Least-privilege: agents only access what their grant specifies
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
- Not how the kora platform works: the platform uses ClusterSPIFFEID resources managed by the SPIRE controller manager, not direct SPIRE registration API calls
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

*(Amended 2026-09-29: the agent path segment is the agent type slug, not its ID, and the extracted tenant ID drops the `orbit-tenant-` prefix. See [agent token breakdown, Amendment 1](2026-06-23-agent-token-breakdown-and-spend-card.md).)*

The SPIFFE ID assigned to managed agent pods follows the platform's tenant agent path ([dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md)):

```
spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}
```

For a managed agent type with slug `{slug}` in tenant namespace `orbit-tenant-{tenant_id}`:

```
spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{slug}
```

This is issued automatically by the `orbit-tenant-agent` ClusterSPIFFEID when the pod carries:
- Label: `spiffe.io/agent={slug}`
- Namespace label: `orbit.io/role: tenant` (already set on all tenant namespaces)

`orbit-agents` sets this label in the StatefulSet pod template spec at agent type creation time. No SPIRE registration code is required.

### Principal Resolution

The `XFCCPrincipalExtractor` (identity-service) parses the SPIFFE URI at JWT exchange time:

| Path segment | Value |
|---|---|
| `/tenant/{namespace}` → `TenantID` | `{tenant_id}` (the `orbit-tenant-` prefix is stripped) |
| `/agent/{slug}` → `ID` | full SPIFFE URI |
| `principal_type` | `AGENT` |

The `principal_id` used throughout the platform (grants, audit logs, usage attribution) is the **full SPIFFE URI**: `spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{slug}`. This is deterministic from the agent type slug and tenant ID.

No identity-service mapping registration is needed — the SPIFFE path encodes all required information.

### Grant Provisioning

When an agent type is created, `orbit-agents` calls the policy-service to create a grant:

```json
{
  "tenant_id":      "{tenant_id}",
  "principal_type": "AGENT",
  "principal_id":   "spiffe://{trust-domain}/tenant/orbit-tenant-{tenant_id}/agent/{slug}",
  "scopes":         ["{scope1}", "{scope2}", ...]
}
```

The agent type's grant scopes (`grant_scopes`, plus the scopes compiled from its connectivity, connectors and MCP tools) map directly to grant scopes; `tool_scopes` never fed the grant and is removed ([agent-registry-and-definition-model](2026-05-11-agent-registry-and-definition-model.md), Amendment 1). The policy-service validates each scope against the scope vocabulary ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)).

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

## Amendment — 2026-06-30: Inference grant scopes are derived server-side from `model_id`

**Status:** Accepted — extends **Grant Provisioning**.

### Context

The agent's effective grant is already composed **server-side** from its declared intent: connectors compile to `connection:<name>:<action>` (`AgentType.ConnectorScopes()`), connectivity to `egress:<host>:connect` (`Connectivity.EgressScopes()`), and `EffectiveGrantScopes()` unions those — plus connector auto-egress — with `grant_scopes` before `ensureAgentGrant` reconciles the result to the policy-service ([agent-sandbox-toolchain-and-connectivity-environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md)).

The **inference** portion of that grant is the lone exception. `inference:invoke` plus the fine-grained `inference:{family}:{name}:use` for the agent's model are derived **client-side** and passed in `grant_scopes`; `orbit-agents` stores `grant_scopes` as an opaque passthrough (`CreateAgentType` defaults a missing slice to empty — "the agent has no policy grants"). The same rule is therefore duplicated in every client — the frontend BFF (`create-agent-form.tsx` `deriveGrantScopes`) and orbit-mcp. Two consequences:

- **Drift.** The rule (`inference:invoke` + `inference:{family}:{name}:use` from a `family/name` model id) lives in both TypeScript and Go and must be hand-synced.
- **A footgun.** A client that sends `model_id` but omits the derived scopes creates an agent whose async provisioning fails — it has no inference grant, so it cannot call the gateway it was created to use.

Connectors and egress are server-derived from intent; inference should be too.

### Decision

`orbit-agents` derives the inference grant scopes from `model_id` in `CreateAgentType`/`UpdateAgentType` — the single source of truth, exactly as it already does for connectors and connectivity. Clients declare intent (`model_id`) and stop deriving.

- **Derivation.** When the request carries no explicit `inference:*` scope, the service adds `inference:invoke`, plus `inference:{family}:{name}:use` when `model_id` is a two-segment `family/name`. These flow into the existing `EffectiveGrantScopes()` union — grant reconciliation is unchanged.
- **Override preserved.** If the caller supplies any `inference:*` scope explicitly, it is taken as-is and nothing is auto-added — non-default grants are unaffected.
- **Update.** When `model_id` changes and no explicit inference scope is supplied, the inference scopes are re-derived so the grant stays aligned with the model (matching today's UI edit behaviour).
- **Still bounded by anti-escalation.** Grant creation already rejects scopes exceeding the caller's (delegation intersection). Server-side derivation can only attach a scope the caller already holds; it opens no new access path. The agent ends up with the exact scopes it gets today via the UI.
- **No catalogue coupling (non-goal).** The service derives the scope string from `model_id` syntactically; it does **not** validate the model against the inference-gateway catalogue — consistent with how connector/egress scopes compile without a cross-service check. Validating `model_id` is deferred (Future Work).

### Consequences

- **Single source of truth.** The derivation lives once, in the service that owns the grant. The frontend BFF and orbit-mcp delete their derivation and send only `model_id`; create-with-just-`model_id` provisions correctly. No cross-language drift.
- **Coupling.** `orbit-agents` now encodes the `inference:{family}:{name}:use` scope-string format (the inference scope vocabulary). It is already referenced across the platform; documenting it here is acceptable.
- **Back-compatible.** Clients that still send the derived scopes are unaffected — derivation is idempotent and explicit inference scopes suppress it — so the server change and the client cleanup need not be a flag day.

### Implementation pointer

A `deriveInferenceScopes(modelID string) []string` in `internal/agents`, applied in `CreateAgentType`/`UpdateAgentType` before the `EffectiveGrantScopes()` composition; then remove the duplicated derivation from orbit-mcp (`internal/koramcp/tools_agents.go` `deriveGrantScopes`) and the frontend BFF (`create-agent-form.tsx`, `use-agent-settings.ts`).

---

**Last Updated:** 2026-06-30

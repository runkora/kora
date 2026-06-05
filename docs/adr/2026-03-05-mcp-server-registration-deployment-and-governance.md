# MCP Server Registration, Deployment, and Governance

**Status:** Accepted
**Date:** 2026-03-05
**Deciders:** Platform Architecture Team
**Technical Story:** Establishes how Model Context Protocol (MCP) servers are registered, deployed, discovered, and governed within the Orbit platform as first-class principal participants in the agent delegation chain.

---

## Context

The Model Context Protocol (MCP) defines a standard interface for exposing tools to AI agents. An MCP server declares a set of callable tools and communicates with agents over Server-Sent Events (SSE). Agents — particularly LLM-driven ones — discover these tools and reason over them to complete tasks.

Orbit needs to treat MCP servers as governed platform components, not as arbitrary external services. An MCP server deployed into Orbit participates in the delegation chain ([agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md)), carries a verified workload identity ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md)), operates behind an Envoy sidecar ([universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)), and routes all outbound external calls through the Credential-Aware Outbound Gateway ([credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md)). This makes the MCP server a first-class `SERVICE` principal — not a sidecar decoration or a thin HTTP wrapper.

Without a formal registration and governance model, MCP servers would be opaque to the platform: their identity would be unverifiable, their tool surfaces ungoverned, their outbound credential usage untraceable, and their availability to agents unpredictable.

### Background

The MCP ecosystem includes both first-party servers (built and maintained by the Orbit platform team, e.g. an MSGraph MCP or a Jira MCP) and third-party servers (open-source images or custom servers built by tenant engineering teams). Orbit must support both without requiring code ownership or internal instrumentation.

[universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) established that all Orbit services — including those whose code Orbit does not own — receive an Envoy sidecar that handles mTLS, token validation, and OPA policy enforcement at the network layer. MCP servers inherit this model entirely.

Multiple teams sharing an Orbit instance (multi-tenancy) is a core platform requirement. Registration and discovery must be scoped to the correct tenancy boundary.

### Problem Statement

Orbit needs a defined model for:
- How MCP servers are registered and at what scope (platform vs tenant)
- What identity and deployment infrastructure they receive
- How agents discover MCP servers appropriate to their delegation context
- How the platform governs which agents can connect to which MCP servers
- Who is responsible for tool manifest correctness

### Goals

- Register MCP servers as first-class `SERVICE` principals with SPIFFE identities
- Support both platform-level (shared across tenants) and tenant-level (private to a tenant) registration
- Provide two complementary discovery models: pull (dynamic) and push (instantiation-time)
- Enable policy-bound discovery so agents only see MCP servers they are permitted to connect to
- Treat platform-managed and tenant-deployed servers uniformly at the enforcement layer
- Keep the registration model simple enough for third-party / open-source MCP servers to participate without code changes

### Non-Goals

- Orbit does not validate MCP tool manifests or verify that declared scope requirements are accurate
- Orbit does not implement the MCP protocol itself — it governs servers that implement it
- Orbit does not restrict which external APIs an MCP server may call, beyond the enforcement applied by the Outbound Gateway ([credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md))
- Tool-level path enforcement is handled by the Outbound Gateway, not the registration layer

---

## Decision Drivers

* **Identity integrity**: every component in the delegation chain must have a verifiable workload identity
* **Multi-tenancy**: platform-level and tenant-level MCP servers must be appropriately scoped and isolated
* **Agent framework compatibility**: many agent frameworks support MCP natively but cannot call Orbit-specific discovery APIs — the push model must accommodate these
* **Third-party operability**: open-source MCP servers must be deployable into the platform without code changes
* **Uniform enforcement**: enforcement posture must not differ based on who authored the MCP server
* **Audit completeness**: the platform must have end-to-end traceability when an agent invokes a tool through an MCP server that calls an external API

---

## Considered Options

### Option 1: Platform-Managed Registry Only

**Description:** All MCP servers are registered and managed by the Orbit platform team. Tenants cannot deploy their own MCP servers — they consume platform-provided ones.

**Pros:**
- Strong governance — every MCP server is reviewed and managed centrally
- Consistent deployment standards
- Simplified trust model

**Cons:**
- Blocks tenant teams from deploying custom or specialist MCP servers
- Creates a bottleneck on the platform team for every new tool surface
- Incompatible with the open MCP ecosystem where teams bring their own servers

### Option 2: Tenant-Managed Registry Only

**Description:** All MCP servers are registered at the tenant level. There are no platform-wide shared servers.

**Pros:**
- Full tenant autonomy
- No cross-tenant visibility concerns

**Cons:**
- Every tenant must independently deploy and maintain common MCP servers (MSGraph, Jira, etc.)
- Duplicates effort across tenants for common integrations
- No mechanism for platform team to provide curated, validated first-party tool surfaces

### Option 3: Hierarchical Registry — Platform and Tenant Levels

**Description:** MCP servers can be registered at two scopes. Platform-level registrations are managed by the Orbit platform team and are available across all tenants (subject to policy). Tenant-level registrations are private to a tenant, managed by tenant operators. Both levels use the same identity, deployment, and enforcement model.

**Pros:**
- Platform team can provide curated first-party MCP servers (MSGraph, Jira, GitHub)
- Tenant teams retain autonomy to deploy specialist or custom servers
- Enforcement is uniform regardless of registration level
- Provenance is captured as metadata without creating a two-tier enforcement model

**Cons:**
- Slightly more complex registration API (must specify scope)
- Platform-level servers require governance process for additions

---

## Decision

**Chosen Option:** Option 3 — Hierarchical Registry with Platform and Tenant Levels

### Rationale

The platform needs to provide first-party, curated MCP servers for common integrations while also enabling tenant engineering teams to deploy custom or open-source servers. These are not competing requirements — they serve different use cases and can coexist within the same registry model.

Critically, the enforcement posture is identical for both levels. Both receive a SPIFFE identity, both are deployed with an Envoy sidecar, both participate in the delegation chain, and both route outbound calls through the Outbound Gateway. The registration level (platform vs tenant) is a governance metadata attribute — it does not create a privileged trust tier.

**Tool manifest responsibility:** Orbit does not attempt to validate that an MCP server's declared tool-to-scope mapping is accurate. This is the MCP author's responsibility. The Outbound Gateway provides the real enforcement gate — even if a tool manifest incorrectly declares a lower scope requirement than the underlying API call actually uses, the gateway's scope intersection computation (performed independently from the manifest) will enforce the correct ceiling. The manifest is a discovery and UX aid, not a security boundary.

**Discovery model:** Two complementary models are supported:

- **Pull model**: An agent queries `GET /mcp/services` to retrieve the list of MCP servers available under its current delegation context. The platform filters this list based on the agent's capability grants. This model suits long-running agents that acquire tasks with varying delegation scopes mid-session and need to refresh their tool surface dynamically.

- **Push model**: When a task is dispatched to an agent, Orbit includes the relevant MCP server configuration in the task context alongside the delegation token. The agent framework receives a fully-configured MCP server list at instantiation time. This model is essential for agnostic agent frameworks (LangChain, AutoGen, CrewAI, etc.) that speak MCP natively but provide no hook to call an Orbit-specific discovery API. If Orbit pushes the configuration at task dispatch, those frameworks work without modification.

Both models produce the same result: the agent (or its framework) has a set of MCP server connection parameters scoped to what the delegation context permits.

---

## Consequences

### Positive

- MCP servers are fully traceable participants in the audit chain — from agent tool invocation through to external API call
- Tenant teams can deploy open-source or custom MCP servers without platform team involvement
- Platform team can provide curated first-party tool surfaces available across all tenants
- Agnostic agent frameworks gain MCP server access via the push model without Orbit-specific code
- Enforcement is uniform — no special-casing for platform vs tenant vs third-party servers

### Negative

- Orbit must maintain a registration schema and API for MCP server onboarding at both levels
- Platform-level MCP server additions require a governance process to prevent unreviewed tool surfaces being exposed platform-wide
- The push model requires Orbit to know at task-dispatch time which MCP servers are relevant to the task — this implies the task dispatch system must reason about MCP availability

### Neutral

- MCP servers are `SERVICE` principals — they are not a new principal type, they inherit the full existing identity and deployment model
- Provenance (platform-managed vs tenant-deployed) is captured as a metadata field on the registration record — it does not affect enforcement behaviour

---

## Implementation

### MCP Server Registration Schema

Each registered MCP server declares:

```yaml
id: mcp-msgraph
display_name: Microsoft Graph MCP
scope: platform                          # platform | tenant
tenant_id: null                          # null for platform-scope; tenant ID for tenant-scope
spiffe_id: spiffe://orbit.{instance-id}/system/service/mcp-msgraph
endpoint: https://mcp-msgraph.internal.orbit.svc
transport: sse
external_scopes_consumed:               # what external scopes tools in this server may request
  - msgraph:mail:read
  - msgraph:mail:write
  - msgraph:calendar:read
  - msgraph:calendar:write
  - msgraph:files:read
tools:                                   # declarative manifest — not validated by Orbit
  - name: list_emails
    required_scope: msgraph:mail:read
  - name: send_email
    required_scope: msgraph:mail:write
  - name: list_calendar_events
    required_scope: msgraph:calendar:read
```

### Identity and Deployment

Every registered MCP server receives:

1. A SPIFFE identity issued by SPIRE, determined by scope:
   - **Platform-scope**: `spiffe://orbit.{instance-id}/system/service/{service-id}`
   - **Tenant-scope**: `spiffe://orbit.{instance-id}/tenant/{tenant-id}/service/{service-id}`
2. An Envoy sidecar deployed alongside the server process, handling:
   - Inbound mTLS termination from agents
   - `X-Orbit-Delegation-Token` header validation via `ext_authz`
   - OPA policy evaluation for which agents may connect
3. Registration in the Orbit service registry for discovery

Open-source or third-party MCP servers that Orbit does not own require no code changes — the Envoy sidecar handles all platform integration at the network layer.

### Pull Discovery Model

```
GET /mcp/services
Authorization: Bearer {platform-session-token}
X-Orbit-Delegation-Token: {delegation-token}

Response:
{
  "mcp_servers": [
    {
      "id": "mcp-msgraph",
      "display_name": "Microsoft Graph MCP",
      "endpoint": "https://mcp-msgraph.internal.orbit.svc",
      "transport": "sse",
      "tools": [
        { "name": "list_emails", "description": "..." },
        { "name": "read_email", "description": "..." }
      ]
    }
  ]
}
```

The platform filters the response to servers the agent is permitted to access. The tool list returned per server is the intersection of the server's full tool manifest and the agent's delegation scopes — the agent receives only the tools it can actually invoke.

### Push Discovery Model

At task dispatch, Orbit includes MCP server configuration in the task context:

```json
{
  "task_id": "task-abc",
  "delegation_token": "{X-Orbit-Delegation-Token}",
  "mcp_servers": [
    {
      "id": "mcp-msgraph",
      "endpoint": "https://mcp-msgraph.internal.orbit.svc",
      "transport": "sse",
      "headers": {
        "X-Orbit-Delegation-Token": "{delegation-token}"
      }
    }
  ],
  "task_payload": { ... }
}
```

The agent framework consumes this configuration and connects to the listed MCP servers directly, presenting the delegation token on the SSE connection. No discovery call is required.

### Migration Path

There is no existing MCP server registration model to migrate from. This is a greenfield definition.

---

## Validation

### Success Metrics

- Agent frameworks without Orbit-specific code successfully connect to and invoke tools from MCP servers via the push model
- Audit logs capture complete delegation chains (agent → MCP server → external API) for all tool invocations
- Tenant teams can register and deploy custom MCP servers without platform team involvement beyond initial access grants
- MCP servers deployed from open-source images participate in the platform without code changes

### Monitoring

- Registration events emitted for all MCP server onboarding operations
- Connection events emitted when agents establish SSE sessions with MCP servers
- Rejection events emitted when agents attempt to connect to MCP servers outside their delegation scope

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — `SERVICE` principal type that MCP servers instantiate
- [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — Delegation chain patterns that MCP servers participate in
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE identity model MCP servers receive
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Envoy sidecar deployment model applied to MCP servers
- [credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md) — Outbound Gateway that MCP servers call for external API access
- [scope-gated-tool-exposure-via-mcp](2026-03-05-scope-gated-tool-exposure-via-mcp.md) — How MCP servers filter tool surfaces based on delegation scope

---

## References

- [Model Context Protocol Specification](https://spec.modelcontextprotocol.io)
- [SPIFFE Workload API](https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/)
- [RFC 8693 — OAuth 2.0 Token Exchange](https://datatracker.ietf.org/doc/html/rfc8693)
---

## Notes

The tool manifest declared at registration time is a discovery and user-experience aid. It is not a security boundary. The Outbound Gateway's scope intersection enforcement is the authoritative gate for all external API calls, regardless of what the tool manifest declares.

**Last Updated:** 2026-03-03

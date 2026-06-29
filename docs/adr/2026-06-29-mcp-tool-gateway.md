# MCP / Tool Gateway

<!-- Filename: 2026-06-29-mcp-tool-gateway.md -->

**Status:** Proposed
**Date:** 2026-06-29
**Deciders:** Justin Wilkin, Orbit Core Team
**Technical Story:** [MCP / Tool Gateway PRD](../prd/mcp-tool-gateway.md)

---

## Context

### Background

The [registration ADR](2026-06-29-mcp-server-registration-and-introspection.md) turns each MCP server's tool surface into governable `mcp:<server>:<tool>:use` scopes. This ADR is the component that *enforces* them: a governing proxy that both humans (via desktop/IDE MCP clients) and agents (via their injected platform token) connect to, which filters every `tools/list` and checks every `tools/call` against the caller's grants — down to the individual tool — before forwarding to the upstream server.

This is the missing consumption plane. The [2026-03-05 scope-gated-exposure ADR](2026-03-05-scope-gated-tool-exposure-via-mcp.md) assumed the MCP server filters its *own* tool surface, which only works for servers we own and modify. The gateway inverts that: enforcement happens at the platform, so unmodified open-source servers are governed identically. For governed servers, **this ADR supersedes** the server-side-filtering approach.

### Problem Statement

A `tools/call` is a single JSON-RPC method on one endpoint; the tool being invoked is in the request *body* (`params.name`), not the URL path. Orbit's Layer-1 OPA sidecar enforces on the *route* and is therefore blind to which tool is called. Per-tool authorisation can only happen in a component that parses MCP semantics. We need that component: an MCP-aware PEP and proxy, with two front doors (human, agent) over one enforcement spine, that reuses the existing grant/scope machinery rather than inventing a parallel access model.

### Goals

- One governed MCP endpoint, speaking standard MCP, for humans and agents.
- Per-tool enforcement: filter `tools/list`, check `tools/call`, default-deny — reusing the existing in-process policy evaluator.
- Agent provisioning: tool selection in the agent UI → reconciled grant; an unauthenticated generated session config.
- Human provisioning: connect a desktop MCP client (auth model per the [OAuth ADR](2026-06-29-mcp-oauth-authentication.md)); see exactly granted tools.
- Per-decision audit via existing OTel spans + structured logs.

### Non-Goals

- Server registration & introspection — the [registration ADR](2026-06-29-mcp-server-registration-and-introspection.md).
- The human authentication mechanism — the [OAuth ADR](2026-06-29-mcp-oauth-authentication.md); this ADR consumes its resulting principal.
- Remote third-party MCP servers; argument-level policy; `resources/*`/`prompts/*`; delegation `act`-chain enforcement across the gateway — all deferred per the PRD.
- Credential injection on the gateway→server hop (managed microservice owns its own backend creds).

---

## Decision Drivers

- **The per-tool decision must be MCP-aware** — only a body-parsing component can authorise `tools/call`.
- **Reuse the gateway template.** inference-gateway and policy-gateway already define the binary, config, controller, middleware, evaluator, and streaming patterns. The tool gateway is another instance, not a new framework.
- **No parallel access model.** Grants and scopes are policy-service's; the gateway is a PEP only.
- **Agent ergonomics.** Agents must reach tools with zero credential handling, matching how model access already works.

---

## Considered Options

### Decision 1 — Deployment shape

- **Option A: New `cmd/tool-gateway` binary + `internal/toolgateway` package (chosen).** Mirrors inference-gateway/policy-gateway: its own port, config, SPIFFE service identity, horizontal scaling. A gateway in the request path of all tool calls deserves its own scaling and blast-radius boundary.
- **Option B: A feature inside orbit-agents.** Couples a shared, latency-sensitive data-plane proxy to the agent control plane; conflicts with independent scaling. Rejected.

### Decision 2 — How the per-tool decision is evaluated

- **Option A: In-process `policy.Evaluator` (OPA bundle), like inference-gateway (chosen).** The gateway runs `policy.NewEvaluator(bundleEndpoint, …)`, `Start`s bundle polling, and calls `Check`/`Query` locally. Sub-millisecond, no per-call network hop, identical to how the inference gateway authorises models. `mcp:<server>:<tool>:use` maps cleanly to `CheckInput{ResourceType:"mcp", PathSegments:[server,tool], Action:"use"}`.
- **Option B: Call policy-service `/policy/check` per request.** A network hop on every tool call; rejected on latency. (The typed policy client still exists for non-hot-path needs.)
- **Layer 1 in front:** the Envoy sidecar keeps a coarse route gate ("may this principal reach the gateway at all", e.g. `mcp:invoke`); the gateway does the per-tool check. Not a redundant double-check — the two enforce at different granularities, and only the gateway can see the tool name.

### Decision 3 — `tools/list` filtering

The gateway calls `Query` once per list, passing the server's registered tools as candidates, and returns only the allowed set (mirrors the policy-gateway `query` pattern). The candidate set comes from the registered catalog for that server, so the filter is one evaluator call over a known list, not a fan-out of N `Check`s. Result is cacheable per `(principal, server, bundle-revision)`.

### Decision 4 — Transport & proxy

MVP speaks **Streamable HTTP** (the current MCP transport) on the consumer side and as client to upstreams. The gateway is an MCP server to consumers and an MCP client to upstreams, mapping consumer sessions to upstream sessions. Streaming responses reuse the inference-gateway SSE/flush pattern. Long-running `tools/call`s get an explicit per-call timeout, context-cancellation propagation to the upstream, and backpressure via the streamed channel.

### Decision 5 — Aggregation & namespacing

One gateway endpoint fronts many upstream servers. Tools are namespaced by server (`<server>__<tool>` on the wire, or per-server mount paths) so two servers exposing `search` don't collide. The namespace maps 1:1 to the `mcp:<server>:<tool>:use` scope path, keeping the wire name and the authz path aligned.

---

## Decision

| # | Decision |
|---|----------|
| 1 | New `cmd/tool-gateway` binary + `internal/toolgateway` package, own SPIFFE identity and port. |
| 2 | Per-tool authz via the in-process `policy.Evaluator` (OPA bundle); Layer-1 sidecar keeps a coarse route gate. |
| 3 | `tools/list` filtered with one `Query` over the server's registered candidates; cacheable per principal+bundle. |
| 4 | Streamable HTTP; gateway proxies consumer↔upstream MCP sessions with timeout/cancel/backpressure. |
| 5 | Server-namespaced tool names aligned 1:1 with the scope path. |
| 6 | Per-decision audit via OTel span + structured log; no event-spine dependency. |

---

## Architecture

### Component & request topology

```
  human MCP client ──(MCP OAuth bearer)──┐
                                          ▼
                                 ┌──────────────────┐         ┌────────────────────┐
  agent (in-pod) ──(platform ───▶│  tool-gateway     │──MCP───▶│ Orbit-hosted MCP   │
   token via sidecar)            │  • authN (verify) │  (in-   │ server (governed)  │
                                 │  • Query: list     │  mesh)  └────────────────────┘
                                 │  • Check: call     │
                                 │  • proxy + stream  │──▶ policy.Evaluator (OPA bundle, in-proc)
                                 │  • OTel audit      │──▶ policy-service /routes/register (Layer-1 gate)
                                 └──────────────────┘
```

### Binary & controller

Mirrors inference-gateway `main.go`: `initObservability → initLogger → initStorage → initControllers → initServer → run`. A single `toolgateway.Controller` implements `api.Controller`:

```go
func (c *Controller) GroupedBy() string { return "/mcp" }

func (c *Controller) Middleware() mw.MiddlewareHandler {
    return mw.MiddlewareChain(
        xAPIKeyMiddleware(),                       // x-api-key → Bearer (parity with inference-gw)
        mcpOAuthMiddleware(c.oauth, c.logger),     // human path: validate MCP OAuth bearer (OAuth ADR)
        mw.AuthMiddleware(c.verifier, c.logger),   // agent path: validate injected platform token
        mw.ScopeEnforcementMiddleware(c.eval, ...) // Layer-1-equivalent coarse gate: mcp:invoke
    )
}

func (c *Controller) Boot(mux *api.Mux) {
    mux.Handle("POST /", c.handleMCP)                       // Streamable HTTP JSON-RPC entrypoint
    mux.Handle("GET /.well-known/oauth-protected-resource", c.handlePRM) // resource-server metadata (OAuth ADR)
}
```

Both personas resolve, via their respective middleware, to a `*token.PlatformClaims` in context — `AuthMiddleware`/`ClaimsFromContext`. From there the enforcement spine is identical.

### The PEP — list filter & call check

`handleMCP` dispatches by JSON-RPC method:

```go
claims, _ := token.ClaimsFromContext(ctx)

switch req.Method {
case "tools/list":
    candidates := c.catalog.Tools(ctx, claims.Orbit.TenantID, server) // registered tools for server
    allowed, _ := c.eval.Query(ctx, policy.QueryInput{
        Claims: claims, ResourceType: "mcp", Action: "use",
        Candidates: toCandidates(server, candidates), // PathSegments [server, tool]
    })
    return filteredList(allowed) // only granted tools advertised

case "tools/call":
    res, _ := c.eval.Check(ctx, policy.CheckInput{
        Claims: claims, ResourceType: "mcp",
        PathSegments: []string{server, normalise(toolName)}, Action: "use",
    })
    if !res.Allow {
        audit(ctx, claims, server, toolName, "deny", res.Reason)
        return mcpError(-32001, "tool not permitted")
    }
    audit(ctx, claims, server, toolName, "allow", "")
    return c.proxy.Call(ctx, server, req) // forward + stream upstream response
}
```

Default-deny is structural: `Query` omits ungranted tools so they never enter the model's possibility space, and `Check` rejects an ungranted `tools/call` even if the client guessed the name.

### Agent provisioning (reused reconciliation)

Selecting tools in the agent settings UI extends exactly the model-access path ([agent provisioning ADR](2026-05-11-agent-identity-and-policy-provisioning.md)):

- The UI derives `mcp:<server>:<tool>:use` (or `mcp:<server>:use`) scopes, alongside the model-derived `inference:…:use` scopes, and sends them as the agent's `grant_scopes`.
- `ensureAgentGrant` reconciles the agent's **single** grant (delete-and-recreate, since grants are immutable) to carry model + tool scopes together. No new grant per agent.
- At session spin-up, `writeMCPConfig` (today hardcodes the `runtime` server) is extended to emit the **gateway** as an MCP source — an **unauthenticated** entry; the identity-proxy sidecar injects the agent's platform token on the outbound call ([sidecar token injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)), so the config holds no credential.

```jsonc
// generated .mcp-config.json (agent session) — no credentials
{ "mcpServers": { "orbit": { "type": "streamable-http",
    "url": "http://tool-gateway.orbit-system.svc/mcp" } } }
```

### Human provisioning

A human points Claude Desktop / Cursor at the gateway URL; the client performs the MCP OAuth flow ([OAuth ADR](2026-06-29-mcp-oauth-authentication.md)) — the gateway is the **resource server** and serves `/.well-known/oauth-protected-resource`. The resulting bearer resolves to a HUMAN principal; the same `Query`/`Check` spine applies on the human's grants. The dashboard renders a copy-paste client config snippet.

### Audit

Each decision emits an OTel span (`mcp.tools.call`, attributes: principal, tenant, server, tool, decision) and a structured log line, riding the existing observability discipline. No dependency on the (still-stub) event spine; spine emission is additive when it ships.

### Failure & performance

- Upstream unreachable / error → a clean MCP JSON-RPC error to the consumer, never a silent hang; per-call timeout + context cancellation propagated upstream.
- `tools/list` filter: one `Query` over the candidate set, cached per `(principal, server, bundle-revision)`; target **authz < 10 ms p95** added to a call.
- Stateless authz (bundle in-process) → horizontal scaling; the gateway is the SPOF for tool calls and is sized/replicated accordingly.

---

## Consequences

### Positive

- Per-tool governance for any MCP server, owned or not; one endpoint, two personas, one spine.
- Reuses the gateway template, the in-process evaluator, the agent-grant reconciliation, and OTel audit — little genuinely new beyond the MCP proxy itself.
- Agents reach tools with zero credential handling; humans get the native MCP-client experience.

### Negative

- A new always-in-path data-plane component (latency + availability sensitive); must be replicated and watched.
- An MCP proxy with session/state mapping and streaming is non-trivial; transport churn in the MCP spec is a maintenance surface (mitigated by pinning and isolating transport).
- The gateway depends on the registered catalog being fresh (introspection cadence bounds new-tool visibility).

### Neutral

- Layer-1 coarse gate + Layer-2 per-tool check is the same two-layer split as the policy gateway, applied to MCP semantics.
- `tools/list` caching trades a small staleness window (bounded by bundle poll) for latency.

---

## Implementation

- **Phase 1 — Binary & PEP.** `cmd/tool-gateway` + `internal/toolgateway`, config, controller, in-process evaluator; `tools/list` filter + `tools/call` check against a registered server; OTel audit. Exit: agent-token caller reaches only granted tools; default-deny holds.
- **Phase 2 — Proxy & streaming.** Consumer↔upstream session mapping, Streamable HTTP, timeout/cancel/backpressure, aggregation/namespacing.
- **Phase 3 — Agent provisioning.** Settings-UI tool selection → `grant_scopes` → `ensureAgentGrant`; extend `writeMCPConfig` to emit the gateway entry.
- **Phase 4 — Human path.** MCP OAuth resource-server integration (PRM endpoint, bearer validation per OAuth ADR); dashboard client-config snippet + catalog view.

Exit: an agent and a human both reach exactly their granted tools through one endpoint; every call is authorised and audited.

---

## Validation

- **Success metrics:** denied `tools/call` rejected before reaching the upstream; `tools/list` advertises only granted tools for both personas; authz p95 < 10 ms; agent reaches tools with no credential in its config.
- **Monitoring:** `mcp_tool_call_total{decision}`, `mcp_tools_list_filtered`, authz latency histogram, upstream proxy error rate, active MCP sessions.

---

## Related Decisions

- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) — supplies the catalog + scopes this enforces.
- [MCP OAuth Authentication](2026-06-29-mcp-oauth-authentication.md) — the human auth model this consumes.
- [Scope-Gated Tool Exposure via MCP](2026-03-05-scope-gated-tool-exposure-via-mcp.md) — server-side filtering this **supersedes** for governed servers.
- [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md) — binary/controller/streaming/evaluator template.
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) — `Check`/`Query`, two-layer enforcement.
- [Agent Identity and Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — the single-grant reconciliation extended for tool scopes.
- [Envoy ext_proc Sidecar Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — why the agent's MCP config needs no credentials.

## References

- MCP specification — Streamable HTTP transport, `tools/list`, `tools/call` (spec version pinned in implementation).
- `internal/platform/policy/evaluator.go` (`Check`/`Query`), `internal/inferencegw` (binary + SSE streaming), `internal/agents/k8s.go` (`ensureAgentGrant`), `internal/agents/sessionmanager/runner_claude.go` (`writeMCPConfig`).

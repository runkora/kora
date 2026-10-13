# Connectivity

Agents in the kora platform reach the outside world through MCP tools and through connectors. Each governs the egress from an agent in a different way and gives the agent a different mechanism to interact.

## MCP tools

Each run is dynamically bundled with an MCP config at startup that lists only the servers the agent has been granted. The agent uses them through its normal MCP client. `identity-proxy` stamps the platform JWT on every outbound MCP call. The pod's `opa` sidecar authorizes the call at the route level, and the `tool-gateway` then authorizes the specific tool for that identity before it reaches the server.

```mermaid
sequenceDiagram
    autonumber
    participant A as agent (run)
    participant E as pod sidecars (Envoy)
    participant TG as tool-gateway
    participant M as MCP server
    Note over A: run bundled with an MCP config (granted servers only)
    A->>E: MCP call to /run/<id>/v1/mcp/<server> (no credential)
    E->>E: identity-proxy injects platform JWT
    E->>E: opa authorizes the route (mcp:invoke), deny stops it here
    E->>TG: request + Authorization: Bearer <JWT> (allow only)
    TG->>TG: authorize the specific tool (mcp:<server>:<tool>:use)
    TG->>M: forward call (allow only)
    M-->>A: result
```

For the tool-gateway components, per-tool authorization, catalog, and scope registration, see [MCP tool gateway](mcp-tool-gateway.md).

## Connectors

Connectors run as adapters inside the `egress-gateway`. Envoy forces every sandbox egress through it. The pod's `opa` sidecar authorizes the destination (`egress:<host>:connect`) and `identity-proxy` stamps the platform JWT before the tunnel opens. The `egress-gateway` matches the destination to a connection, checks that this identity may use it, then mints and injects a scoped, just-in-time credential. The upstream system remains the authority on what the credential can do.

```mermaid
sequenceDiagram
    autonumber
    participant A as agent (sandbox)
    participant E as pod sidecars (Envoy)
    participant EG as egress-gateway
    participant EXT as external system
    A->>E: HTTPS to a registered host (no credential)
    E->>E: identity-proxy injects platform JWT
    E->>E: opa authorizes egress (egress:<host>:connect), deny stops it here
    E->>EG: forced CONNECT tunnel (allow only)
    EG->>EG: match connection, check policy, mint just-in-time credential
    EG->>EXT: request + injected credential
    EXT-->>EG: response
    EG-->>A: response (agent never saw the credential)
```

For the egress-gateway components, connection matching, and just-in-time credential brokering, see [Egress gateway](egress-gateway.md).

ADRs: [scope-gated-tool-exposure-via-mcp](../adr/2026-03-05-scope-gated-tool-exposure-via-mcp.md), [mcp-tool-gateway](../adr/2026-06-29-mcp-tool-gateway.md), [jit-credential-brokering-egress-gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md), [connection-scope-qualifiers](../adr/2026-07-22-connection-scope-qualifiers.md), [envoy-ext-proc-sidecar-token-injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md).

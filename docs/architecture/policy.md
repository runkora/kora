# Policy

Orbit checks every action against policy before it runs. `policy-service` compiles per-tenant OPA bundles from grants, roles, and a route-scope vocabulary. The edge OPA on the ingress gateway and the in-process middleware in each service both evaluate those bundles.

```mermaid
sequenceDiagram
    autonumber
    participant C as client / agent
    participant GW as edge gateway (Istio)
    participant OPA as edge OPA
    participant SVC as service
    Note over OPA,SVC: bundles compiled and served by policy-service
    C->>GW: request + Bearer token
    GW->>OPA: ext_authz
    OPA->>OPA: match route to scope, check grant / role / delegation
    OPA-->>GW: allow or deny (fail-closed)
    GW->>SVC: routed request (allow only)
    SVC->>SVC: in-process scope check (per-tenant bundle)
    SVC->>SVC: handler runs, or 403 before any action
```

A denial returns before the handler executes. Nothing outside a principal's granted scope reaches the business logic.

For the components that author, compile, and distribute policy, see [Policy plane](policy-plane.md).

ADRs: [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md), [gateway-level-opa-enforcement-for-external-traffic](../adr/2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md), [platform-scope-enforcement-middleware](../adr/2026-06-04-platform-scope-enforcement-middleware.md), [dynamic-route-scope-registration](../adr/2026-04-14-dynamic-route-scope-registration.md).

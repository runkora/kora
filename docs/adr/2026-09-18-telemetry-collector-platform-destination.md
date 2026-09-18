# Telemetry Collector as a Declared Platform Destination

<!-- Filename: 2026-09-18-telemetry-collector-platform-destination.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-09-18
**Deciders:** Justin Wilkin
**Technical Story:** Let agent pods export telemetry to the platform collector without an agent grant and without sending the agent's token. Required by [2026-09-18: Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md).

---

## Context

Agent pods cannot export telemetry. Every outbound request from a tenant pod passes two sidecars:

1. The identity-proxy ([2026-03-21: Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) adds the agent's platform token to every request, whatever the destination.
2. The tenant OPA sidecar allows a request only when its path maps to a scope the principal holds, or when the path is on the platform-internal list in `authz.rego`.

An OTLP export matches neither and is denied with `PermissionDenied`. Observed on a local cluster: the session-manager's own trace export and Claude Code's export were both denied, and the OPA decision log showed the agent's platform token on the export request.

The platform services in `orbit-system` are unaffected. They carry no identity-proxy or tenant OPA sidecar, and `global.observability.collectorNamespace` already opens their egress.

This touches *What can it access?*, because agent pods gain one destination outside their grants, and *How does it connect?*, because no credential is sent to it.

### Goals

- The session-manager and Claude Code export traces, metrics and logs from agent pods to the platform collector.
- The agent's platform token and delegation token never reach the collector.
- The collector is the only destination opened, and only for OTLP export calls.
- The collector is declared once, by the existing `global.observability.otlpEndpoint`.

### Non-Goals

- Sandbox pods.
- OTLP over HTTP. The platform endpoint is gRPC.

---

## Considered Options

### Option 1: The collector as a declared platform destination

The configured collector is treated like the other platform-internal calls in `authz.rego`: allowed without a grant, and bound to the collector's host and the OTLP export paths. The identity-proxy sends no token to it.

### Option 2: An in-pod collector sidecar

A collector container in every agent pod receives on loopback and exports under the UID Envoy does not intercept, the way OPA fetches its JWKS. It adds a container to every agent pod, and a platform container whose egress bypasses enforcement.

### Option 3: A telemetry scope granted to agents

The collector routes map to a scope that every agent is granted. Telemetry export is platform infrastructure rather than an agent action, and the agent's token still reaches the collector.

---

## Decision

**Chosen Option:** Option 1, the collector as a declared platform destination.

1. **One declared value.** `global.observability.otlpEndpoint` names the collector. Its `host:port` is the collector's authority, derived the same way the platform exporters already parse the endpoint.
2. **Policy bundle.** The policy service adds the collector's authority to the tenant bundle's `data.config` as `telemetry_collector`, next to `jwks_url`. It is absent when no endpoint is configured.
3. **Allow rule.** `authz.rego` allows, without a JWT, a request whose `:authority` equals `data.config.telemetry_collector` and whose `:path` starts with `/opentelemetry.proto.collector.`. No other path to the collector and no other authority is allowed by this rule.
4. **No credentials.** The identity-proxy, which already receives the endpoint from the operator, recognises the same authority. For those requests it removes the `Authorization` and delegation headers and adds neither token.
5. **Mesh-registered collector.** The collector must be a host the mesh routes by name: an in-cluster Service, or a ServiceEntry for a collector outside the cluster. Under `ALLOW_ANY`, a request to an unregistered host passes through to whatever address was dialled, so an authority match alone would not guarantee the request reaches the collector.

---

## Consequences

### Positive

- Agent pods deliver session-manager and Claude Code telemetry to the same collector as the platform services.
- No token leaves the pod toward the collector.
- The collector is configured in one place. Changing the endpoint changes the platform exporters, the bundle rule and the identity-proxy together.

### Negative

- Agent pods reach one destination that no grant controls. An agent can write arbitrary data to the collector through the OTLP export calls.
- A collector outside the cluster needs a ServiceEntry, which the deployment guide must document next to `networkPolicy.externalServices`.

### Neutral

- Export requests appear in the tenant OPA decision log as platform-internal allows.

---

## Implementation

- `policyservice/bundle/compiler.go`: `configDocument` gains `telemetry_collector`, set from the policy service's own observability config.
- `policyservice/bundle/authz.rego`: a rule next to `_is_platform_internal_path`, with rego tests for the allowed call, another path to the collector, and an OTLP path to another authority.
- `identityproxy/server.go`: `handleRequestHeaders` skips injection when `:authority` matches the collector, with unit tests.
- `docs/guides/deployment.md`: the ServiceEntry requirement for an external collector.

---

## Validation

On a cluster running this branch with an in-cluster collector and no bypasses:

- The session-manager's and Claude Code's exports from an agent pod reach the collector, and the collector receives no `Authorization` header.
- An OTLP export path to any other authority is denied.
- A non-OTLP path to the collector's authority is denied.
- A request carrying the collector's authority but dialled to another address reaches the collector, not that address.

---

## Related Decisions

- [2026-03-21: Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)
- [2026-03-21: OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md)
- [2026-09-18: Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md)

**Last Updated:** 2026-09-18

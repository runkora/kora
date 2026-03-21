# 0021. Envoy ext_proc Sidecar for Transparent Platform Token Injection

**Status:** Accepted
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Resolving the open implementation question from ADR-0010 and ADR-0011 — the concrete mechanism by which the Envoy sidecar performs the SVID-to-platform-token exchange and injects the resulting token on outbound requests from agent workloads, without any involvement from the agent workload itself.

---

## Context

ADR-0010 specifies that the Envoy sidecar performs the SVID-to-platform-token exchange on behalf of the agent workload, caches the resulting platform JWT internally, and injects it on all outbound requests. The agent workload is completely identity-unaware. ADR-0011 establishes Envoy as the unified data plane and defers the specific filter configuration to subsequent engineering specifications.

Neither ADR specifies the concrete mechanism: what filter type is used, how the exchange call is made, how the token is cached across concurrent requests, and how SVID rotation triggers re-exchange. This ADR closes that gap.

### Background

Envoy provides several extension points for intercepting and modifying HTTP requests in flight:

- **Lua filter** — inline Lua scripts executed per-request inside Envoy's worker threads
- **WASM plugin** — compiled WebAssembly modules executed per-request inside sandboxed WASM VMs
- **External Processing (`ext_proc`)** — a gRPC protocol by which Envoy delegates request/response processing decisions to an external process, communicating via a Unix domain socket or TCP

All three can inject headers on outbound requests. They differ significantly in their programming model, shared-state characteristics, and operational properties.

### Why Platform Tokens and Not SVIDs at the Policy Layer

A reasonable question is: why not skip the exchange entirely and use the SVID directly at the policy layer? The Envoy sidecar already validates the mTLS peer certificate — why not pass the SPIFFE URI through to OPA and evaluate policy against that?

There are three defensible reasons this approach is wrong, and they justify the entire exchange architecture:

**1. SVIDs answer the wrong question.**

An SVID proves which pod is talking — infrastructure identity. It tells you which Kubernetes workload, service account, and namespace originated a request. It tells you nothing about which tenant owns the agent, which principal it maps to in the platform, what scopes or roles it holds, or what session it is operating under. Those are the claims OPA needs to evaluate policy. Without the exchange, you push identity resolution — the SPIFFE URI to platform principal mapping — into OPA on every single request. That is a network call or a Rego data lookup on every evaluation instead of once at startup. The exchange resolves identity once; the platform token carries the result as claims OPA can read in O(1) with no external dependency.

**2. Revocation is surgical, not a sledgehammer.**

Revoking an SVID means deregistering the SPIRE entry for the workload — the entire pod's identity is gone. You cannot selectively end a compromised session while the workload continues operating. Platform token revocation places the token's `jti` on the blocklist: that specific session is dead instantly, the workload continues running and can be issued a new session under a new token. In a platform managing many agent sessions across many tenants, session-level revocation granularity is not optional.

**3. Policies stay stable when infrastructure changes.**

Policy rules that reference SPIFFE URIs — `spiffe://orbit.io/ns/production/sa/agent-abc` — are coupled to Kubernetes namespace topology and SPIRE trust domain configuration. Rename a namespace, change an attestation method, migrate SPIRE trust domains, move a workload between clusters: policies break. The platform token is a stable abstraction grounded in the platform data model (`tenant_id`, `principal_id`, `scopes`) rather than the deployment topology. One public key verifies it anywhere, with no SPIRE infrastructure dependency. The exchange is the boundary that insulates the application and policy layers from infrastructure churn.

The one-line version: **SVIDs prove you are a workload. Platform tokens prove you are a principal with authority. Those are different questions, and conflating them couples your policy to your infrastructure.**

### Problem Statement

We need to define:

1. The Envoy filter mechanism used to intercept outbound requests and inject the platform token
2. How the exchange call to the identity service is made from the sidecar layer, including mTLS authentication
3. How the platform token is cached across concurrent outbound requests
4. How SVID rotation events trigger proactive token re-exchange
5. Where the implementation binary lives in the Orbit monorepo and how it is deployed

### Goals

- **Zero agent workload involvement**: Agent workloads — including third-party agents, MCP servers, and open-source frameworks — make plain HTTP calls with no identity-related code
- **Single shared token cache**: All concurrent outbound requests from a pod share one cached platform token — no redundant exchange calls per request or per Envoy worker thread
- **Proactive rotation**: SVID rotation and platform token expiry trigger proactive re-exchange, not reactive failure-and-retry
- **Go native**: The implementation is standard Go — full stdlib, real goroutines, `go test`, and integration with Orbit's existing logging, config, and OpenTelemetry infrastructure
- **Orbit-owned**: The sidecar binary lives in the Orbit monorepo, is built and deployed by the Orbit platform team, and is injected into agent pods without any workload author involvement

### Non-Goals

- Replacing the SPIRE CSI driver's role in mounting the Workload API socket into pods — the CSI driver (node-level DaemonSet) continues to serve as the mechanism by which `istio-proxy` and identity-proxy access SVIDs
- Implementing in-application policy enforcement inside the agent workload (that is Layer 2 per ADR-0011)
- Handling the delegation token (`X-Orbit-Delegation-Token`) — delegation is an application-level concern per ADR-0010; the sidecar only handles the workload identity token
- Defining the ext_authz OPA policy evaluation path (separate from token injection)

---

## Decision Drivers

* **Go native**: The implementation must be standard Go — not TinyGo, not a restricted embedded environment — so it can share Orbit's existing patterns, models, logging, and observability infrastructure
* **Shared token state**: A single platform token must be shared across all concurrent outbound requests from a pod, not independently managed per Envoy worker thread
* **Proactive SVID rotation handling**: The mechanism must be able to react to SVID rotation events rather than relying solely on TTL-based reactive refresh
* **Testability**: The implementation must be testable with `go test` and Orbit's standard test patterns
* **Operational transparency**: The sidecar must emit the same structured logs, traces, and metrics as all other Orbit Go services
* **Zero workload modification**: The mechanism must work for any workload — owned, third-party, MCP server — without code changes or configuration in the workload container

---

## Considered Options

### Option A: Lua Filter

**Description:** An inline Lua script attached to Envoy's outbound HTTP listener. The script intercepts each outbound request, checks an in-memory token cache, calls the exchange endpoint via an Envoy async subrequest if the cache is empty or stale, and injects the `Authorization` header before forwarding.

**Pros:**
- No extra container per pod
- Envoy-native: no additional process, no Unix socket round-trip
- Istio `EnvoyFilter` CRD handles distribution

**Cons:**
- Lua state is per-Envoy-worker-thread: no shared cache across threads. Each worker independently exchanges for a token on startup, resulting in N parallel exchange calls rather than one
- No goroutines: proactive background refresh is not possible. Refresh is reactive, triggered by the next request after a token approaches expiry
- Subrequests in Lua are callback-based and asynchronous — not idiomatic and hard to reason about
- No integration with Orbit's logging, tracing, or metrics infrastructure
- Not `go test`-able; debugging requires Envoy-specific tooling

### Option B: WASM Plugin

**Description:** A WebAssembly module distributed via Istio's `WasmPlugin` CRD. Compiled from TinyGo, it intercepts outbound requests and performs the same token caching and injection logic as the Lua filter, but in a compiled module with better performance characteristics.

**Pros:**
- No extra container per pod
- Istio `WasmPlugin` CRD provides automatic OCI-based distribution to all matching sidecars
- Better performance than Lua (compiled, not interpreted)
- Superficially familiar syntax for Go engineers (TinyGo)

**Cons:**
- **TinyGo is not Go**: no goroutines, no full standard library, no `net/http`, no `sync.RWMutex` across VMs. The proxy-wasm host API imposes a callback-driven, event-loop programming model that is fundamentally different from idiomatic Go
- WASM VMs in Envoy are per-worker-thread isolated: the shared-state problem from Option A is unchanged. Each worker thread runs its own WASM VM with its own independent token cache
- `DispatchHttpCall()` for the exchange subrequest is async and callback-based — cannot be made into a clean blocking call
- Cannot use `go test`: requires a proxy-wasm host environment for testing
- Cannot share Orbit's logging, OTel, or config packages (TinyGo stdlib restrictions prevent it)
- The resulting code looks like Go but is not Go — it would be unfamiliar and difficult to maintain for engineers working in the rest of the Orbit codebase

### Option C: External Processing (`ext_proc`) Go Binary

**Description:** A standard Go binary implementing Envoy's External Processing gRPC protocol (`envoy.service.ext_proc.v3`). It runs as a dedicated container in the agent pod, communicates with Envoy via a Unix domain socket (local gRPC — sub-millisecond round-trip), and handles token caching, exchange, and injection entirely within a single Go process.

The binary connects to the SPIRE Workload API socket (shared mount within the pod) using `go-spiffe`'s `workloadapi.WatchX509Context()`. This provides a live SVID rotation callback — whenever the SPIRE Agent delivers a new SVID, identity-proxy is notified and re-exchanges immediately. The exchange call to the identity service is made via a typed platform HTTP client (`internal/platform/clients/identity/`) using plain HTTP — Istio's Envoy sidecar transparently upgrades the outbound connection to mTLS using the pod's SPIRE SVID. Performing manual mTLS in-process would produce a double-TLS situation where Envoy sees an opaque TCP stream and never injects the XFCC header that the identity service reads to establish the caller's principal (see ADR-0020 §Service-to-Service Communication).

Token state is held in a single shared struct behind a `sync.RWMutex`. A background goroutine monitors expiry and re-exchanges proactively. SVID rotation callbacks trigger immediate re-exchange. All concurrent outbound requests from the pod read from the same cache.

**Pros:**
- Standard Go: full stdlib, real goroutines, `net/http`, `sync`, `go test`
- Single shared token cache across all Envoy worker threads — one exchange call on startup, not N
- Proactive refresh via a real background goroutine, not reactive per-request retry
- SVID rotation handled via `go-spiffe` callback — re-exchange happens immediately when the SVID rotates, not on the next expiry cycle
- Full integration with Orbit's logging, OTel tracing, metrics, and config packages
- Binary lives in `backend/cmd/identity-proxy/` — same monorepo, same build pipeline, same patterns as all other Orbit services
- Exchange HTTP call made via a typed platform client (`internal/platform/clients/identity/`) per ADR-0020 — wire types owned by the client, not imported from the identity service package; Istio handles mTLS transparently
- Testable with `go test` — the gRPC server, cache logic, and refresh loop are all standard Go constructs
- Per-request overhead is a Unix domain socket gRPC call — ~50–100µs, negligible for the agent workload profile (LLM calls, tool APIs, platform APIs all operate in the millisecond-to-second range)

**Cons:**
- One additional container per agent pod
- Slight per-request overhead (Unix socket round-trip) vs. pure in-process filter options

---

## Decision

**Chosen Option:** Option C — External Processing (`ext_proc`) Go Binary

### Rationale

Option A (Lua) and Option B (WASM) are rejected on the same fundamental grounds: neither provides shared state across Envoy worker threads, and neither supports a proactive background refresh model. The Lua/WASM per-worker isolation problem means that under any meaningful concurrency, multiple independent exchange calls are made on startup — inelegant and wasteful. More importantly, neither option can use standard Go. TinyGo's restrictions mean WASM cannot use goroutines, full stdlib, or Orbit's existing packages. The result would be a maintenance island — code that looks like Go but cannot be developed, tested, or observed like the rest of the Orbit codebase.

Option C is selected because it is genuinely Go native. The ext_proc binary is a standard Go service — same build toolchain, same test patterns, same logging and observability infrastructure, same ability to import from `internal/`. It solves the shared-state problem by construction: one process, one cache, one background refresh goroutine, no per-worker isolation. The per-request Unix socket overhead is real but irrelevant for the agent workload profile.

The extra container per pod is the honest cost of this approach and is accepted. It is the correct architectural expression of what the identity proxy is: a platform-owned process doing platform work, running alongside the workload, not embedded inside the proxy configuration.

---

## Architecture Detail

### Component Roles in the Agent Pod

```
┌─────────────────────────────────────────────────────────┐
│                      Agent Pod                          │
│                                                         │
│  ┌──────────────────────────────────────────────────┐   │
│  │  spire-workload-socket (CSI volume mount)        │   │
│  │  /run/secrets/workload-spiffe-uds/socket         │   │
│  │  Provided by SPIRE CSI driver (node DaemonSet)   │   │
│  └──────┬────────────────────────┬───────────────────┘  │
│         │ Workload API socket    │ Workload API socket   │
│         ▼                        ▼                       │
│  ┌─────────────┐        ┌────────────────┐               │
│  │istio-proxy  │        │ identity-proxy │               │
│  │  (Envoy)    │        │  (this binary) │               │
│  │             │◄───────│  ext_proc gRPC │               │
│  │  mTLS via   │ header │  (Unix socket) │               │
│  │  SPIFFE     │ inject │                │               │
│  │  socket     │        └───────┬────────┘               │
│  └──────┬──────┘                │                        │
│         │                       │ mTLS (go-spiffe)       │
│         │                       │ POST /v1/identity/     │
│         │                       │       exchange         │
│         │                       ▼                        │
│         │          [identity service — cluster network]  │
│         │                                                │
│  ┌──────┴──────────────────┐                             │
│  │     Agent Workload      │── plain HTTP ──► Envoy ──►  │
│  │  (third-party, MCP,     │                  ▲          │
│  │   framework...)         │         injects  │          │
│  └─────────────────────────┘    Authorization: Bearer    │
└─────────────────────────────────────────────────────────┘
```

**SPIRE CSI driver** (node-level DaemonSet, not a pod container) mounts the SPIRE Agent's Workload API socket into each pod as a read-only volume at `/run/secrets/workload-spiffe-uds/socket`. This is the mechanism by which both `istio-proxy` and `identity-proxy` access SPIFFE SVIDs — there is no SPIFFE Helper sidecar container in any pod.

**`istio-proxy`** (Envoy, injected by Istio) reads the mounted Workload API socket directly via `SPIFFE_ENDPOINT_SOCKET: unix:///run/secrets/workload-spiffe-uds/socket`. It uses the SVID for mTLS on inbound and outbound transport connections. Istio has native SPIRE/SPIFFE integration — no intermediary SDS sidecar is required.

**identity-proxy** also connects to the same mounted Workload API socket independently using `go-spiffe`'s `workloadapi.NewX509Source()`. It uses the SVID to build an mTLS `http.Client` for the exchange call. Both `istio-proxy` and `identity-proxy` consuming from the same socket simultaneously is explicitly supported by the SPIFFE Workload API protocol.

**Envoy** is configured with an `ext_proc` filter on its outbound HTTP listener, pointing at the identity-proxy's Unix socket. Every outbound HTTP request from the agent workload is sent to identity-proxy for header injection before being forwarded.

### identity-proxy Internal Architecture

```
identity-proxy binary
│
├── workloadapi.WatchX509Context (go-spiffe)
│     Watches SPIRE Workload API socket for SVID rotation events.
│     Triggers OnX509ContextUpdate callback on each new SVID delivery.
│
├── identityclient.Client  (internal/platform/clients/identity/)
│     Typed platform HTTP client for POST /api/v1/identity/exchange.
│     Uses plain HTTP — Istio's Envoy sidecar transparently upgrades the
│     outbound connection to mTLS using the pod's SPIRE SVID. Manual mTLS
│     is intentionally avoided: doing so would produce a double-TLS stream
│     that Envoy cannot inspect, blocking XFCC header injection (ADR-0020).
│
├── TokenCache
│     sync.RWMutex-protected struct: { token string, expiry time.Time }
│     Single instance shared across all goroutines and all Envoy workers.
│
├── Background refresh goroutine
│     Monitors cache expiry. Re-exchanges at expiry − 60 seconds.
│     Independent of request traffic.
│
├── SVID rotation callback (registered on X509Source)
│     On new SVID: immediately invalidates cache, triggers re-exchange.
│     Ensures token is always bound to the current SVID before it is injected.
│
└── ext_proc gRPC server
      Listens on Unix domain socket (e.g. /run/orbit/identity-proxy.sock).
      Implements envoy.service.ext_proc.v3.ExternalProcessor.
      On ProcessingRequest (request_headers phase):
        1. Acquire RLock on TokenCache.
        2. Read cached token (zero-copy string read — nanoseconds).
        3. Release RLock.
        4. Return HeaderMutation: add Authorization: Bearer <token>.
      No exchange logic in the hot path. Exchange is entirely background-driven.
```

### Exchange Call Flow (Startup)

```
1. identity-proxy starts.
2. Connects to SPIRE Workload API socket via go-spiffe.
   Receives current SVID (cert + key) as X509Source.
3. Starts HTTP health server on port 9090 immediately.
   /healthz always returns 200 — it is intentionally NOT gated on token cache
   state. If /healthz returned 503 until a token was cached, Istio's PostStart
   hook would block, Envoy's iptables intercept would never be configured, and
   the exchange call (step 6) would deadlock (connection refused).
4. Binds the Unix domain socket and starts the gRPC ext_proc server in the
   background. The gRPC health service starts as NOT_SERVING — Envoy will not
   send ext_proc traffic until it transitions to SERVING (step 8).
5. Waits for Envoy to be xDS-ready by polling localhost:15021/healthz/ready.
   This ensures iptables intercept is active before any outbound call is made.
6. Performs initial exchange:
   POST {ORBIT_IDENTITY_SERVICE_URL}/api/v1/identity/exchange
   where ORBIT_IDENTITY_SERVICE_URL is the scheme+host only, e.g.
   "http://orbit-identity.orbit-system.svc.cluster.local".
   The "/api/v1/identity/exchange" suffix is appended by Exchange() —
   do NOT include it in the env var.
   mTLS: Envoy intercepts the outbound HTTP call and presents the pod's SPIRE
   SVID as the mTLS client certificate automatically.
   No request body — the mTLS peer certificate (SVID) is the sole auth signal.
   The identity service reads the XFCC header injected by its Envoy sidecar.
7. Identity service validates mTLS peer cert (SVID), looks up principal, issues platform JWT.
   If the exchange call fails (identity service unavailable, etc.):
   Retry with exponential backoff (initial 500ms, max 30s).
   If no token is obtained within the pod's startup probe timeout, crash — let
   the pod restart policy handle recovery.
8. identity-proxy caches (token, expiry).
   gRPC health service transitions to SERVING — Envoy ext_proc traffic now flows.
9. Starts SVID rotation watcher (WatchX509Context callback on X509Source).
   On new SVID: immediately invalidates cache and re-exchanges.
10. Starts background refresh goroutine (monitors expiry − 60s threshold).
```

From this point, every outbound request from the agent workload is handled in ~100µs: Envoy sends request headers to the Unix socket, identity-proxy reads the cached token under RLock, returns the Authorization header mutation. No network calls in the hot path.

### Token TTL Alignment

Per ADR-0010, the identity service issues platform tokens with TTL = `min(configured platform TTL, remaining SVID validity)`. For a 15-minute SVID TTL, the platform token is also at most 15 minutes. The SPIRE Agent rotates SVIDs at approximately 50% of TTL (7.5 minutes in) — go-spiffe's `X509Source` delivers the new SVID via its `OnX509ContextUpdate` callback automatically. identity-proxy's rotation callback fires as soon as go-spiffe receives the new SVID — well before the old platform token expires. Re-exchange with the new SVID produces a fresh token bound to the new SVID. The old token's JTI expires naturally.

The background refresh goroutine at `expiry − 60s` acts as a safety net for any edge cases where SVID rotation and token expiry do not align (e.g. operator-configured short platform token TTLs independent of SVID TTL).

### Identity Service Endpoint — Gateway Exemption

The identity exchange endpoint (`POST /api/v1/identity/exchange`) is gateway-exempt: it must be reachable without an existing platform token. The mTLS peer certificate IS the authentication. The identity service's Envoy sidecar must be configured to permit requests to this path without an `Authorization: Bearer` header — the XFCC header injected by Envoy from the validated mTLS peer cert is sufficient for the identity service to perform the lookup and issue the token.

All other Orbit platform API paths enforce the presence of a valid platform token at Layer 1 per ADR-0011.

### Deployment — Binary Location and Container Injection

The binary is located at `backend/cmd/identity-proxy/` — a standard entrypoint under the Orbit backend monorepo (ADR-0020). It is built as a separate Docker image and deployed as an additional container in all agent pod specifications.

Agent workload authors do not configure this container. The reference pod spec (maintained by the platform team) includes:

```yaml
containers:
  - name: identity-proxy
    image: orbit/identity-proxy:<version>
    env:
      - name: ORBIT_IDENTITY_SERVICE_URL
        value: http://orbit-identity.orbit-system.svc.cluster.local
      - name: SPIFFE_ENDPOINT_SOCKET
        value: unix:///run/secrets/workload-spiffe-uds/socket
    volumeMounts:
      - name: spire-workload-socket   # CSI driver volume — same mount used by istio-proxy
        mountPath: /run/secrets/workload-spiffe-uds
        readOnly: true
      - name: identity-proxy-socket
        mountPath: /run/orbit
```

Envoy is configured (via Istio `EnvoyFilter` CRD) with an `ext_proc` filter on its outbound HTTP listener pointing at `/run/orbit/identity-proxy.sock`. The filter is applied to all outbound requests from the pod.

Future: sidecar injection can be automated via a Kubernetes `MutatingAdmissionWebhook` that injects the identity-proxy container and the Envoy filter config into pods carrying a well-known label (e.g. `orbit.io/identity-proxy: enabled`).

---

## Consequences

### Positive

- Third-party agents, MCP servers, and open-source frameworks are fully supported with zero modification — they make plain HTTP calls and the sidecar layer handles everything
- Single shared token cache across all Envoy workers eliminates redundant exchange calls on startup and under concurrency
- SVID rotation is handled proactively via go-spiffe's callback API — the token is always bound to the current SVID with no gap window
- The implementation is standard Go: full stdlib, real goroutines, `go test`, Orbit's logging and OTel packages, platform-layer service clients per ADR-0020
- Operational observability is identical to all other Orbit services — structured logs, distributed traces, Prometheus metrics
- The binary lives in the Orbit monorepo under `backend/cmd/identity-proxy/` and is owned, built, and versioned by the platform team

### Negative

- One additional container per agent pod increases pod resource footprint (expected: small — the identity-proxy is idle on the hot path, consuming no CPU per request)
- Unix domain socket round-trip per outbound request: ~50–100µs overhead. Negligible for the agent workload profile but real
- The SPIRE CSI driver volume (`spire-workload-socket`) must be mounted into the identity-proxy container in addition to `istio-proxy` — two consumers of the same socket, which is supported by the Workload API protocol and requires correct pod spec configuration

### Neutral

- The SPIRE CSI driver (node-level DaemonSet) continues to serve its existing role unchanged — mounting the Workload API socket into pods. identity-proxy adds a second independent Workload API consumer alongside `istio-proxy` — this is additive, not a change to the existing node infrastructure
- The `ORBIT_IDENTITY_SERVICE_URL` environment variable becomes a platform convention for all agent pod specs — injected by the reference pod template or mutating webhook

---

## Implementation Notes

### Prerequisites — go.mod Updates

Neither `go-spiffe/v2` nor `go-control-plane/envoy` are currently in the Orbit go.mod. Both must be added before any code in `internal/identityproxy/` is written:

```bash
go get github.com/spiffe/go-spiffe/v2@v2.6.0
go get github.com/envoyproxy/go-control-plane/envoy@v1.37.0
```

The `/envoy` sub-module at v1.37.0 pins to grpc v1.78.0 which matches the existing go.mod — no grpc version conflict.

### Key Dependencies

- `github.com/spiffe/go-spiffe/v2 v2.6.0` — Workload API client, X509Source, SVID rotation callbacks, mTLS TLS config
- `google.golang.org/grpc` — ext_proc gRPC server (already used in the Orbit backend)
- `github.com/envoyproxy/go-control-plane/envoy v1.37.0` — generated proto types for `envoy.service.ext_proc.v3`; use the `/envoy` sub-module at v1.37.0 which pins to grpc v1.78.0 matching the existing go.mod
- Orbit's `internal/platform/logging`, `internal/platform/observability` — logging and OTel integration (same as all other Orbit services)

### Package Structure

```
backend/cmd/identity-proxy/
├── main.go                          # Wiring: config, identity client, TokenCache, gRPC server
internal/identityproxy/
├── cache.go                         # TokenCache: RWMutex, get/set, expiry check
├── config.go                        # Runtime tunables (timeouts, backoff, socket path)
├── refresh.go                       # Background goroutine: expiry monitor + SVID rotation callback
├── server.go                        # ext_proc gRPC server: ProcessingRequest handler
└── identityproxy_test.go            # go test: cache, refresh, and gRPC handler unit tests
internal/platform/clients/identity/
└── client.go                        # Typed HTTP client for POST /api/v1/identity/exchange
                                     # ADR-0020 compliant: wire types owned by the client,
                                     # not imported from internal/identity/; Istio handles mTLS
```

### Envoy Filter Configuration (via Istio EnvoyFilter CRD)

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: identity-proxy-ext-proc
spec:
  workloadSelector:
    labels:
      orbit.io/identity-proxy: enabled
  configPatches:
    - applyTo: HTTP_FILTER
      match:
        context: SIDECAR_OUTBOUND
        listener:
          filterChain:
            filter:
              name: envoy.filters.network.http_connection_manager
      patch:
        operation: INSERT_BEFORE
        value:
          name: envoy.filters.http.ext_proc
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
            grpc_service:
              envoy_grpc:
                cluster_name: identity_proxy_local
            processing_mode:
              request_header_mode: SEND
              response_header_mode: SKIP
              request_body_mode: NONE
              response_body_mode: NONE
    - applyTo: CLUSTER
      match:
        context: SIDECAR_OUTBOUND
      patch:
        operation: ADD
        value:
          name: identity_proxy_local
          type: STATIC
          connect_timeout: 1s
          load_assignment:
            cluster_name: identity_proxy_local
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        pipe:
                          path: /run/orbit/identity-proxy.sock
          http2_protocol_options: {}
```

The filter references `cluster_name: identity_proxy_local`. The second `configPatches` entry above defines that cluster as a static STATIC cluster resolving to the Unix domain socket at `/run/orbit/identity-proxy.sock`. Both patches must be deployed together — the filter patch alone is not deployable.

### Migration Path

No migration is required. This ADR establishes a new component for a flow that has not yet been implemented. Existing deployments are unaffected. The identity-proxy container is additive to the agent pod spec.

---

## Validation

### Success Metrics

- Single exchange call made per pod on startup regardless of the number of concurrent outbound requests — verified by identity service request logs
- SVID rotation triggers re-exchange within one go-spiffe rotation callback cycle — verified by integration tests that cycle the SVID TTL
- Per-request token injection overhead (ext_proc round-trip) is ≤ 1ms at p99 — verified by load testing the ext_proc gRPC path on the Unix socket
- Zero exchange calls or token logic visible in agent workload container — verified by code review of reference pod spec and integration tests
- Platform token injected correctly on 100% of outbound requests from agent workload containers — verified by end-to-end integration tests

### Monitoring

- `identity_proxy_token_cache_hits_total` — counter: requests served from cache (expected: nearly all)
- `identity_proxy_exchange_calls_total` — counter: calls to `/v1/identity/exchange` (expected: one on startup, then one per SVID rotation)
- `identity_proxy_exchange_duration_seconds` — histogram: latency of exchange call
- `identity_proxy_token_expiry_seconds` — gauge: seconds until current cached token expires
- `identity_proxy_svid_rotations_total` — counter: SVID rotation events received from go-spiffe
- `identity_proxy_ext_proc_request_duration_seconds` — histogram: per-request ext_proc gRPC handler latency (the ~50–100µs path)

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — Specifies that the Envoy sidecar performs the SVID-to-platform-token exchange; this ADR defines the concrete mechanism
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Establishes Envoy as the unified data plane; defers the specific filter configuration resolved here
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Istio as the sidecar control plane; the `EnvoyFilter` and `WasmPlugin` CRDs are the distribution mechanism for the ext_proc filter config
- [ADR-0020](0020-multi-service-backend-deployment-structure.md) — Multi-binary monorepo structure; `backend/cmd/identity-proxy/` follows the established pattern

---

## References

- [Envoy ext_proc filter documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
- [Envoy ext_proc proto API](https://github.com/envoyproxy/envoy/blob/main/api/envoy/service/ext_proc/v3/external_processor.proto)
- [go-spiffe Workload API client](https://github.com/spiffe/go-spiffe/tree/main/v2/workloadapi)
- [go-spiffe X509Source](https://pkg.go.dev/github.com/spiffe/go-spiffe/v2/workloadapi#X509Source)
- [go-control-plane ext_proc types](https://github.com/envoyproxy/go-control-plane/tree/main/envoy/service/ext_proc/v3)

---

## Notes

The identity-proxy binary is a **platform sidecar** — it is owned, built, versioned, and injected by the Orbit platform team. Agent workload authors have no visibility into or responsibility for its operation. This is the correct architectural boundary: authentication infrastructure is platform infrastructure, not application infrastructure.

The choice to have identity-proxy connect to the SPIRE Workload API socket directly via go-spiffe (rather than any intermediary) is intentional. The Workload API is a gRPC streaming protocol with first-class rotation callback support, designed exactly for use cases like this. Using go-spiffe's `X509Source` gives identity-proxy the cleanest possible SVID rotation integration with the least operational coupling — it reads from the same CSI-mounted socket as `istio-proxy`, with no intermediary process in the path.

**Last Updated:** 2026-03-03

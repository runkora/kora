# North-South Ingress and External Service Exposure

<!-- Filename: 2026-07-04-north-south-ingress-and-external-service-exposure.md -->

**Status:** Accepted
**Date:** 2026-07-04
**Deciders:** Platform Engineering, Justin Wilkin
**Technical Story:** Exposing the MCP tool-gateway and the identity OAuth surface publicly surfaced that north-south ingress was built ad-hoc for a single service (the inference gateway). This ADR formalises how *any* backend service is exposed externally.

---

## Context

### Background

North-south (external → cluster) ingress terminates at the Istio IngressGateway. One `Gateway` resource (`orbit-gateway`, orbit-system) fronts it, and individual services declare `VirtualService`s that bind it. [gateway-level-opa-enforcement-for-external-traffic](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md) placed an OPA `ext_authz` at the IngressGateway so every north-south request passes a policy enforcement point, reusing the platform OPA bundle and route→scope table.

The routing layer, however, grew ad-hoc around the first external service (the inference gateway):

```
orbit-gateway (hosts: orbit.<domain>, api.<domain>)
  ├── orbit.<domain>  → frontend VS  (catch-all)   ── EXCEPT /api/v1/webhooks/ → identity (path carve-out)
  └── api.<domain>    → inference VS (catch-all — the whole subdomain IS inference)
```

Two symptoms show the model does not scale:

1. **`api.` is a single-service catch-all.** There is no room for another backend API without either a new subdomain or bolting path-matches onto the inference VS.
2. **The webhook carve-out.** The Triggers feature needed a backend route (`/api/v1/webhooks/` → identity) exposed, and the only place to put it was a path-match spliced into the *frontend* VS. That is the "just bolt a path onto an existing VS" antipattern, and it already happened once.

Meanwhile the platform now has several backend surfaces that need external exposure: the inference gateway (done), the **MCP tool-gateway** (`/v1/mcp` + RFC 9728 protected-resource metadata), and the **identity OAuth surface** (RFC 8414 AS metadata, `/oauth/token`, `/oauth/register`, JWKS, the device flow). Each currently has no defined path to the outside.

### Problem Statement

There is no consistent, repeatable convention for exposing a backend service north-south. Each exposure has been bespoke — a new subdomain or a hand-spliced path — and the enforcement interaction (edge OPA vs a service that issues its own auth challenge) is undefined. We need one pattern that any current or future backend service follows, with a documented onboarding recipe.

### Goals

- One repeatable convention for exposing any backend service externally — no bespoke per-service ingress design.
- Adding an external service is a small, mechanical change: a route on a shared API VirtualService, an enforcement posture, and a netpol line — no new subdomain or TLS SAN.
- A defined interaction between the edge OPA and services that perform their own authentication/authorization (e.g. the MCP gateway's `401 + WWW-Authenticate` challenge).
- Close the deferred trusted-host allowlist for host-derived discovery metadata.
- Preserve every property of [gateway-level-opa-enforcement-for-external-traffic](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md).

### Non-Goals

- Rate-limiting / DCR abuse controls on the now-public `/oauth/register` — deferred (tracked in the MCP OAuth ADR).
- The full audit event pipeline for gateway decisions.
- East-west (BFF→service, agent→service) enforcement — unchanged.
- Multi-cluster / multi-region ingress topology.

---

## Decision Drivers

* **Repeatability** — a new external service must not require inventing ingress design each time.
* **Blast radius & operational cost** — subdomain/TLS-SAN/DNS sprawl is real operational cost per environment.
* **Security** — host-derived discovery metadata must not be poisonable; the edge remains a policy enforcement point.
* **Minimal code churn** — the MCP OAuth flow was built assuming the gateway and identity share an origin; the model should preserve that.
* **Consistency with the existing edge-OPA infrastructure** — reuse route→scope and the bypass mechanism, don't replace them.

---

## Considered Options

### Option A: Single origin, everything path-routed

One host (`orbit.<domain>`) serves the frontend *and* every backend API by path prefix.

**Pros:** zero metadata code change (everything is same-origin); one cert.
**Cons:** merges the browser app and the API surface onto one origin; retires the existing app/API separation the platform already started with `api.`; a bug in one API's routing shares the browser origin.

### Option B: Subdomain per service

`orbit.` (frontend), `api.` (inference), `mcp.` (tool-gateway), `auth.` (identity) …

**Pros:** strongest isolation; per-service VS is trivial.
**Cons:** subdomain, DNS, and TLS-SAN sprawl per environment; the MCP discovery metadata spans origins (gateway on `mcp.` → auth server on `auth.` → consent on `orbit.`), forcing the AS/PRM host to become configurable cross-origin — more code, more moving parts.

### Option C: App host + unified API host (chosen)

`orbit.<domain>` serves **only** the frontend/BFF (the browser origin, including the MCP consent page). `api.<domain>` is the **single, unified backend API host**, path-routed internally to whichever binary owns each route.

**Pros:**
- Keeps the app/API separation the platform already began; one API cert, one edge-OPA surface, one netpol pattern.
- Onboarding a service = add a path prefix to one VS. No new subdomain or SAN.
- The MCP gateway and identity stay **same-origin** under `api.`, so the protected-resource → authorization-server derivation needs no change; only the consent-page URL (on the app host) becomes an explicit derivation.

**Cons:**
- The `/v1` namespace is shared across inference and the tool-gateway, so path-prefix ownership must be coordinated (declarative, low cost).
- One VirtualService owns routing for multiple services — a platform concern, not a per-service one.

---

## Decision

Adopt **Option C**. Formalise north-south exposure as:

### 1. Host model

| Host | Serves | Origin role |
|------|--------|-------------|
| `<domain>` (no prefix, e.g. `orbit.<domain>`) | **Frontend / BFF only** | Browser origin (iron-session cookies, the MCP consent page) |
| `api.<domain>` | **All backend APIs**, path-routed | Machine/API origin (bearer tokens, no cookies) |

No other subdomains. A new backend service is exposed by adding a path route on `api.`, never a new host.

### 2. Routing: one platform-owned API VirtualService

`api.<domain>` is served by a single, platform-owned `orbit-api` VirtualService with ordered path-prefix routes (most-specific first). Services **declare their prefix**; the platform **owns the Gateway and the API VS**. Initial map:

```
api.<domain>
  /.well-known/oauth-protected-resource                → orbit-tool-gateway   (see §6)
  /.well-known/oauth-authorization-server              → orbit-identity       (see §6)
  /v1/mcp                                               → orbit-tool-gateway
  /api/v1/identity/oauth/,
    /api/v1/identity/device, /api/v1/identity/session,
    /api/v1/identity/exchange, /api/v1/identity/.well-known/jwks.json,
    /api/v1/webhooks/                                   → orbit-identity
  /v1/ (chat, models, completions, …)                  → orbit-inference-gateway
```

The `/api/v1/webhooks/` route moves here from the frontend VS; the frontend VS returns to a pure catch-all. Routes that are BFF-mediated (PAT management, user lookup, the session endpoints the BFF calls east-west) are **not** exposed on `api.` — only genuinely external callers' routes are. The root `/.well-known/*` discovery namespace is owned by the edge VS and routed by suffix (§6).

### 3. Enforcement: edge-enforced vs self-enforced services

The edge OPA (`authz_gateway.rego`) stays the default enforcement point, but we formalise two postures:

- **Edge-enforced** — the service relies on the edge OPA's route→scope check and performs no network-layer authz of its own (e.g. the inference gateway). Default.
- **Self-enforced** — the service runs its **own** PEP and/or authentication challenge, so the edge OPA **bypasses** its paths to avoid double-enforcement and, critically, to let it own the request/response. The **frontend** (session cookies) is already this. The **MCP tool-gateway** joins it: it validates the token (`AuthMiddleware`), issues the `401 + WWW-Authenticate` OAuth challenge for unauthenticated requests, and enforces per-tool `mcp:<server>:<tool>:use` in its PEP.

This resolves a hard interaction: with `/v1/mcp` edge-enforced (`mcp:invoke` route-scope), an unauthenticated request is denied `invalid_token` at the edge **before** the gateway can issue the discovery challenge — breaking the OAuth bootstrap. Marking the tool-gateway self-enforced (edge bypass on `/v1/mcp` and `/v1/.well-known/`) lets the challenge through; the gateway's own PEP is stricter than the coarse `mcp:invoke` gate anyway. (The east-west agent path keeps its `mcp:invoke` enforcement via the sidecar OPA — a separate policy from the gateway OPA — unchanged.)

Public/unauthenticated backend paths are declared in the edge bypass exactly as `/api/v1/webhooks/` and `/api/v1/identity/.well-known/` already are. New for this ADR: `/v1/mcp`, the root `/.well-known/` discovery namespace (§6), and `/api/v1/identity/oauth/token` + `/api/v1/identity/oauth/register`.

### 4. Host derivation and the trusted-host allowlist

Discovery metadata (PRM, AS metadata, the 401 challenge) is derived from the request host (`X-Forwarded-*`). Under this convention the relationship between the two hosts is **mechanical**: the app host is the request host with the leading `api.` label stripped (`api.orbit.<domain>` → `orbit.<domain>`). So the AS metadata's `authorization_endpoint` (the consent page, on the app host) is derived, not configured, and no host is hardcoded.

The deferred **trusted-host allowlist** lands here: `RequestBaseURL` validates the resolved host against a configured allowlist of the platform's public hosts (`<domain>` and `api.<domain>`) and rejects anything else, closing host-header injection into the derived metadata. The **IngressGateway is the trust boundary** — it terminates TLS and sets `X-Forwarded-Proto`/`X-Forwarded-Host`, and client-supplied `X-Forwarded-*` never passes through it.

### 5. Onboarding recipe (the repeatable pattern)

To expose a new backend service externally:

1. **Route** — add an ordered path-prefix route to the `orbit-api` VirtualService → the service's ClusterIP.
2. **Enforcement** — choose edge-enforced (register route→scope in the bundle) or self-enforced (declare the paths in the edge bypass; the service owns its PEP).
3. **Public paths** — add any unauthenticated paths (health, discovery, challenge) to the edge bypass.
4. **Netpol** — add the service's app port to `allow-gateway-ingress`.
5. **No new host, no new TLS SAN, no new Gateway server.**

### 6. Discovery metadata at the root (RFC 8414 / 9728)

Discovery documents are served at the **root** of the API host, not under a service basePath:

- Protected-resource metadata: `https://api.<domain>/.well-known/oauth-protected-resource` (RFC 9728)
- Authorization-server metadata: `https://api.<domain>/.well-known/oauth-authorization-server` (RFC 8414)

RFC 8615 places `.well-known/` at the root of the authority, and RFC 8414/9728 construct the metadata URL as `<host>/.well-known/<suffix>` (a resource/issuer path is *appended after* the suffix, e.g. `/.well-known/oauth-protected-resource/v1/mcp`) — **not** `<path>/.well-known/<suffix>` (that is the OpenID Connect Discovery variant). This matters because a spec-compliant client **constructs** the AS-metadata URL from the issuer per RFC 8414; serving it under a basePath (`/api/v1/identity/.well-known/…`) means a strict client looks at the root and 404s. The PRM location is more forgiving (advertised verbatim in `WWW-Authenticate: resource_metadata=…`), but we serve both at the root for consistency.

Consequences of root discovery:
- The **AS issuer is the API-host root** (`https://api.<domain>`), with **absolute** endpoint URLs in the metadata (`token_endpoint = https://api.<domain>/api/v1/identity/oauth/token`, `authorization_endpoint = https://<app-host>/mcp/authorize`, `jwks_uri = https://api.<domain>/api/v1/identity/.well-known/jwks.json`). Endpoints need not share the issuer's path; only the issuer↔metadata-location pairing is fixed.
- The `resource_metadata` challenge URL and the PRM/AS documents are generated in **root form**, still host-derived (§4).
- The edge VS owns the root `/.well-known/*` namespace and routes by suffix to the owning service. Services may keep serving internally under their basePath with an edge **rewrite** (`/.well-known/oauth-protected-resource` → `/v1/.well-known/oauth-protected-resource`), or register the root path directly — an implementation detail.
- `jwks_uri` is referenced absolutely, so JWKS stays under the identity basePath (it is not an IANA-registered well-known suffix and needs no relocation).

---

## Consequences

### Positive

- A new external service is ~4 mechanical lines (a VS route + a bypass/scope entry + a netpol port), not a bespoke ingress design.
- The MCP OAuth flow works north-south with a one-line derivation change (app host = strip `api.`), because gateway↔identity stay same-origin.
- The frontend VS stops accreting backend carve-outs.
- The trusted-host allowlist closes the host-injection vector flagged in the MCP OAuth ADR.
- The edge-enforced/self-enforced distinction generalises to tenant-deployed MCP servers that bring their own PEP.

### Negative / risks

- One VirtualService owns multi-service routing — ordering mistakes route to the wrong backend. Mitigated by most-specific-first ordering and tests.
- `/v1` is shared between inference and the tool-gateway; prefix ownership must be coordinated (a registry/table, documented in the onboarding recipe).
- Self-enforced services must genuinely enforce — a service wrongly marked self-enforced would be unprotected at the edge. The posture is an explicit, reviewed declaration.

### Implementation checklist

- [ ] Replace the inference catch-all VS with a platform-owned `orbit-api` path-routed VirtualService (inference + tool-gateway + identity).
- [ ] Move `/api/v1/webhooks/` from the frontend VS to `orbit-api`; frontend VS back to pure catch-all.
- [ ] Serve PRM + AS metadata at the root `/.well-known/*` (§6): move generation to root form, set the AS issuer to the API-host root with absolute endpoint URLs, and route/rewrite root `/.well-known/*` by suffix in the `orbit-api` VS.
- [ ] `authz_gateway.rego`: add `/v1/mcp`, `/.well-known/`, `/api/v1/identity/oauth/token`, `/api/v1/identity/oauth/register` to the bypass (tool-gateway self-enforced).
- [ ] `allow-gateway-ingress` netpol: add the tool-gateway app port (8096).
- [ ] Identity AS metadata: derive `authorization_endpoint` host by stripping the `api.` label; add the trusted-host allowlist to `RequestBaseURL` with configured public hosts.
- [ ] Gateway comment/docs: `api.` is the unified backend API host, not inference-specific.
- [ ] Onboarding recipe added to the code-organization / deployment docs.

### Deferred

- Rate-limiting / DCR abuse controls on public `/oauth/register` and `/oauth/token` (MCP OAuth ADR).
- Gateway decision audit pipeline.

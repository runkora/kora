# OIDC Provider Abstraction and Local Identity Provider

**Status:** Accepted
**Date:** 2026-03-16
**Deciders:** Engineering Team
**Technical Story:** The identity service ([unified-identity-model](2026-03-05-unified-identity-model.md)) requires a federated identity source. Before a frontend exists, the platform needs a way to authenticate human operators and test the full OIDC flow locally — without coupling the codebase to a specific cloud identity provider or requiring cloud credentials for day-to-day development.

---

## Context

[unified-identity-model](2026-03-05-unified-identity-model.md) defines kora's unified identity model. One of the two supported identity sources is `FEDERATED` — identities asserted by an external OIDC provider and mapped into a kora `Principal` at session creation time. The identity service must:

1. Initiate an OIDC authorization flow with an external provider
2. Receive and verify the provider's ID token
3. Map standard claims (`sub`, `email`, `name`, `preferred_username`) to an internal `Principal`
4. Issue a signed kora platform JWT in exchange

The platform has no frontend at this stage of development. All human operator authentication must work from a terminal.

### Problem Statement

Three problems must be solved simultaneously:

1. **Provider coupling**: If the identity service is implemented against a single provider's SDK (e.g., the Microsoft MSAL library), switching providers or adding a second provider requires modifying application code. kora must remain provider-agnostic.

2. **Headless authentication**: Without a browser-based redirect flow, human operators need a mechanism to authenticate from the CLI. The OAuth 2.0 Device Authorization Grant (RFC 8628) is the standard solution — the user is given a short code, visits a URL on any device, and approves the request. The backend polls until authorization completes.

3. **Local development independence**: Requiring a cloud OIDC provider (Microsoft Entra, Okta, etc.) for local development creates hard dependencies on external network access, cloud account provisioning, and per-developer credential management. Engineers should be able to run the full authentication flow locally with zero cloud dependencies.

### Goals

- Implement OIDC authentication against the standard `/.well-known/openid-configuration` discovery document — provider-agnostic by construction
- Support the OAuth 2.0 Device Authorization Grant (RFC 8628) for headless operator authentication
- Run a real, standards-compliant local OIDC provider via Docker Compose for development and integration testing
- Allow any real OIDC provider (Microsoft Entra, Google, Okta, etc.) to be configured via environment variables without code changes
- Cleanly disable federated auth when no provider is configured — local dev should boot without OIDC if desired

### Non-Goals

- Implementing SAML federation (future concern)
- Building a custom OIDC provider or token introspection service
- Supporting OAuth 2.0 authorization code flow (requires a frontend — deferred)
- Defining per-provider claim mapping beyond the standard OIDC `profile` and `email` scopes

---

## Decision Drivers

* **Provider agnosticism**: kora will support multiple enterprise identity providers over time. Vendor lock-in at the SDK level is unacceptable.
* **Standards compliance**: The Device Authorization Grant is an IETF standard (RFC 8628). Implementing against the standard means any compliant provider works without modification.
* **Local development independence**: Requiring cloud credentials for `make dev-up` creates friction that slows onboarding and makes the CI environment harder to reason about.
* **Real protocol testing**: A real local OIDC provider (not a mock) validates that the full token issuance, claim extraction, and session lifecycle work correctly against actual OIDC protocol responses — not hand-crafted test fixtures.
* **Fail-safe configuration**: An unconfigured OIDC provider should produce a clear, immediate error — not a silent failure or a service that starts but cannot authenticate.

---

## Considered Options

### Option 1: Vendor-specific SDK (Microsoft MSAL)

**Description:** Use the Microsoft Authentication Library (MSAL) for Go directly. Configure Entra as the canonical identity provider for local development, staging, and production.

**Pros:**
- Well-documented SDK with first-party Microsoft support
- Entra is a reasonable long-term enterprise choice
- Device code flow is natively supported in MSAL

**Cons:**
- Couples the application permanently to Microsoft's authentication infrastructure
- Local development requires an Azure AD tenant, application registration, and per-developer credential provisioning
- Adding a second provider (Google Workspace, Okta) requires application code changes
- CI pipelines need Entra service principal credentials
- Engineers without an Azure account cannot run the authentication flow locally

### Option 2: Generic OIDC library + stub in-memory token server

**Description:** Use a generic OIDC library (`go-oidc/v3`) and implement a minimal in-memory OIDC stub in Go for local development. The stub returns hardcoded tokens.

**Pros:**
- Fully offline, no external dependencies
- Fast in tests
- No Docker dependency for auth

**Cons:**
- A hand-rolled OIDC stub does not test the actual protocol exchange — it tests only the application's side of the conversation
- The stub must be kept in sync with any OIDC protocol behaviour the application relies on
- Device code polling, `authorization_pending` responses, and token endpoint error handling are not exercised by a stub
- Engineers may develop false confidence that auth "works" when it has only been tested against a non-conformant stub

### Option 3: Generic OIDC library + Dex as local identity provider

**Description:** Use `github.com/coreos/go-oidc/v3` for all OIDC operations. Run [Dex](https://dexidp.io/) (CoreOS) as a real, standards-compliant OIDC provider via Docker Compose for local development. Configure production providers via environment variables.

**Pros:**
- The application code is provider-agnostic: any OIDC-compliant provider works
- Dex is a production-grade OIDC server used by large-scale projects (Kubernetes, Argo CD, Flux)
- Dex natively supports RFC 8628 (Device Authorization Grant) — the full device code flow is testable locally without cloud credentials
- Setting `issuerUrl: ""` in local YAML disables OIDC cleanly with a typed error (`ErrOIDCNotConfigured`) — no partial-init footguns
- Switching to Entra, Okta, or any other provider requires only environment variable changes, zero code changes
- `go-oidc/v3` is maintained by the CoreOS team — the same team that maintains Dex — ensuring protocol compatibility

**Cons:**
- Adds a Docker container to the Tier 1 local dev stack
- Dex's YAML configuration has non-obvious quoting rules for bcrypt password hashes (mitigated by the `make dex-env` generator target)
- Engineers unfamiliar with OIDC concepts may initially find the multi-step device flow confusing

### Option 4: Generic OIDC library, require cloud provider even locally

**Description:** Use `go-oidc/v3` but require engineers to configure a real provider (e.g., personal Entra tenant) even for local development.

**Pros:**
- Tests against real provider behaviour
- No local mock to maintain

**Cons:**
- Every engineer needs a cloud account and registered application
- Onboarding friction is high — new engineers cannot run `make dev` and authenticate on day one
- CI pipelines require cloud credentials scoped to device flow, which is unusual and adds secret management overhead
- Network outages or provider service incidents block local development

---

## Decision

**Chosen Option:** Option 3 — Generic OIDC library (`go-oidc/v3`) + Dex as the local identity provider.

### Rationale

The provider-agnostic OIDC approach aligns directly with kora's design principle of open standards over vendor lock-in. `go-oidc/v3` consumes the standard `/.well-known/openid-configuration` discovery document; the application has no awareness of which specific provider is behind the issuer URL.

Dex is the right choice for the local mock because it is not a mock — it is a real OIDC server that speaks the full protocol. Testing the device code flow against Dex exercises `authorization_pending` polling, token endpoint exchange, ID token signature verification, and claim extraction exactly as they will behave against Entra or any other production provider.

The `make dex-env` generator target solves the bcrypt hash interpolation problem (Docker Compose v2 mangles `$2y$10$...` hashes as shell variable expansions) by writing the hash to `config/dex/.env` via `env_file:` rather than the `environment:` block. This is an implementation detail that engineers do not need to understand — the Make target handles it.

The fail-safe design (`issuerUrl: ""` → `ErrOIDCNotConfigured` → HTTP 400) means the service boots cleanly without OIDC configured, and the error is unambiguous when authentication is attempted without a provider configured.

---

## Consequences

### Positive

- Any OIDC-compliant provider works without code changes — Entra, Google Workspace, Okta, Keycloak, Auth0, and others can be configured purely via environment variables
- Local development authentication is fully self-contained — `make dev-up` + `make auth-device` produces a valid kora session token against the local Dex instance with no cloud dependencies
- The full Device Authorization Grant protocol is exercised in local integration testing — not just the application's claim parsing logic
- New engineers can authenticate on day one with `dev@orbit.local.clearroute.io` / `password` without any cloud account provisioning
- The identity service's `OIDCConfig.IssuerURL` is a single field — pointing at Entra is a one-line change in the environment

### Negative

- Dex adds a third container to the Tier 1 Docker Compose stack — `make dev-up` now starts Postgres, Redis, and Dex
- The `config/dex/.env` file must be generated before the first `make dev-up` via `make dex-env` — this is a one-time step but adds to the onboarding checklist
- Dex is an additional piece of infrastructure to keep updated; the pinned Dex image must be reviewed periodically

### Neutral

- The `ORBIT_OIDC_ISSUER_URL` environment variable overrides the YAML `issuerUrl` value at runtime — this is intentional and allows pointing at a real provider without modifying committed YAML files
- The `tenant_id` field in the device auth request payload is a kora application concept, not forwarded to the OIDC provider — it exists for future multi-tenant session scoping

---

## Implementation

### Component Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    identity.Service                          │
│                                                             │
│  InitiateDeviceAuth()  ──────────────────────────────────►  │
│                          gooidc.Provider.Endpoint()         │
│                          oauth2.DeviceAuth()                │
│                                                             │
│  PollDeviceAuth()      ──────────────────────────────────►  │
│                          oauth2.DeviceAccessToken() (poll)  │
│                          gooidc.IDTokenVerifier.Verify()    │
│                          mapClaimsToSession()               │
│                          signToken() → Orbit JWT            │
└─────────────────────────────────────────────────────────────┘
         │                              │
    [local dev]                  [production]
         │                              │
    Dex (Docker)              Entra / Google / Okta
    port 5556                 (issuer URL via env var)
```

### Configuration

**`backend/internal/identity/config/local.yaml`** — default local OIDC configuration:

```yaml
oidc:
  issuerUrl: "https://dex.orbit.local.clearroute.io:5556/dex"
  scopes:
    - openid
    - profile
    - email
```

Setting `issuerUrl: ""` disables OIDC; `InitiateDeviceAuth` and `PollDeviceAuth` return `ErrOIDCNotConfigured` (HTTP 400).

**Environment variable overrides** (take precedence over YAML):

| Variable | Purpose |
|---|---|
| `ORBIT_OIDC_ISSUER_URL` | Override issuer URL at runtime (e.g., point at real Entra without changing YAML) |
| `ORBIT_OIDC_CLIENT_ID` | OAuth2 client ID — required when OIDC is enabled |
| `ORBIT_OIDC_CLIENT_SECRET` | OAuth2 client secret — leave empty for public clients (Dex `orbit-local` is a public client) |

### Local Dex Configuration

Dex is configured at `config/dex/config.yaml` with:
- In-memory storage (no separate Dex datastore)
- One static client: `orbit-local` (public client, no secret)
- One static test user: `dev@orbit.local.clearroute.io`
- `oauth2.deviceCodeGrant: true`
- `oauth2.skipApprovalScreen: true` (no consent screen in local dev)

The test user's password hash is stored in `config/dex/.env` (gitignored) and loaded via Docker Compose `env_file:` to avoid shell variable interpolation of the `$2y$10$...` bcrypt prefix.

### Generating Local Credentials

```bash
# Generate the Dex password hash and write config/dex/.env
make dex-env                   # uses default password "password"
make dex-hash password=mypass  # uses a custom password
```

### HTTP API

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/identity/device` | Initiate Device Authorization Grant — returns `user_code` and `verification_uri` |
| `POST` | `/api/v1/identity/session` | Poll for token (`grant_type: urn:ietf:params:oauth:grant-type:device_code`) |
| `POST` | `/api/v1/identity/refresh` | Refresh an access token using a refresh token |
| `POST` | `/api/v1/identity/exchange` | Exchange a SPIFFE SVID for a kora platform token (workload identity) |
| `DELETE` | `/api/v1/identity/revoke` | Revoke a session (adds JTI to Redis blocklist) |

### Pointing at a Real Provider

To use Microsoft Entra instead of local Dex, set environment variables before `make dev`:

```bash
export ORBIT_OIDC_ISSUER_URL=https://login.microsoftonline.com/{tenant-id}/v2.0
export ORBIT_OIDC_CLIENT_ID={your-app-client-id}
export ORBIT_OIDC_CLIENT_SECRET={your-app-client-secret}
```

No code changes are required. The OIDC discovery endpoint is fetched at service startup; if discovery fails, the service refuses to start.

---

## Validation

### Success Criteria

- `make dev-up && make auth-device` produces a valid kora platform JWT signed with the local signing key against local Dex
- Setting `ORBIT_OIDC_ISSUER_URL` to a real Entra tenant and running `make auth-device` produces a valid kora platform JWT — no code changes required
- Setting `issuerUrl: ""` in `local.yaml` (and unsetting the env override) causes `POST /api/v1/identity/device` to return HTTP 400 with `{"code":"oidc_not_configured"}`
- The JTI in the issued kora JWT differs from the Dex ID token JTI — the platform issues its own token; it does not forward the provider token

### Monitoring

- Log line at `INFO` level on successful provider discovery: `OIDC provider initialized`, including `issuer` field
- Log line at `WARN` level when ephemeral signing key is generated (local-only): `Using ephemeral signing key — tokens will not survive restart`
- Log line at `ERROR` level if OIDC discovery fails at startup

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — Unified identity model defining the `Principal` abstraction and `IdentitySource`
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE workload attestation (the `POST /identity/exchange` counterpart for machine identity)
- [local-development-environment](2026-03-10-local-development-environment.md) — Local development environment (Dex is added to the Tier 1 Docker Compose stack)

---

## References

- [RFC 8628 — OAuth 2.0 Device Authorization Grant](https://datatracker.ietf.org/doc/html/rfc8628)
- [Dex Identity Provider](https://dexidp.io/)
- [github.com/coreos/go-oidc/v3](https://github.com/coreos/go-oidc)
- [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html)

---

**Last Updated:** 2026-03-10

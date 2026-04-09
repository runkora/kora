# 0026. Tenant Registration and OIDC Provider Binding

**Status:** Proposed
**Date:** 2026-06-10
**Deciders:** Engineering Team
**Technical Story:** Identity system is not tenant-bound — `tenant_id` is a free-form client-supplied string with no validation against an authoritative tenant registry, and there is no per-tenant OIDC provider configuration despite ADR-0008 requiring it.

---

## Context

ADR-0008 established the foundational identity model for Orbit. It declared, as first-class requirements:

> *"Each tenant may have its own OIDC/SAML federation configuration. Orbit supports multiple upstream IdP registrations, each scoped to a tenant."*
> *"Multi-tenancy is enforced at the identity layer — tenant isolation cannot be accidentally bypassed by omission."*

The current implementation has `tenant_id` as a field on `Principal`, `Session`, and JWT claims, and the `identity_sessions` table has a `tenant_id NOT NULL` column. However, several critical requirements from ADR-0008 are not yet implemented:

1. There is no authoritative registry of which tenants exist. Any non-empty string is accepted as a `tenant_id`.
2. There is a single global OIDC provider (`OIDCConfig.IssuerURL`) shared by all tenants. There is no per-tenant IdP routing.
3. The device auth flow (`POST /identity/device`, `POST /identity/session`) accepts `tenant_id` as a client-supplied string with no validation that the authenticated OIDC identity belongs to the claimed tenant. A successfully authenticated user can claim any tenant string and receive a legitimately-signed platform JWT scoped to that tenant.
4. There is no mechanism for the OIDC issuer claim in the inbound token to be verified against the expected issuer for the claimed tenant.

### Background

The identity gaps exist because ADR-0008 was intentionally forward-looking. It defined the target model before all the pieces were ready to implement. The SPIFFE/attested identity path was correctly prioritised first (it is cryptographically safe by design), but the federated/human identity path was left with placeholder enforcement.

### Why Tenant Config Cannot Live in the Service YAML Files

Orbit's service YAML files (`internal/identity/config/local.yaml`, `development.yaml`, `production.yaml`) are **baked into the container image at build time** via the Dockerfile `find /app -path "*/config/*.yaml"` step. This means they are build-time artifacts, not runtime configuration.

Tenant configuration is fundamentally different in nature:

- It is **deployment-time operator configuration** — it changes per deployment environment without any code or image change
- It must be **binary-independent** — the same image must be deployable for any set of tenants; the operator provides the tenant list at deploy time, not the engineer at build time
- Different deployments of the same image version (production EU, production US, staging, a customer's self-hosted install) will have entirely different tenant registries
- In Kubernetes, this class of configuration is managed via **ConfigMaps mounted as files**, injected into the container at runtime via a volume mount

The service YAML files are the right place for service tuning (token TTL, server port, observability settings). They are the wrong place for operator-managed infrastructure configuration like tenant registries.

This is consistent with how comparable platforms handle the same distinction:
- **Dex** separates its `config.yaml` (operator-mounted at runtime) from any compiled-in defaults
- **Prometheus** reads its scrape config from a file path supplied at startup (`--config.file`)
- **Vault** reads its backend config from a file path supplied at startup (`-config`)
- **Nginx** reads server block configuration from a directory mounted at runtime

### The Bootstrapping Question

Filling this gap requires answering: **how does a tenant get registered, and by whom?**

There is a real bootstrapping problem:

> To call a platform API you need a platform token. To get a platform token you need a tenant. To have a tenant, someone must have registered it.

The answer depends on the operational model of the deployment. Three patterns exist in the industry:

- **Self-service registration** — any user can create a tenant via a public API (SaaS model: Slack, Vercel)
- **Platform-admin bootstrap** — a privileged root credential exists at install time; the platform admin registers tenants via an API
- **Operator-managed, infrastructure-declared** — tenants are declared as configuration in the deployment repository, applied by whoever controls the cluster (Dex, Vault, Keycloak in operator mode)

Orbit's current deployment model is explicitly operator-managed: Helm charts are provided for others to deploy into their environments, and the platform team maintains a separate deployment repository for internal environments. This is not a SaaS product with self-service sign-up at this stage.

### Problem Statement

The identity service needs:
1. An authoritative source of which tenants exist and what OIDC provider each tenant uses
2. Validation that the `tenant_id` in a device auth request refers to a real, active tenant
3. Validation that the OIDC token presented during device auth was issued by the OIDC provider registered for that tenant
4. A model for the above that does not require the platform team to manage individual user membership
5. A mechanism that is **deployment-time injectable** — not baked into the image

### Goals

- Introduce an authoritative tenant registry that the identity service reads at runtime from an operator-mounted file
- Bind each tenant to its own OIDC issuer configuration — resolving the single-provider limitation
- Validate `tenant_id` and OIDC issuer in the device auth flow, closing the cross-tenant token issuance gap
- Keep local development zero-config — `make dev-up` + `make dev` must continue to work without extra manual setup steps
- Avoid coupling tenant membership management to the platform team — the customer's own IdP owns membership
- Provide a clear evolution path toward a Kubernetes-native operator model as the platform matures

### Non-Goals

- Self-service tenant registration via a public API (deferred)
- A platform admin UI or CLI for managing tenants at runtime
- Tenant lifecycle operations (suspend, delete) beyond the data model
- Defining per-tenant RBAC or role assignment within a tenant (separate concern)
- Building a Kubernetes operator or CRD controller in this ADR (identified as the evolution path, not the immediate implementation)

---

## Decision Drivers

* **Security**: The cross-tenant token issuance gap in the current device auth flow is a high-severity identity boundary failure that must be closed
* **ADR-0008 compliance**: Per-tenant OIDC provider support was a stated requirement of the foundational identity ADR
* **Binary independence**: Tenant config must be injectable at deployment time without rebuilding the image
* **Operational simplicity**: The platform team should not own or manage individual user membership; that must remain with the customer's IdP
* **Local dev experience**: The `make dev-up` + `make dev` flows must remain zero-config — no extra init jobs, seeds, or out-of-band setup
* **Deployment model alignment**: Orbit ships Helm charts; tenant configuration is operator-provided infrastructure config, mounted at runtime
* **Evolution path**: The solution should not close off the future operator/CRD model

---

## Considered Options

### Option 1: Database-backed tenant registry with seed migrations

**Description:** Introduce a `tenants` table in PostgreSQL. Tenant records are inserted via a seed migration. The identity service queries this table at auth time to validate `tenant_id` and look up OIDC configuration.

**Pros:**
- DB is already the source of truth for sessions and blocklist
- Supports runtime queries (e.g. list all tenants) without file parsing
- Supports future lifecycle state management natively

**Cons:**
- Requires a DB seed or init job — platform team owns and must manage that data
- Adding a new tenant requires a migration run, coupling tenant provisioning to the release cycle
- Not consistent with how comparable tools (Dex, Vault, Keycloak) manage multi-tenancy configuration
- Membership is still not solved — the DB only holds the tenant record, not who belongs to it

### Option 2: Add tenant config to the service YAML files

**Description:** Add a `tenants` block to `internal/identity/config/{env}.yaml`. The identity service reads this at startup.

**Pros:**
- Uses the existing config system — no new loading mechanism needed
- Simple — one place for all config

**Cons:**
- **Fundamentally wrong**: service YAML files are baked into the container image at build time by the Dockerfile. Tenant config would then be image-baked, not operator-injectable.
- Changing tenants requires rebuilding and redeploying the image
- The same image cannot serve different tenant configurations across deployments
- Tenant config is not service config — mixing the two categories creates an architectural confusion that compounds over time
- Violates binary-independence: `internal/identity/` is service-specific; tenant config must be deployment-specific

### Option 3: Runtime-mounted tenant config file, read via environment variable path

**Description:** Tenants are declared in a YAML file that the operator provides at deployment time. The identity service discovers the file path via an environment variable (`ORBIT_TENANTS_CONFIG`). In Kubernetes, this file is mounted from a ConfigMap (which lives in the deployment repository). In local dev, a `config/tenants.yaml` file at the project root is committed and referenced by both `docker-compose.yml` and `.env.example` — zero extra setup for developers.

This is exactly the pattern used by Dex (`dex serve /etc/dex/config.yaml`), Prometheus (`--config.file`), and Vault (`-config`).

**Pros:**
- Completely binary-independent — the same image is deployable for any tenant set
- Operator-provided at runtime — no image rebuild needed to add or change a tenant
- The deployment repository (separate from this repo) owns the tenant ConfigMap — correct ownership boundary
- Local dev zero-config — `config/tenants.yaml` committed to this repo covers all local dev flows for both `make dev` (direct binary) and Docker Compose
- OIDC issuer binds tenant to IdP — membership is fully owned by the customer's IdP
- Maps cleanly onto the future CRD model — the file schema becomes the CRD `spec`

**Cons:**
- Requires a pod restart to add or change a tenant (no hot-reload initially — can be added later)
- Operators must ensure the file is mounted — a missing mount causes a startup failure (which is the correct and safe behaviour)
- YAML parsing must route through `internal/platform/config/` to respect the `no-raw-yaml` lint rule (see Implementation)

### Option 4: Kubernetes CRD with an operator controller

**Description:** Define a `Tenant` CRD (`orbit.io/v1alpha1`). An Orbit controller reconciles CRDs into in-memory state consumed by the identity service.

**Pros:**
- Fully declarative and Kubernetes-native
- kubectl is the management interface
- Consistent with how `ClusterSPIFFEID` already works in this platform

**Cons:**
- Requires building a controller/operator — significantly more complexity than needed now
- Overkill before there are dynamic, runtime-provisioned tenants to manage
- Blocked on Option 3 anyway — the CRD model reads the same schema, backed by a different source

---

## Decision

**Chosen Option:** Option 3 — Runtime-mounted tenant config file, discovered via environment variable.

### Rationale

Option 2 (service YAML files) is ruled out on principle: service YAML files are baked into the image at build time by the Dockerfile. Tenant config is operator-provided deployment-time infrastructure — mixing these two categories is an architectural error that would compound as the platform grows.

Option 1 (DB seeds) forces the platform team to own tenant data via migration files, couples tenant provisioning to the release cycle, and is not how the industry manages this class of configuration.

Option 4 (CRD operator) is the correct long-term direction but is premature. It is blocked on having the file-based schema (Option 3) defined first — the CRD `spec` will be the file schema expressed as a Kubernetes resource.

Option 3 is the correct answer because it separates concerns cleanly:
- The **image** owns service behaviour (compiled in)
- The **service YAML files** own service tuning (baked into image, environment-specific)
- The **mounted tenant config file** owns operator infrastructure config (injected at runtime, deployment-specific)

This is the same separation made by every comparable platform tool.

#### Why OIDC Issuer as the Membership Boundary

The platform team should never own individual user membership. If a tenant has registered their Entra ID tenant as their OIDC issuer, whoever can authenticate against that Entra tenant is a member of the Orbit tenant — Orbit enforces nothing more granular than that at the identity layer. Role assignment and fine-grained authorisation are handled by the policy engine (ADR-0011).

This is correct because:
- The customer already manages their user directory in their IdP
- Duplicating membership in Orbit creates synchronisation drift
- Deprovisioning a user in Entra automatically revokes their ability to obtain a new platform token
- The OIDC `iss` claim is cryptographically bound to the issuing provider — it cannot be spoofed by the client

---

## Tenant Config File Schema

The tenant config file is a YAML file with the following structure. It is **not** part of the service YAML system — it is a separate file with its own loader (see Implementation).

```yaml
# tenants.yaml — operator-provided, mounted at runtime
# In Kubernetes: mounted from a ConfigMap + Secrets for credentials
# In local dev:  config/tenants.yaml at project root

version: "v1"

tenants:
  - id: "acme-corp"
    displayName: "Acme Corp"
    state: "active"              # active | suspended
    oidc:
      issuerURL: "https://login.microsoftonline.com/acme-tenant-id/v2.0"
      clientID: "orbit-acme"
      # clientSecret injected from the env var named by clientSecretEnv at startup
      clientSecretEnv: "ORBIT_TENANT_ACME_CORP_OIDC_CLIENT_SECRET"
      scopes:
        - openid
        - profile
        - email

  - id: "platform-team"
    displayName: "Platform Engineering"
    state: "active"
    oidc:
      issuerURL: "https://accounts.google.com"
      clientID: "orbit-platform"
      clientSecretEnv: "ORBIT_TENANT_PLATFORM_TEAM_OIDC_CLIENT_SECRET"
      scopes:
        - openid
        - profile
        - email
      # Optional: restrict membership by an OIDC claim value within a shared issuer.
      # See "claimBinding Matching Semantics" below for array vs scalar rules.
      claimBinding:
        claim: "hd"              # Google Workspace hosted domain claim (scalar)
        value: "clearroute.io"
```

### Schema Version

The file must include a top-level `version` field. The loader rejects files with a missing or unrecognised version with a clear startup error. This is low-cost now and high-value as the schema evolves — an operator mounting a stale config file gets an immediate actionable error rather than silent misbehaviour.

The current schema version is `v1`.

### File Discovery

The identity service reads the file path from the `ORBIT_TENANTS_CONFIG` environment variable at startup.

```
ORBIT_TENANTS_CONFIG=/etc/orbit/tenants.yaml
```

If the environment variable is not set, or the file at the given path does not exist, the service **fails to start** with a clear error message. Tenant configuration is required — an identity service with no tenant registry cannot safely issue tokens. This is the correct safe-fail behaviour.

### Secret Injection

Client secrets are never written to the tenant config file (or any YAML file — ADR-0006). Each tenant entry declares a `clientSecretEnv` field naming the environment variable that holds the secret. The identity service resolves this at startup:

```go
secret := os.Getenv(cfg.OIDC.ClientSecretEnv)
```

In Kubernetes, the secret environment variables are populated from Kubernetes Secrets via `envFrom` or `env[].valueFrom.secretKeyRef` in the Helm chart. The Secrets themselves live in the deployment repository.

### Tenant State

- `active` — auth requests are accepted
- `suspended` — new auth requests (device initiation, device code exchange, refresh) are rejected with `403 Forbidden`

**Existing sessions for a suspended tenant:** When a tenant is suspended, existing valid platform JWTs for that tenant remain valid until their natural expiry. The accepted design decision is to tolerate this window because:
- `accessTokenTtlMinutes` defaults to 60 minutes — the window is short
- Individual session revocation is available via the JTI blocklist (`POST /identity/revoke`) for urgent cases
- Active bulk revocation on suspend (writing all active JTIs to the blocklist) is deferred — it requires a query by tenant across the session store, which is a feature not yet justified by the threat model

If the suspension reason is a security incident, the operator should use the JTI blocklist endpoint to revoke specific known sessions. Bulk tenant revocation is identified as a future feature.

### claimBinding Matching Semantics

`claimBinding` is an optional constraint applied after successful OIDC authentication to restrict membership within a shared issuer. Matching rules:

- **Absent claim**: if the specified claim is not present in the ID token, the match **fails** and auth is rejected with `403 Forbidden`
- **Scalar claim** (JSON string, number, or boolean): the resolved claim value is compared as a string against `value`; equality is required
- **Array claim** (JSON array, e.g. `groups`): the match **succeeds if any element of the array equals the configured `value` string** — it is a "contains" check, not equality of the whole array

Examples:
- `{"hd": "clearroute.io"}` with `claimBinding: {claim: hd, value: clearroute.io}` → match (scalar equality)
- `{"groups": ["platform-team", "engineering"]}` with `claimBinding: {claim: groups, value: platform-team}` → match (array contains)
- `{"groups": ["engineering"]}` with `claimBinding: {claim: groups, value: platform-team}` → no match → `403 Forbidden`
- `{}` (groups claim absent) with any `claimBinding` on `groups` → no match → `403 Forbidden`

### Shared IdP with Multiple Tenants

For deployments where a single OIDC provider serves multiple tenants (most commonly local dev with a shared Dex instance), `claimBinding` discriminates tenants within the same issuer:

```yaml
version: "v1"
tenants:
  - id: "platform-team"
    displayName: "Platform Engineering"
    state: "active"
    oidc:
      issuerURL: "https://dex.orbit.local.clearroute.io:5556/dex"
      clientID: "orbit"
      clientSecretEnv: "ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET"
      scopes: [openid, profile, email]
      claimBinding:
        claim: "groups"
        value: "platform-team"

  - id: "product-team"
    displayName: "Product Team"
    state: "active"
    oidc:
      issuerURL: "https://dex.orbit.local.clearroute.io:5556/dex"
      clientID: "orbit"
      clientSecretEnv: "ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET"
      scopes: [openid, profile, email]
      claimBinding:
        claim: "groups"
        value: "product-team"
```

When `claimBinding` is absent, the issuer URL alone determines membership. When present, the specified claim must match the configured value after successful OIDC authentication.

---

## Removal of the Global OIDCConfig

The current `internal/identity/config.go` `OIDCConfig` struct and the associated environment variables (`ORBIT_OIDC_ISSUER_URL`, `ORBIT_OIDC_CLIENT_ID`, `ORBIT_OIDC_CLIENT_SECRET`) are **removed in the same PR as this implementation**. They are not deprecated and deferred — they are removed.

Rationale: with `ORBIT_TENANTS_CONFIG` required for startup, the global OIDC vars become dead configuration. Dead configuration is confusing to operators (which variables take effect?) and creates a correctness risk if both code paths co-exist (which provider is used?). There is no migration burden because the current single-provider config has never been used in production.

**What is removed:**
- `OIDCConfig` struct and its fields in `internal/identity/config.go`
- The `ORBIT_OIDC_ISSUER_URL` env var read and override in `LoadConfig`
- The `ORBIT_OIDC_CLIENT_ID` and `ORBIT_OIDC_CLIENT_SECRET` env var reads
- The `cfg.OIDC.IssuerURL != "" && cfg.OIDC.ClientID == ""` validation check
- The `s.oidcProvider` single global field on `Service`
- `initOIDCProvider()` — replaced by per-tenant provider init in `TenantRegistry`

**What replaces it:**
- `ORBIT_TENANTS_CONFIG` environment variable (required, fails startup if unset)
- Per-tenant `oidc.Provider` instances held in `TenantRegistry`
- `ORBIT_TENANT_{UPPER_ID}_OIDC_CLIENT_SECRET` per-tenant env vars (optional, empty for public clients)

---

## Local Development

For local dev, a `config/tenants.yaml` file is committed to this repository at the project root. It contains a single pre-configured `local` tenant pointing at the Dex container started by `make dev-up`.

```yaml
# config/tenants.yaml
# Committed to repo — safe to commit, contains no secrets.
# Referenced by .env.example and bind-mounted by Docker Compose.

version: "v1"

tenants:
  - id: "local"
    displayName: "Local Development"
    state: "active"
    oidc:
      issuerURL: "https://dex.orbit.local.clearroute.io:5556/dex"
      clientID: "orbit"
      # Public client — ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET is empty string
      clientSecretEnv: "ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET"
      scopes:
        - openid
        - profile
        - email
```

### `make dev` (non-containerised, canonical flow)

The canonical local dev workflow runs the Go binary directly on the host via `air`:

```bash
make dev-up   # starts Postgres + Redis + Dex in Docker
make dev      # starts the backend binary via air — directly on the host
```

There is no bind-mount in this path. The binary reads `ORBIT_TENANTS_CONFIG` from the environment, which is sourced from `.env`. The following entries are added to `.env.example`:

```env
# Tenant registry config file path (required).
# Points at the committed local dev config. Relative to the repo root.
ORBIT_TENANTS_CONFIG=./config/tenants.yaml

# Per-tenant OIDC client secrets. Empty = public client (Dex local dev default).
ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET=
```

`make env` (which copies `.env.example` → `.env`) picks these up automatically. Developers who have already run `make env` need to add these two variables manually once — after that, `make dev` works without any further configuration.

### Docker Compose

Docker Compose bind-mounts the same file and populates the env var:

```yaml
# docker-compose.yml (identity service)
services:
  identity:
    environment:
      ORBIT_TENANTS_CONFIG: /etc/orbit/tenants.yaml
      ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET: ""
    volumes:
      - ./config/tenants.yaml:/etc/orbit/tenants.yaml:ro
```

---

## In Kubernetes (Deployment Repository)

In the operator's deployment repository (separate from this repository), the Helm chart provides a `tenants` values block. The chart renders this into a Kubernetes ConfigMap and one or more Secrets.

```yaml
# values.yaml in deployment repo
orbit:
  tenants:
    - id: "acme-corp"
      displayName: "Acme Corp"
      state: "active"
      oidc:
        issuerURL: "https://login.microsoftonline.com/acme-tenant-id/v2.0"
        clientID: "orbit-acme"
        clientSecretEnv: "ORBIT_TENANT_ACME_CORP_OIDC_CLIENT_SECRET"
```

The Helm chart renders:
1. A `ConfigMap` from the non-secret tenant values (id, displayName, state, oidc.issuerURL, oidc.clientID, oidc.clientSecretEnv, claimBinding if present). The rendered YAML always includes `version: "v1"`.
2. The ConfigMap is mounted at `/etc/orbit/tenants.yaml` in the identity service pod
3. Client secrets are stored in Kubernetes Secrets and exposed as environment variables in the pod spec via `env[].valueFrom.secretKeyRef`

The deployment repository owns the tenant configuration. The Orbit repository owns the schema and the loader. Neither owns the other's data.

---

## Identity Service Changes

### YAML Loader — Conformance with `no-raw-yaml` Lint Rule

The `no-raw-yaml` depguard rule in `.golangci.yml` bans `gopkg.in/yaml.v2` and `gopkg.in/yaml.v3` imports everywhere outside `internal/platform/config/`. The tenant registry loader must parse a YAML file, so it cannot call `yaml.Unmarshal` directly.

**Resolution:** A new exported function `config.LoadFile[T any](path string) (*T, error)` is added to `internal/platform/config/config.go`. It reads the file at the given absolute path and unmarshals it into `T` using the existing `unmarshalExact` helper. This generalises the existing `config.Load[T]` from `APP_ENV`-derived paths to arbitrary paths, which is a capability the platform config package should have regardless.

`internal/identity/tenants.go` calls `config.LoadFile[TenantRegistryFile](path)` and never imports a YAML library directly. The identity package owns file path resolution (reading `ORBIT_TENANTS_CONFIG`); the platform package owns YAML decoding.

### At Startup

1. Read `ORBIT_TENANTS_CONFIG` environment variable — fail fast with a clear error if unset or empty
2. Call `config.LoadFile[TenantRegistryFile](path)` — fail fast if the file is missing or malformed
3. Validate `version == "v1"` — fail fast if the version is unknown
4. For each tenant, resolve `clientSecretEnv` via `os.Getenv`
5. Initialise a dedicated `oidc.Provider` per tenant via OIDC discovery (`{issuerURL}/.well-known/openid-configuration`)
6. **OIDC provider init failures are per-tenant, not service-fatal.** If one tenant's discovery endpoint is unreachable at startup, that tenant is marked `degraded` and all auth requests for it return `503 Service Unavailable`. The service starts successfully for all other tenants. A degraded tenant retries discovery on the next auth request to it (lazy re-init). This prevents a transient network issue with one customer's IdP from taking down the entire identity service.
7. Build the in-memory `TenantRegistry` keyed by `tenant_id`

### Device Auth Flow (`InitiateDeviceAuth`, `PollDeviceAuth`)

1. Look up `req.TenantID` in the `TenantRegistry` — unknown tenant → `400 Bad Request`
2. Check `tenant.State == "active"` — suspended tenant → `403 Forbidden`
3. Check `tenant.Status != degraded` — degraded (OIDC provider unreachable) → `503 Service Unavailable`
4. Use the tenant's registered OIDC provider for device auth initiation and token polling
5. After the OIDC token is returned and verified, confirm `idToken.Issuer == tenant.OIDC.IssuerURL` — mismatch → `403 Forbidden`
6. If `claimBinding` is configured, verify the specified claim matches the configured value using the semantics defined above — mismatch → `403 Forbidden`
7. Proceed to build the `Principal` with `TenantID: req.TenantID` as before — now this value is validated

**Cross-tenant device code mismatch:** When a client calls `POST /identity/device` with `tenant_id: "acme"` and then calls `POST /identity/session` with `tenant_id: "other-corp"`, the session handler uses `other-corp`'s OIDC provider to attempt the device code exchange. This will fail at the OIDC provider level with an `invalid_client` or `invalid_grant` error — the platform surfaces this as a `400 Bad Request`. This failure mode is **safe** (no cross-tenant token issuance occurs) and is not worth adding a device code / tenant affinity check to fix — that would require persisting the initiating tenant alongside the device code, adding complexity for a better error message only.

### SVID Exchange Flow

No changes required. The SVID path derives `tenant_id` from the cryptographically-attested SPIFFE URI (ADR-0010, ADR-0018). The `TenantRegistry` is not consulted for this path.

---

## Consequences

### Positive

- The cross-tenant token issuance gap in the device auth flow is closed
- The platform team never owns individual user membership — the customer's IdP is authoritative
- Tenant config is binary-independent — the same image deploys for any set of tenants
- Local development remains zero-config after a one-time `.env` addition — `config/tenants.yaml` is committed and pre-wired
- Tenant configuration is GitOps-managed in the deployment repository — correct ownership boundary
- Adding a tenant is a config change in the deployment repo + pod restart — no code change, no image rebuild, no migration
- Per-tenant OIDC provider routing is implemented, fulfilling ADR-0008
- The global `OIDCConfig` and its associated env vars are removed, eliminating dead and confusing configuration
- The config file schema maps 1:1 onto a future `Tenant` CRD `spec` — the operator evolution path is not blocked

### Negative

- Adding a new tenant requires a pod restart (no hot-reload initially)
- Each tenant requires its own OIDC client registration + secret in the deployment environment
- A missing or malformed tenant config file causes a startup failure (intentionally safe-fail)
- Developers who have already run `make env` must manually add `ORBIT_TENANTS_CONFIG` and `ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET` to their `.env` once

### Neutral

- Existing sessions for a suspended tenant remain valid until natural expiry (accepted gap — short TTL + JTI blocklist for urgent revocation)
- No `tenants` DB table is introduced — tenant config is infrastructure configuration, not application data. If future features require querying tenants at scale, a DB-cached representation would be introduced at that point via the CRD controller model

---

## Implementation

### Phase 1 — Platform Config Extension and Tenant Registry Loader

1. Add `config.LoadFile[T any](path string) (*T, error)` to `internal/platform/config/config.go`
   - Reads file at the given path (absolute or relative to CWD), unmarshals into `T` via `unmarshalExact`
   - No `APP_ENV` or project-root resolution — caller provides the full path
2. Define `TenantRegistryFile`, `TenantConfig`, `TenantOIDCConfig`, `ClaimBinding` types in a new `internal/identity/tenants.go`
3. Implement `LoadTenantRegistry(configPath string) (*TenantRegistry, error)`:
   - Calls `config.LoadFile[TenantRegistryFile](configPath)`
   - Validates `version == "v1"` — unknown version returns an error
   - For each tenant, resolves `clientSecretEnv` via `os.Getenv`
   - Calls OIDC discovery per tenant; marks failures as `degraded` rather than returning an error
4. Define `TenantRegistry` as an interface (matching the existing repository pattern in this package) to enable mock injection in tests:
   ```go
   type TenantRegistry interface {
       Get(tenantID string) (*TenantConfig, error)
   }
   ```
5. Remove `OIDCConfig` from `internal/identity/config.go`, remove all global OIDC env var reads (`ORBIT_OIDC_ISSUER_URL`, `ORBIT_OIDC_CLIENT_ID`, `ORBIT_OIDC_CLIENT_SECRET`), remove the `OIDCConfig` validation check

### Phase 2 — Identity Service Wiring and Device Auth Validation

6. Replace `s.oidcProvider` (single global `*oidc.Provider`) on `Service` with `s.registry TenantRegistry`
7. Remove `initOIDCProvider()` from `NewService`; add `LoadTenantRegistry` call in `NewService` reading `ORBIT_TENANTS_CONFIG` — fail fast if unset
8. Update `InitiateDeviceAuth` to look up the tenant registry and use the tenant-specific OIDC provider
9. Update `PollDeviceAuth` to validate `idToken.Issuer` against the tenant's registered issuer
10. Add `claimBinding` validation post-OIDC-auth using the matching semantics defined in this ADR
11. Add `503 Service Unavailable` response for `degraded` tenants
12. **Testing:** `mocks_test.go` gains a `MockTenantRegistry` implementing `TenantRegistry`. All existing `service_test.go` and `handlers_test.go` tests are updated to inject the mock registry rather than the removed single-provider assumption. Tests are updated before the PR is mergeable — a failing test suite is not acceptable.

### Phase 3 — Local Dev and Docker Compose Wiring

13. Create `config/tenants.yaml` at project root (content as shown in Local Development section above)
14. Add to `.env.example`:
    ```env
    ORBIT_TENANTS_CONFIG=./config/tenants.yaml
    ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET=
    ```
15. Update `docker-compose.yml` identity service to bind-mount `config/tenants.yaml` at `/etc/orbit/tenants.yaml` and set `ORBIT_TENANTS_CONFIG`
16. Update `AGENTS.md` dev setup section to document the new env vars
17. Verify `make dev-up` + `make auth-device` works end-to-end with `tenant_id: "local"` in the device auth request
18. Verify `make dev` works end-to-end (direct binary, sourcing from `.env`)

### Phase 4 — Helm Chart

19. Add `orbit.tenants` values block to the Helm chart (schema mirrors the YAML file structure)
20. Render `tenants.yaml` ConfigMap from values, always including `version: "v1"` as the first line
21. Mount ConfigMap at `/etc/orbit/tenants.yaml` in identity service Deployment
22. Document in chart README: how to add a tenant, how to provide client secrets via Kubernetes Secrets

### Migration Path

There is no migration from an old state. This is an additive change for production (no tenant registry existed before). The removed `OIDCConfig` env vars have never been used in production — their removal has no rollback concern.

The path from this ADR to the future CRD model:
- Each entry in `tenants.yaml` maps 1:1 to a `Tenant` CRD `spec`
- The CRD controller reconciles `Tenant` resources into the same `TenantRegistry` interface
- The identity service's `TenantRegistry` interface does not change — only its backing source changes from a file to a controller-managed cache

---

## Validation

### Success Metrics

- A user who authenticates against tenant A's OIDC provider cannot receive a platform token scoped to tenant B
- A `tenant_id` not present in the registry returns `400 Bad Request`
- A token issued by a different OIDC provider than the one registered for the requested tenant returns `403 Forbidden`
- A `claimBinding` mismatch returns `403 Forbidden`
- A request to a `degraded` tenant returns `503 Service Unavailable`
- `make dev-up` + `make dev` + `make auth-device` works with no additional manual setup beyond `make env`
- The identity service fails fast at startup with a clear error when `ORBIT_TENANTS_CONFIG` is unset, the file is missing, or the version is unknown

### Monitoring

- Auth failures by reason: unknown tenant, suspended tenant, issuer mismatch, claim binding mismatch, provider degraded
- Per-tenant token issuance rate
- OIDC provider initialisation failures at startup (per tenant)
- Degraded tenant recovery events (lazy re-init success/failure)

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — Foundational identity model establishing per-tenant OIDC as a requirement
- [ADR-0006](0006-yaml-file-based-configuration.md) — YAML config system for service tuning; tenant config deliberately lives outside this system; `no-raw-yaml` lint rule requires YAML parsing to route through `platform/config`
- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — SPIFFE trust domain topology for attested identity; unaffected by this ADR
- [ADR-0017](0017-oidc-provider-abstraction-and-local-identity-provider.md) — OIDC provider abstraction and local Dex; extended to per-tenant routing
- [ADR-0018](0018-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — Namespace-derived tenant isolation for SPIFFE identities
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Policy enforcement layer that consumes the tenant-scoped platform token

---

## References

- [Dex Connector Configuration](https://dexidp.io/docs/connectors/) — runtime-mounted operator config as a reference model
- [Prometheus Configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/) — `--config.file` flag pattern for operator-provided config
- [Vault Configuration](https://developer.hashicorp.com/vault/docs/configuration) — file-based operator config mounted at runtime
- [RFC 8414 — OAuth 2.0 Authorization Server Metadata](https://www.rfc-editor.org/rfc/rfc8414) — OIDC discovery used for per-tenant provider initialisation
- [OpenID Connect Core — `iss` Claim](https://openid.net/specs/openid-connect-core-1_0.html#IDToken) — issuer binding for tenant membership validation

---

## Notes

The `claimBinding` mechanism is an escape hatch for shared-IdP deployments. It is not the recommended model for production multi-tenancy where each tenant should have its own OIDC client registration — then the issuer URL alone is sufficient for membership.

The absence of a `tenants` DB table is intentional at this stage. Tenant configuration is deployment-time infrastructure configuration. The DB is introduced when features requiring at-scale tenant queries arrive — at that point, the CRD controller model writes a DB-cached copy of the CRD state.

**Last Updated:** 2026-06-10

# Branch Review: PR Preparation

**Date:** 2026-04-03
**Branch:** `feat/frontend-cluster-setup`
**Scope:** 3 commits, 61 files changed — domain migration, TLS setup, JWT restructure, frontend/inference-gateway cluster deployment

**Commits reviewed:**
1. `8d8f640` — chore(config): migrate local dev domain from orbit.local to orbit.test
2. `7569f92` — feat: implement TLS setup for local development with mkcert
3. `7f0ffbf` — feat(auth): restructure Orbit JWT payload to use nested object for claims and display name

---

## Issue Summary

| Severity | Count | Areas |
|----------|-------|-------|
| CRITICAL | 2 | JWT type guard, Dockerfile/Helm URL mismatch |
| HIGH | 3 | Tenant config still HTTP, Dex comment mismatch, ADR gateway selector wrong |
| MEDIUM | 6 | Missing security contexts, Tier 1 TLS trust, docs not updated, health check path |
| LOW | 4 | Hardcoded domains, missing service types, minor inconsistencies |

---

## 1. JWT Payload Restructure (commit `7f0ffbf`)

### 1.1 CRITICAL: Type Guard Not Updated for New Structure

**File:** `frontend/src/app/api/auth/session/route.ts:60-67`

The `isOrbitJwtPayload()` type guard still only checks for `sub` and `exp`. It does **not** validate the new nested `orbit` object:

```typescript
function isOrbitJwtPayload(v: unknown): v is OrbitJwtPayload {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>)['sub'] === 'string' &&
    typeof (v as Record<string, unknown>)['exp'] === 'number'
  );
}
```

Any JWT with `sub` and `exp` passes validation, even if `orbit` is malformed or missing. This means invalid payloads can silently produce empty user data downstream.

**Fix:** Add validation for the `orbit` field structure, or at minimum check that if `orbit` exists, it's an object.

### 1.2 HIGH: No Backward Compatibility for In-Flight Tokens

**File:** `frontend/src/app/api/auth/session/route.ts:165-169`

The code now reads claims exclusively from the nested structure:

```typescript
const user: SessionUser = {
  sub: claims.sub,
  email: claims.orbit?.claims?.['email'] ?? '',
  name: claims.orbit?.display_name ?? '',
};
```

Old tokens issued before the backend update used flat dot-notation fields (`'orbit.display_name'`, `'orbit.claims'`). Any cached tokens from the old format will silently produce empty `email` and `name` strings. Either:
- Add dual-format support during a transition period
- Force token refresh on deployment
- Document this as an intentional breaking change

### 1.3 MEDIUM: No Test Coverage

No tests exist for JWT parsing with the new nested structure. Missing coverage for: valid full payload, partial `orbit` object, missing `orbit` field, fallback to empty strings.

---

## 2. TLS Setup (commit `7569f92`)

### 2.1 HIGH: Tenant Config Still Uses HTTP Issuer URL

**File:** `config/tenants.yaml:42`

```yaml
issuerURL: "http://dex.orbit.test:5556/dex"
```

Dex now runs with TLS (HTTPS), but this config still points to HTTP. The identity service will fail OIDC discovery in Tier 1 (host process) development because Dex is only serving HTTPS.

**Fix:** Update to `https://dex.orbit.test:5556/dex`.

### 2.2 HIGH: Dex Config Comment Mismatch

**File:** `config/dex/config.yaml:17`

The comment header says `Issuer: http://dex.orbit.test:5556/dex` but the actual config on line 23 is `issuer: https://dex.orbit.test:5556/dex`. Misleading for developers.

**Fix:** Update comment to match HTTPS reality.

### 2.3 MEDIUM: Tier 1 Dev TLS Trust Gap

When running `make dev` (identity service on host), the Go HTTP client needs to trust Dex's self-signed cert (signed by local mkcert CA). There's no mechanism to:
- Load the mkcert CA into the host's trusted CA store
- Configure the Go runtime to trust it

The identity service will fail HTTPS verification against `dex.orbit.test`.

**Fix:** Either document the CA trust setup for host dev, or keep Dex on HTTP for docker-compose Tier 1 mode.

### 2.4 MEDIUM: Documentation Still References HTTP

Multiple docs still use HTTP URLs and old port 8880:

| File | Lines | Issue |
|------|-------|-------|
| `docs/guides/local-development.md` | 111, 354, 357, 379-384 | HTTP issuer URL, curl examples |
| `docs/adr/0034-frontend-cluster-deployment-and-ingress-routing.md` | 231-234 | Test assertions use `http://orbit.test:8880` |
| `docs/adr/0026-tenant-registration-and-oidc-provider-binding.md` | Various | HTTP dex references |
| `docs/adr/0031-home-realm-discovery-and-email-based-tenant-routing.md` | Various | HTTP dex references |
| `docs/adr/0017-oidc-provider-abstraction-and-local-identity-provider.md` | Various | HTTP dex references |
| `docs/adr/0030-browser-auth-flow-and-bff-session-architecture.md` | Various | HTTP examples |
| `docs/prd/browser-auth-flow.md` | Various | HTTP references |

### 2.5 Positive: TLS Script Is Well-Designed

`scripts/setup-tls.sh` is idempotent, has clear error messages, properly copies the mkcert CA for in-cluster use, and integrates cleanly with the Makefile `setup` target.

---

## 3. Domain Migration — orbit.local to orbit.test (commit `8d8f640`)

### 3.1 PASS: No Remaining orbit.local References

Grep confirms zero remaining references to `orbit.local` in the codebase. Migration is complete.

### 3.2 Positive: Consistent Updates

All config files, Helm values, scripts, docker-compose, Dex config, and ADRs were consistently updated from `orbit.local` to `orbit.test`.

---

## 4. Helm Charts & Deployment (new templates)

### 4.1 CRITICAL: Dockerfile Default vs Helm Values URL Mismatch

**Files:**
- `frontend/Dockerfile:46` — `ARG NEXT_PUBLIC_APP_URL=http://orbit.test:8880`
- `deploy/local/orbit-values.yaml:83` — `nextPublicAppUrl: "https://orbit.test:8443"`

The Dockerfile default build arg uses HTTP on port 8880, but the Helm values expect HTTPS on port 8443. The Next.js client bundle baked at build time will have the wrong URL.

**Fix:** Update Dockerfile default to `https://orbit.test:8443` to match the TLS-enabled local setup, or ensure the build always passes the correct arg.

### 4.2 HIGH: ADR-0034 Gateway Selector Mismatch

**File:** `docs/adr/0034-frontend-cluster-deployment-and-ingress-routing.md:153`

ADR specifies `app: istio-gateway` but the actual implementation in `charts/orbit-platform/templates/gateway.yaml:36` uses `app: gateway`. Developers following the ADR code snippet will create a non-functional Gateway.

**Fix:** Update ADR-0034 line 153 to `app: gateway`.

### 4.3 MEDIUM: Missing securityContext on New Deployments

**Files:**
- `charts/orbit/templates/frontend/deployment.yaml:31-76`
- `charts/orbit/templates/inference-gateway/deployment.yaml:29-87`

Neither deployment defines `securityContext`. While the Dockerfiles set up non-root users (uid 65532), Kubernetes doesn't enforce this without:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
```

### 4.4 MEDIUM: Frontend Health Check Path Not Validated

**File:** `charts/orbit/templates/frontend/deployment.yaml:56,64`

Probes use `/api/health` but this endpoint is not visible in the frontend source code. If the route doesn't exist, pods will never become ready.

**Verify:** Confirm `/api/health` route exists in the Next.js app, or change to a known path.

### 4.5 LOW: Hardcoded Domain Names in Templates

**Files:**
- `charts/orbit-platform/templates/gateway.yaml:48-49,58-59,67-68` — `orbit.test`, `api.orbit.test` hardcoded
- `charts/orbit/templates/frontend/virtualservice.yaml:18` — `orbit.test` hardcoded
- `charts/orbit/templates/inference-gateway/virtualservice.yaml:20` — `api.orbit.test` hardcoded

These should be templated for production use (e.g., `{{ .Values.global.domain }}`).

### 4.6 LOW: Missing Explicit Service Type

**Files:**
- `charts/orbit/templates/frontend/service.yaml`
- `charts/orbit/templates/inference-gateway/service.yaml`

No explicit `type: ClusterIP`. Works (it's the default) but should be explicit for clarity.

### 4.7 Positive: Good Patterns

- Multi-stage Dockerfile with non-root user (uid 65532) and standalone Next.js output
- Resource limits defined for all services
- Proper `app.kubernetes.io/*` labels across all resources
- SPIFFE CSI volume mounting correct
- Istio sidecar annotations proper
- NetworkPolicy well-designed with configurable egress
- OrbitTenant CRD `emailDomains` field addition is backward compatible

---

## 5. Recommended Actions

### Before Merge

1. **Fix Dockerfile default URL** — Change `frontend/Dockerfile:46` to `https://orbit.test:8443`
2. **Fix tenant config issuer** — Change `config/tenants.yaml:42` to HTTPS
3. **Fix Dex config comment** — Update `config/dex/config.yaml:17` comment to HTTPS
4. **Fix ADR-0034 gateway selector** — Change `app: istio-gateway` to `app: gateway` on line 153
5. **Update JWT type guard** — Validate `orbit` field in `isOrbitJwtPayload()` at `route.ts:60-67`

### Before Production

6. **Add securityContext** to frontend and inference-gateway deployments
7. **Validate `/api/health` exists** in the frontend app or update probe path
8. **Template domain names** in gateway and virtualservice Helm templates
9. **Document Tier 1 TLS trust** — How to trust mkcert CA on host for `make dev`
10. **Update docs** — Fix HTTP references in guides and ADRs listed in section 2.4

### Nice to Have

11. Add tests for JWT nested payload parsing
12. Add backward compatibility for old-format JWT tokens during transition
13. Add explicit `type: ClusterIP` to new service definitions

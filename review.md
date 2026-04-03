# Orbit Codebase Review

**Date:** 2026-04-03
**Branch:** `claude/code-review-documentation-M9osX`
**Scope:** Full codebase — ADRs, backend Go code, frontend, Helm charts, infrastructure, scripts

---

## Executive Summary

The Orbit codebase demonstrates **sophisticated architectural thinking** and **strong security fundamentals**. The ADR collection is comprehensive (34 documents), the Go backend follows clean patterns, and infrastructure is well-structured. However, there are several issues that need attention before a production-ready PR, ranging from critical stub implementations to documentation inconsistencies.

**Issue Counts by Severity:**

| Severity | Count | Summary |
|----------|-------|---------|
| CRITICAL | 4 | Stub policy engine, stub pricing, missing security contexts, duplicate ADR numbering |
| HIGH | 6 | CSRF protection, rate limiting, hardcoded image repos, missing imagePullSecrets, broken doc links, proposed ADRs blocking |
| MEDIUM | 16 | Error handling gaps, validation issues, missing PDBs, NetworkPolicy gaps, config parsing |
| LOW | 10+ | Code style, documentation, minor inconsistencies |

---

## 1. Architecture Decision Records (ADRs)

### 1.1 CRITICAL: Duplicate ADR Numbering (ADR-0021)

Two files share the number 0021:
- `docs/adr/0021-envoy-ext-proc-sidecar-token-injection.md` (Accepted, 2026-03-03)
- `docs/adr/0021-terra-frontend-design-system.md` (Accepted, 2025-07-11)

All references to "ADR-0021" are ambiguous. The Terra Design System ADR should be renumbered to 0035.

### 1.2 HIGH: Broken File References

- **ADR-0007** references `[code-organization.md](../code-organization.md)` — file does not exist at that path.
- **ADR-0006** mentions `docs/product/scratch/config-layering.md` — directory does not exist.
- **ADR-0001** references "Future ADR: API schema and contract definition" — never created.

### 1.3 HIGH: Stale "Proposed" Status on Foundational ADRs

Several ADRs remain "Proposed" despite having full implementations and being referenced by later Accepted ADRs:

| ADR | Title | Status | Issue |
|-----|-------|--------|-------|
| 0018 | Dynamic SPIFFE | Proposed | Well-written, no apparent blockers |
| 0023 | Inference Gateway | Proposed | Fully implemented in codebase |
| 0026 | Tenant Registration | Proposed | ADR-0031 (Accepted) depends on it |
| 0029 | OIDC Client Secrets | Proposed | No blockers documented |

**Recommendation:** Promote to Accepted or add explicit "Blockers" sections explaining what prevents acceptance.

### 1.4 MEDIUM: Date Inconsistencies

- ADR-0019 (Frontend Scaffolding): Date 2026-07-10 (future)
- ADR-0021 (Terra): Date 2025-07-11 (past, inconsistent with surrounding ADRs)
- ADR-0032 (Model Capability): Date 2025-07-24

### 1.5 MEDIUM: Incomplete Cross-References

Many ADRs reference others but the "Related Decisions" sections are not bidirectional. For example:
- ADR-0013 (MCP Server Registration) should appear in ADR-0009's related decisions
- ADR-0014 (Scope-Gated Tools) should appear in ADR-0011's related decisions
- ADR-0011 contains unresolved "open question" text that was resolved by ADR-0015

### 1.6 MEDIUM: Missing ADR Coverage

| Topic | Gap |
|-------|-----|
| API Schema/Contracts | Go-TypeScript contract definition never documented |
| Audit Event Pipeline | How audit events flow from enforcement to event spine |
| Database Migrations | Migration tool (Goose) used but not formally decided |
| GitOps Workflow | Promotion between environments not documented |
| Session Storage | Redis vs primary DB not clarified in identity ADR |

### 1.7 LOW: Writing Quality Variance

- **Strong:** ADR-0009 (Delegation), ADR-0012 (Credential Gateway), ADR-0014 (Scope-Gated Tools)
- **Needs improvement:** ADR-0018 (Dense, no summary), ADR-0023 (Brief rationale for major system), ADR-0028 (Lacks high-level summary)

---

## 2. Backend Go Code

### 2.1 CRITICAL: Stub Implementations Not Production-Ready

#### AllowAllPolicyEvaluator
**File:** `backend/internal/inferencegw/policy.go:32-50`

```go
type AllowAllPolicyEvaluator struct{}

func (a *AllowAllPolicyEvaluator) EvaluateInferenceRequest(
    _ context.Context,
    _ *InferencePolicyRequest,
) (*PolicyDecision, error) {
    return &PolicyDecision{
        Allowed: true,  // ALWAYS allows all requests
        Reason:  "policy engine not yet integrated — all requests permitted",
    }, nil
}
```

The inference gateway has **zero policy enforcement**. All requests are unconditionally allowed. A startup warning is logged but this must be replaced with real OPA integration before production.

#### NopPricingCache
**File:** `backend/internal/inferencegw/pricing.go:44-49`

Returns zero pricing for all models, meaning all usage records have `estimated_cost_usd = 0`. Billing/chargeback is non-functional.

### 2.2 HIGH: Missing CSRF Protection

**File:** `backend/internal/identity/handlers.go:259-316`

The token revocation POST endpoint accepts `application/x-www-form-urlencoded` without CSRF token validation. RFC 7009 requires protection against cross-site request forgery on state-changing operations.

### 2.3 HIGH: No Rate Limiting on Auth Endpoints

**Files:** All auth endpoints in `backend/internal/identity/handlers.go`

No rate limiting middleware on `POST /device`, `POST /session`, `POST /tokens`. Device auth polling could be abused for tenant enumeration.

### 2.4 MEDIUM: Systematically Ignored Errors in Operator Controllers

**Files:**
- `backend/internal/operator/tenant_controller.go:90-106`
- `backend/internal/operator/platform_controller.go:92-111`

Status/condition-setting errors are discarded with `_ =` throughout both controllers:

```go
_ = r.setTenantCondition(ctx, tenant, ConditionNamespaceReady, metav1.ConditionFalse, "CreateError", err.Error())
_ = r.setTenantPhase(ctx, tenant, TenantPhaseDegraded)
```

If status updates fail, the operator has no visibility into these failures. At minimum, log warnings.

### 2.5 MEDIUM: Silent Config Parsing Failures

**Files:**
- `backend/internal/platform/storage/redis_config.go:46-49` — Invalid REDIS_PORT silently falls back to default
- `backend/internal/platform/storage/postgres_config.go:66-68` — Same for Postgres port
- `backend/internal/platform/observability/config.go:58` — Sampling rate parse error ignored
- `backend/internal/identity/config.go:73-78` — `overrideInt` helper silently ignores `strconv.Atoi` errors

Invalid environment variable values are silently discarded. Operators get no warning that their configuration overrides failed.

**Fix:** Log warnings when env var overrides fail to parse.

### 2.6 MEDIUM: Silent JSON Unmarshal Errors

**Files:**
- `backend/internal/inferencegw/handlers_openai.go:60` — `_ = json.Unmarshal(b, &raw)` on stream flag
- `backend/internal/inferencegw/handlers_anthropic.go:33` — Same pattern

If the request body is malformed, the stream flag silently defaults to `false` with no indication to the caller.

### 2.7 MEDIUM: Missing Input Validation at Handler Boundary

**File:** `backend/internal/identity/handlers.go:347`

Email query parameter is extracted but format validation only happens deep in the service layer. Early validation at the handler would give cleaner error responses.

### 2.8 MEDIUM: Potential Information Disclosure

**File:** `backend/internal/identity/service.go:215-223`

Device auth error responses include internal provider endpoint URLs:

```go
return nil, fmt.Errorf("device auth endpoint %s returned %d (%s): %w",
    ts.meta.DeviceAuthorizationEndpoint, resp.StatusCode, oe.Code, oauthErr)
```

Internal infrastructure details could be exposed to callers.

### 2.9 MEDIUM: Missing Audit Logging

PAT creation/revocation and token exchanges are logged but not in a structured audit format that's easily queryable for compliance/forensics.

### 2.10 MEDIUM: Race Condition Risk in Helm Release Management

**File:** `backend/internal/operator/platform_controller.go:77-96`

The 10-second requeue interval may be too aggressive for Helm operations that take 30+ seconds. Consider exponential backoff.

### 2.11 LOW: Unused Context Parameter

**File:** `backend/internal/inferencegw/router.go:90`

```go
func (r *RoutingEngine) RouteRequest(
    ctx context.Context, ...
) ([]RouteTarget, RoutingStrategy, error) {
    _ = ctx // reserved for future policy-aware routing
```

### 2.12 LOW: Inconsistent Error Code Naming

Error codes mix `snake_case` (`missing_required_field`) and other conventions (`policy_denied`) across API responses.

### 2.13 LOW: Inconsistent Logging Field Names

Field keys vary across the codebase: `error` vs `err`, `tenant_id` vs `tenantId`.

### 2.14 Test Coverage Gaps

- **22 test files for 113 Go files** (~19% by file count)
- **Missing tests for:**
  - `backend/internal/operator/` — No reconciler tests (only model tests)
  - `backend/internal/identityproxy/` — Only basic tests
  - `backend/internal/platform/storage/` — No transaction tests
  - `backend/cmd/` — No main/integration tests
- **Missing error path tests:** OIDC discovery failures, token signing key load failures, tenant registry watcher crashes

---

## 3. Frontend Code

### 3.1 Strengths

- Proper `server-only` imports preventing client-side exposure of sensitive env vars
- HTTP-only cookies with `SameSite: strict` for CSRF protection
- `cache: 'no-store'` on sensitive endpoints
- 5-10 second timeouts on all API calls
- Full TypeScript strict mode enabled

### 3.2 LOW: TODO - Missing Breadcrumbs Component

**File:** `frontend/src/app/(dashboard)/_components/dashboard-header.tsx:17`

```
TODO: Replace with a Terra native Breadcrumbs component
```

Design debt — not a bug but indicates incomplete UI component migration.

### 3.3 LOW: Session Secret Validation Redundancy

**File:** `frontend/src/server/auth-config.ts`

Secret is validated in two places within `getSessionOptions()` (lines 32-34 and 75). Slightly redundant defensive code.

---

## 4. Helm Charts & Kubernetes

### 4.1 CRITICAL: Missing securityContext on All Service Deployments

**Files:**
- `charts/orbit/templates/frontend/deployment.yaml` — No securityContext
- `charts/orbit/templates/identity/deployment.yaml` — No securityContext
- `charts/orbit/templates/inference-gateway/deployment.yaml` — No securityContext

Only the operator deployment defines a security context. All service pods should have:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
containers:
  - securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true  # where applicable
```

### 4.2 HIGH: Missing imagePullSecrets Configuration

No `imagePullSecrets` in any deployment manifest or values.yaml. Local dev works (orbit-registry:5001), but production deployments to ghcr.io would fail without registry auth.

### 4.3 HIGH: Hardcoded Image Repositories

**File:** `charts/orbit/values.yaml:42,115,150`

Image repos hardcoded to `ghcr.io/orbit-sh/`. The `imageRegistry` parameter in the OrbitPlatform CR can prefix these, but manual Helm installs cannot override the registry.

**Fix:** Add `image.registry` to chart values and template as `{{ .Values.image.registry }}/orbit-identity`.

### 4.4 MEDIUM: No PodDisruptionBudgets

No PDB defined for any service. Cluster drains could disrupt single-replica services.

### 4.5 MEDIUM: Missing Network Egress Rules for Production

**File:** `charts/orbit-platform/values.yaml:42-51`

`externalServices: []` defaults to no external egress rules. Production pods cannot reach external Postgres/Redis without explicit rules. Works locally because NetworkPolicy is disabled.

### 4.6 MEDIUM: Operator Pod Missing NetworkPolicy Documentation

The operator correctly disables the Istio sidecar but cluster-wide NetworkPolicy might block operator control-plane communications. Required egress rules should be documented.

### 4.7 LOW: Liveness/Readiness Probe Timing

Identity deployment's `initialDelaySeconds: 15` may be too aggressive for slow-starting services. Consider making configurable.

---

## 5. Infrastructure & Configuration

### 5.1 MEDIUM: k3d Registry on All Interfaces

**File:** `infrastructure/bootstrap/k3d-config.yaml:21-25`

Registry listens on `0.0.0.0:5001` with no authentication. Acceptable for local dev but must be documented as not production-safe.

### 5.2 MEDIUM: Missing `make gen-session-secret` Target

**File:** `.env.example:198`

`BFF_SESSION_SECRET=` is empty. Users must manually generate it (documented: `openssl rand -hex 32`). Unlike signing keys which have `make gen-signing-key`, no automation exists for session secrets.

### 5.3 LOW: Relative Config Path

**File:** `.env.example:89`

`ORBIT_TENANTS_CONFIG=../config/tenants.yaml` — relative path breaks if running from a different directory.

### 5.4 LOW: CoreDNS Hardcoded Fallback IP

**File:** `scripts/cluster/dev-cluster-up.sh:78-112`

Docker bridge IP has hardcoded fallback to `172.18.0.1`. If the Docker Compose network uses a different IP, CoreDNS patching fails silently.

---

## 6. Makefile & Developer Experience

### 6.1 Strengths

- Well-organized targets with `make help` support
- Two-tier local dev (Tier 1: Docker Compose, Tier 2: k3d cluster)
- Automated secret generation for signing keys and Dex passwords
- Proper `.env` auto-copy from `.env.example`

### 6.2 MEDIUM: Missing Automation for BFF Session Secret

As noted in 5.2 — `make gen-session-secret` target would improve developer onboarding.

---

## 7. Recommended Actions

### Immediate (Before PR Merge)

1. **Document stub implementations** — Add clear warnings in README/AGENTS.md that `AllowAllPolicyEvaluator` and `NopPricingCache` are not production-ready
2. **Fix ADR-0021 duplicate numbering** — Renumber Terra Design System ADR to 0035
3. **Fix broken doc references** — ADR-0007 and ADR-0006 broken links
4. **Add securityContext to all Helm deployments** — Critical for production security posture

### Before Production

5. Replace `AllowAllPolicyEvaluator` with real OPA integration
6. Implement real `PricingCache` for accurate billing
7. Add CSRF protection to form-based token revocation endpoint
8. Add rate limiting middleware on auth endpoints
9. Fix silent config parsing — log warnings on env var parse failures
10. Add `imagePullSecrets` and configurable image registry to Helm charts
11. Add PodDisruptionBudgets for all services
12. Add NetworkPolicy egress rules documentation for production

### Housekeeping

13. Promote stale "Proposed" ADRs (0018, 0023, 0026, 0029) or document blockers
14. Add missing ADRs: API Schema/Contracts, Audit Event Pipeline, Database Migrations
15. Audit and fix all ADR cross-references (make bidirectional)
16. Update outdated "future ADR" / "open question" text in ADRs 0001, 0011
17. Add `make gen-session-secret` target
18. Increase test coverage — especially operator reconcilers, identity proxy, storage layer

---

## 8. Positive Observations

- **Strong security design** — SPIFFE/SPIRE workload identity, HTTP-only cookies, SameSite strict, server-only imports
- **Well-structured ADR collection** — 34 ADRs with consistent template, clear decision drivers and consequences
- **Clean Go patterns** — Repository pattern, dependency injection, proper middleware chaining
- **Excellent local dev experience** — Two-tier setup, automated secrets, Docker Compose + k3d
- **Good Helm chart structure** — Platform/tenant separation, CRD validation schemas, SPIRE integration
- **Proper multi-stage Docker builds** — Non-root users, minimal images, standalone Next.js output

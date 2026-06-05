# OIDC Client Secret Projection for Confidential Tenants

**Status:** Accepted
**Date:** 2026-03-31
**Deciders:** Engineering Team
**Technical Story:** `OrbitTenant.spec.oidc.clientSecretRef` is defined in the CRD and data model but the operator does not act on it — the OIDC client secret is never delivered to the `orbit-identity` service. Any tenant using a confidential OIDC client (Okta, Azure AD, Google Workspace, Auth0, etc.) cannot complete device authorization because the identity service has no client secret to supply to the token endpoint. This is a blocking gap for all enterprise tenants.

---

## Context

### Background

[tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) established the tenant registry model: per-tenant OIDC configuration is stored in the `orbit-tenants` ConfigMap and the identity service reads it at startup, reloading automatically via an `fsnotify` file watcher when the ConfigMap volume is updated by the operator. [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) introduced the Orbit operator and `OrbitTenant` CRD, which includes a `spec.oidc.clientSecretRef` field pointing to a pre-existing `Secret` containing the OIDC client secret.

The tenant registry format (`orbit-tenants` ConfigMap) includes a `clientSecretEnv` field per tenant — a named environment variable the identity service will read to obtain the client secret at runtime:

```yaml
tenants:
  - id: acme-corp
    oidc:
      issuerURL: https://acme.okta.com
      clientID: 0oa1b2c3d4e5f6
      clientSecretEnv: ORBIT_TENANT_ACME_CORP_OIDC_CLIENT_SECRET
```

The operator derives this env var name from the tenant ID via a deterministic convention, writes it into the ConfigMap, but **never delivers the actual secret value** to the identity service. The env var referenced in `clientSecretEnv` does not exist in the `orbit-identity` pod's environment unless it is manually injected outside the operator's control.

### Why This Cannot Use `envFrom` or Deployment Patching

The operator could add a new env var to the `orbit-identity` Deployment whenever a tenant with a `clientSecretRef` is registered. This is the obvious approach but it is incompatible with a core architectural constraint established in [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) and [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md):

> **Tenant registration must not cause a pod restart of the identity service.**

A Deployment env var change triggers a rollout. Even a rolling rollout causes a brief window of request drain and pod termination. For a shared platform identity service handling all tenant authentication, any restart induced by a tenant onboarding event is unacceptable downtime.

The same constraint eliminates `envFrom` referencing individual per-tenant Secrets: adding a new Secret to a pod's `envFrom` list also requires a Deployment update and rollout.

### How the Tenant Registry Hot-Reload Works Today

The `orbit-identity` service uses `fsnotify` to watch the directory in which the `orbit-tenants` ConfigMap is mounted. Kubernetes ConfigMap volume updates use an atomic symlink swap on the `..data` directory — `fsnotify` detects the `RENAME`/`CREATE` event, debounces for 200ms, and calls `reloadTenantOIDCProviders`. The in-memory tenant map is replaced under a `sync.RWMutex` with zero downtime. No pod restart is needed when tenants are added, changed, or removed.

A parallel mechanism is needed for OIDC client secrets.

### The Secret Rotation Requirement

Client secrets are not write-once. Enterprise IdPs require periodic rotation (Okta recommends 90 days; Azure AD enforces it by policy). When a tenant rotates their OIDC client secret they update the `Secret` in their namespace. The platform must propagate that change to the identity service without downtime. This means the solution must handle updates to existing secrets, not just initial projection.

### Problem Statement

The identity service needs OIDC client secret values for confidential tenants at runtime — injected without pod restart, updatable without pod restart, and sourced from the `clientSecretRef` the tenant declared in their `OrbitTenant` CR.

### Goals

- Deliver OIDC client secrets from tenant-declared `clientSecretRef` references to the `orbit-identity` service
- Do not restart the identity service pod when a tenant is added, updated, or has their client secret rotated
- Reuse the established hot-reload pattern (file watcher) to keep the mechanism consistent and auditable
- Work in all deployment environments: local k3d, EKS, customer self-hosted (i.e. be compatible with ESO, Vault Agent, SealedSecret, and manual Secret creation)
- Maintain security: secret values must not appear in ConfigMaps, logs, or Kubernetes events

### Non-Goals

- Secret creation, rotation scheduling, or sourcing — the operator's contract remains "reads pre-existing Secrets, does not own their lifecycle" ([orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md))
- Delivering arbitrary tenant secrets beyond OIDC client secrets — this ADR is scoped to the OIDC auth flow
- Supporting OIDC client secret delivery for public clients (no `clientSecretRef`) — public clients require no change

---

## Decision Drivers

* **No pod restarts on tenant events** — the hot-reload architecture is a first-class constraint, not a preference
* **Secret rotation propagation** — a solution that works for initial delivery but breaks on rotation is not acceptable
* **Consistency with existing patterns** — the `orbit-tenants` ConfigMap file-watcher pattern is already implemented, understood, and tested; the client secret mechanism should mirror it
* **Cross-namespace RBAC** — the operator needs to read Secrets from tenant namespaces; the RBAC grant must be explicit and minimal
* **No secret exposure in non-secret resources** — Kubernetes Secrets have restricted RBAC; ConfigMaps do not. Secret values must never be written into a ConfigMap

---

## Considered Options

### Option 1: Patch the `orbit-identity` Deployment With Per-Tenant Env Vars

**Description:** When the tenant controller reconciles a tenant with a `clientSecretRef`, it patches the `orbit-identity` Deployment to add an explicit env var entry sourced from the referenced Secret.

**Pros:**
- Simple to understand
- Standard Kubernetes pattern for env var injection
- Identity service code requires no change (already reads from `os.Getenv`)

**Cons:**
- Every tenant add/update triggers a Deployment rollout — violates the no-restart constraint
- Secret rotation (re-reconcile) triggers another rollout
- N tenants added sequentially = N rollouts; each rollout causes ~10s of drain time
- The identity service Deployment becomes a ledger of per-tenant env vars; its spec grows unboundedly with tenant count

**Verdict:** Eliminated. Violates the no-pod-restart constraint.

---

### Option 2: `envFrom` Referencing an Aggregated Secret

**Description:** Operator maintains a single Secret (`orbit-tenant-oidc-secrets`) in `orbit-system` containing all tenant client secrets keyed by the env var convention (`ORBIT_TENANT_ACME_CORP_OIDC_CLIENT_SECRET=s3cr3t`). The identity service Deployment references this Secret via `envFrom`. The operator updates the Secret contents when tenants change.

**Pros:**
- Single Deployment reference (no per-tenant Deployment patching)
- Secret values are kept in a Secret resource, not a ConfigMap

**Cons:**
- Kubernetes `envFrom` env vars are **not live-reloaded** — they are injected at container start time. Updating the Secret's contents does NOT update the running container's environment. A pod restart is still required for env var changes to take effect.
- Rotation requires a restart. Not acceptable.

**Verdict:** Eliminated. `envFrom` does not support live reload.

---

### Option 3: Aggregated Secret Mounted as a File, Read via File Watcher

**Description:** The operator maintains a single Secret (`orbit-tenant-oidc-secrets`) in `orbit-system`. Its data contains one key (`client-secrets.yaml`) whose value is a YAML map of `tenantID → clientSecret`. The identity service Deployment mounts this Secret as a volume at a well-known path (e.g. `/etc/orbit/tenant-secrets/client-secrets.yaml`). The identity service adds a second `fsnotify` watcher on this file's directory — identical in structure to the existing `watchTenantRegistry` watcher. When the Secret is updated by the operator, the kubelet rewrites the volume via the same atomic symlink swap used for ConfigMaps. The watcher detects the `..data` RENAME/CREATE event, debounces, and reloads the in-memory `tenantClientSecrets` map under `s.mu`.

**Pros:**
- No pod restart on tenant add, update, or secret rotation
- Consistent with and structurally identical to the existing `watchTenantRegistry` hot-reload pattern
- Secret values stay in a Secret resource — not exposed in ConfigMaps, logs, or events
- Operator only needs to update one Secret object in `orbit-system`, not patch Deployments
- Kubelet handles propagation latency (~10–30s for Secret volume updates, same as ConfigMap volumes)
- Works identically in all environments — the operator writes the aggregated Secret; ESO/Vault Agent/SealedSecret manages the source Secrets in tenant namespaces
- Handles rotation: tenant rotates their source Secret → operator detects change → rebuilds aggregated Secret → kubelet propagates → watcher reloads → no restart

**Cons:**
- Identity service code change required: switch from `os.Getenv(clientSecretEnv)` to an in-memory map lookup keyed by `tenantID`
- The `clientSecretEnv` field in the tenant registry format becomes redundant (the identity service no longer reads env vars for client secrets); it should be removed or repurposed as documentation
- Operator needs cross-namespace RBAC to `get`/`list`/`watch` Secrets in tenant namespaces
- Secret volume propagation latency (~10–30s) means there is a brief window after rotation where the old secret is still in use — acceptable for credential rotation (the old secret remains valid during rotation overlap)

**Verdict:** Selected. This is the only option that satisfies all constraints simultaneously.

---

## Decision

**Chosen Option:** Option 3 — Aggregated Secret mounted as a volume file, reloaded via `fsnotify` watcher

The aggregated-file approach is the only design that simultaneously satisfies the no-restart constraint, supports rotation propagation, keeps secret values in Secret resources, and is consistent with the established hot-reload architecture.

### Rationale

The `orbit-tenants` ConfigMap file-watcher pattern is already proven in production. Secret volumes in Kubernetes use the same atomic symlink swap mechanism as ConfigMap volumes — the existing `fsnotify` watcher that detects `..data` RENAME/CREATE events works unchanged for Secret volumes. Extending the pattern to client secrets requires adding one watcher goroutine and one in-memory map to the identity service — the structural work is already done.

The alternative options both require pod restarts, which is a non-starter given that tenant registration is a routine operational event that should be zero-impact on the running platform.

---

## Implementation

### Operator Changes

#### 1. RBAC — Cross-Namespace Secret Access

The operator's `ClusterRole` must be extended to permit `get`, `list`, and `watch` on `secrets` across all namespaces. This is the minimum permission needed to read `clientSecretRef`-referenced Secrets from tenant namespaces.

```yaml
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
```

The `create`/`update`/`patch` verbs are scoped to `orbit-system` via a separate `Role` + `RoleBinding` — the operator writes the aggregated Secret only in `orbit-system`. The `get`/`list`/`watch` on the ClusterRole cover tenant namespace reads.

#### 2. Tenant Controller — Read and Aggregate Client Secrets

In `backend/internal/operator/tenant_controller.go`, after reconciling the tenant namespace and ConfigMap:

1. For each `OrbitTenant` with a non-nil `spec.oidc.clientSecretRef`:
   - `GET` the referenced Secret from `spec.oidc.clientSecretRef.namespace` (or the tenant namespace if namespace is omitted)
   - Extract the value at key `OIDC_CLIENT_SECRET`
   - Add it to the aggregated map: `tenantID → secretValue`
2. Build the aggregated Secret data:
   ```yaml
   # client-secrets.yaml (stored as Secret data value)
   acme-corp: "s3cr3t-value"
   globalcorp: "another-s3cr3t"
   ```
3. Create or update `orbit-tenant-oidc-secrets` Secret in `orbit-system` with this YAML as the value of key `client-secrets.yaml`

The aggregated Secret is always rebuilt from scratch across all tenants on each reconcile — this ensures deletions (tenant removed, `clientSecretRef` cleared) are reflected correctly.

#### 3. Controller Watches — Trigger Reconcile on Source Secret Change

Register a `Watches` on `v1.Secret` in the controller setup, filtered to Secrets referenced by `OrbitTenant` CRs. When a source Secret changes (rotation), the controller-runtime enqueues a reconcile for the owning `OrbitTenant`. This propagates the new value into the aggregated Secret within one reconcile cycle.

```go
// In SetupWithManager:
.Watches(
    &corev1.Secret{},
    handler.EnqueueRequestsFromMapFunc(r.secretToTenantRequests),
)
```

`secretToTenantRequests` lists all `OrbitTenant` CRs and returns reconcile requests for any whose `clientSecretRef` points to the changed Secret.

#### 4. Remove `clientSecretEnv` from Registry Format

The `clientSecretEnv` field in the tenant registry YAML written to `orbit-tenants` ConfigMap is no longer needed — the identity service will not read client secrets from env vars. Remove it from `registryOIDC` struct and the ConfigMap builder in `registry.go`.

### Identity Service Changes

#### 1. New Environment Variable

```
ORBIT_TENANT_SECRETS_CONFIG=/etc/orbit/tenant-secrets/client-secrets.yaml
```

Set in the `orbit-identity` Deployment via the Helm chart values.

#### 2. New In-Memory Map and Watcher

In `backend/internal/identity/service.go`:

```go
type Service struct {
    // existing fields ...
    mu             sync.RWMutex
    oidcTenants    map[string]*tenantOIDCState    // existing
    clientSecrets  map[string]string               // NEW: tenantID → OIDC client secret
}
```

Add `watchClientSecrets(ctx context.Context, secretsPath string)` — structurally identical to `watchTenantRegistry`. Watches the **directory** of the mounted file (to catch kubelet's `..data` symlink swap). Debounces 200ms. Calls `reloadClientSecrets` on relevant events.

`reloadClientSecrets` reads `client-secrets.yaml`, unmarshals the flat YAML map, acquires `s.mu.Lock()`, swaps `s.clientSecrets`.

Both watchers are launched from `NewService`:
```go
go svc.watchTenantRegistry(ctx, os.Getenv("ORBIT_TENANTS_CONFIG"))
go svc.watchClientSecrets(ctx, os.Getenv("ORBIT_TENANT_SECRETS_CONFIG"))
```

If `ORBIT_TENANT_SECRETS_CONFIG` is unset or the file does not exist, the watcher is a no-op — public client tenants (no `clientSecretRef`) continue to work with an empty `clientSecrets` map.

#### 3. OIDC Provider Construction

When the identity service constructs the OIDC provider config for a tenant's device auth flow, it reads the client secret from the in-memory map instead of an env var:

```go
// Before:
clientSecret := os.Getenv(tenant.OIDC.ClientSecretEnv)

// After:
s.mu.RLock()
clientSecret := s.clientSecrets[tenantID]
s.mu.RUnlock()
```

### Helm Chart Changes

#### `charts/orbit/templates/deployment-identity.yaml`

1. Add volume mount for the aggregated Secret:
   ```yaml
   volumeMounts:
     - name: tenant-secrets
       mountPath: /etc/orbit/tenant-secrets
       readOnly: true
   ```

2. Add volume referencing the aggregated Secret:
   ```yaml
   volumes:
     - name: tenant-secrets
       secret:
         secretName: orbit-tenant-oidc-secrets
         optional: true   # graceful startup before any confidential tenants exist
   ```
   `optional: true` is critical — if no tenants have `clientSecretRef` set, the Secret may not exist and the pod must still start cleanly.

3. Add env var:
   ```yaml
   env:
     - name: ORBIT_TENANT_SECRETS_CONFIG
       value: /etc/orbit/tenant-secrets/client-secrets.yaml
   ```

#### `charts/orbit-operator/templates/clusterrole.yaml`

Add the cross-namespace Secret read permissions documented in the RBAC section above.

### Tenant Registry Format Migration

The `clientSecretEnv` field is removed from the registry YAML written by the operator. The identity service no longer reads it. This is not a breaking change for public clients (field was unused for them) and is a simplification for confidential clients (env var lookup replaced by in-memory map lookup).

### Migration Path

Existing deployments with no `clientSecretRef` set on any `OrbitTenant` CR are unaffected — the aggregated Secret will not be created (or will be empty), `optional: true` on the volume mount keeps the identity service starting cleanly, and `ORBIT_TENANT_SECRETS_CONFIG` points to a file that does not exist (handled gracefully by the no-op watcher path).

To migrate a tenant to use a confidential client:
1. Create the OIDC client secret as a Kubernetes `Secret` in the tenant namespace (via ESO, Vault Agent, SealedSecret, or manually)
2. Set `spec.oidc.clientSecretRef` on the `OrbitTenant` CR pointing to that Secret
3. The operator reconciles, reads the source Secret, builds/updates `orbit-tenant-oidc-secrets` in `orbit-system`
4. The kubelet propagates the updated Secret volume to the `orbit-identity` pod (10–30s)
5. The `watchClientSecrets` goroutine detects the `..data` event, reloads, logs `"tenant client secrets reloaded"`
6. No pod restart at any step

---

## Consequences

### Positive

- Tenant registration and secret rotation are zero-downtime operations for the identity service
- The hot-reload model is consistent across tenant config (ConfigMap) and secrets (Secret) — same watcher pattern, same kubelet propagation mechanism
- Secret values are never written to ConfigMaps, logs, or events — they remain in Secret resources with restricted RBAC
- Rotation is handled automatically: source Secret update → operator reconcile → aggregated Secret update → kubelet propagation → watcher reload
- `optional: true` on the Secret volume means the identity service starts cleanly even before any confidential tenants are registered

### Negative

- Propagation latency: kubelet updates Secret volumes on its sync period (default 10–30s). There is a brief window during rotation where the old client secret is still active. This is operationally acceptable — OIDC providers support overlapping secrets during rotation periods.
- The operator's ClusterRole requires cross-namespace `get`/`list`/`watch` on `secrets`. This is a broad permission. It is necessary and is the minimum viable grant, but should be documented and reviewed in security audits.
- One additional goroutine per `orbit-identity` pod for the secrets watcher — negligible resource impact.

### Neutral

- `clientSecretEnv` field is removed from the tenant registry YAML format — internal implementation detail, not a user-facing API
- The `tenantOIDCSecretEnvVar()` function in `registry.go` becomes obsolete and should be removed alongside `clientSecretEnv`

---

## Validation

### Success Metrics

- An `OrbitTenant` with `spec.oidc.clientSecretRef` completes device authorization against a confidential OIDC client without a pod restart of `orbit-identity`
- Rotating the source Secret (updating its value) propagates to the identity service within 60 seconds without a pod restart
- Removing `clientSecretRef` from an `OrbitTenant` CR removes the tenant entry from the aggregated Secret within one reconcile cycle
- `orbit-identity` starts cleanly when `orbit-tenant-oidc-secrets` does not exist (no confidential tenants)
- `orbit-identity` logs `"tenant client secrets reloaded"` within 200ms of the kubelet propagating a Secret volume update

### Monitoring

- Log line `"tenant client secrets reloaded"` on each successful reload — observable in structured logs
- Log line `"watching tenant client secrets for changes"` on startup confirms watcher is active
- Operator logs `"reconciled orbit-tenant-oidc-secrets"` after each aggregated Secret write
- Error path: if the operator cannot `GET` a referenced source Secret, it logs the error and sets the tenant condition to `Degraded` with a descriptive message — the aggregated Secret retains the last known good value for that tenant

---

## Related Decisions

- [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) — defines the tenant registry ConfigMap format and hot-reload architecture; this ADR extends the pattern to secrets
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — defines the operator model, `OrbitTenant` CRD, and the `clientSecretRef` field; this ADR implements the gap documented there

---

## References

- [Kubernetes: ConfigMap and Secret volume update propagation](https://kubernetes.io/docs/concepts/configuration/configmap/#mounted-configmaps-are-updated-automatically)
- [fsnotify: directory watching for atomic symlink swaps](https://github.com/fsnotify/fsnotify)
- [OIDC Device Authorization Grant — confidential clients (RFC 8628 §3.1)](https://www.rfc-editor.org/rfc/rfc8628#section-3.1)

---

## Notes

The `optional: true` field on the Secret volume mount is load-bearing — omitting it causes the identity service pod to fail to start if the aggregated Secret does not yet exist (e.g. on a fresh cluster before any `OrbitTenant` with `clientSecretRef` is applied). This must be present in the Helm template.

The source Secret key is fixed at `OIDC_CLIENT_SECRET`. This should be validated by the operator when reading the referenced Secret — if the key is absent, the operator logs a warning and skips that tenant's entry in the aggregated Secret rather than writing an empty value.

**Last Updated:** 2026-06-20

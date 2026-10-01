# Namespace-Scoped Tenants: Letting Multiple Orbit Installs Share a Cluster

<!-- Filename: 2026-09-29-namespace-scoped-tenants.md -->

**Status:** Proposed
**Date:** 2026-09-29
**Deciders:** Constellation platform team (proposed; needs Orbit maintainer sign-off before merge)
**Technical Story:** Constellation's demo Orbit environment needs its own database, isolated from prod's `clearroute` tenant, ideally on the same cluster prod already runs on.

---

## Context

### Background

[Install and Distribution Model](2026-09-04-install-and-distribution-model.md) made `OrbitTenant` cluster-scoped (`charts/orbit-crds/templates/crd-orbittenant.yaml`, `scope: Cluster`), reasoning that "the operator installs cluster-scoped resources on behalf of operators" ([operator ADR](2026-03-31-orbit-kubernetes-operator.md)). That ADR's Non-Goals explicitly exclude multi-cluster, but say nothing about *multiple installs on one cluster* — a narrower, cheaper thing than multi-cluster support, and the thing this decision is actually about.

Auditing the current code confirms cluster scope is load-bearing, not incidental:

- Every operator aggregator (`registry.go`, `connection_aggregator.go`, `mcpserver_aggregator.go`, `oauthclient_aggregator.go`, `secret_aggregator.go`, `tenant_ca_aggregator.go`) and `tenant_controller.go`'s own watch-mapping functions call `client.List(ctx, &OrbitTenantList{})` with no namespace or ownership filter.
- `util.go`'s `tenantNamespace()` derives a tenant's workload namespace globally as `orbit-tenant-<name>`.
- `internal/agents/k8s_owner_references.go`'s `tenantOwnerRef` does a bare, unnamespaced `Get` by tenant ID.
- RBAC is 100% cluster-scoped: two hardcoded `ClusterRole`/`ClusterRoleBinding` pairs (`orbit-operator`, `orbit-agents`), two admission webhooks with literal names and a `namespaceSelector` matching only `orbit.io/role: tenant` (no install qualifier), and a cluster-scoped `PriorityClass` (`orbit-sandbox-headroom`).
- The manager's controller-runtime cache is unrestricted (`cmd/operator/main.go`'s `initManager`).

The Postgres schema is *not* part of the problem: `tenant_id` is a partition column inside composite keys (e.g. `agent_types_tenant_id_slug_key UNIQUE (tenant_id, slug)`), never a cross-database global key, so two installs each with their own database were always going to be safe on that axis.

### Problem Statement

Two independent Orbit installs (each own database, own tenants) cannot coexist on one Kubernetes cluster today. Installing a second `orbit` release would collide on every cluster-scoped object above, and even past that, both operators would aggregate and reconcile *every* `OrbitTenant` on the cluster — install A's operator would write install B's tenants into its own `orbit-tenants` registry ConfigMap, and vice versa.

### Goals

- Let a second (third, ...) independent Orbit install share a cluster with an existing one: own namespace, own database, own tenants, no cross-install visibility.
- Change nothing for anyone who doesn't opt in — today's single-install-per-cluster deployments keep their exact object names and behavior.
- Reuse the install's own system namespace as its identity; no new concept to configure.

### Non-Goals

- Multi-cluster (unchanged from the 2026-09-04 ADR).
- Reducing the operator's cluster-wide RBAC surface for ConfigMaps/Secrets/Pods/Events (tracked separately, issue #269). These span both the system namespace and a dynamic, growing set of tenant namespaces, so unlike `orbittenants` there's no single fixed namespace to scope a Role to. Worse: even a per-tenant Role can't be self-service granted by the operator's own runtime reconcile loop — Kubernetes' RBAC escalation-prevention check requires the granting identity to already hold the rules it's delegating *at that same scope*, and the operator can't hold namespace-scoped permission for a tenant namespace that doesn't exist yet. Shrinking this would need either a higher-privileged, GitOps-applied per-namespace Role (reintroducing a dynamic-namespace problem at the GitOps layer) or dropping the cache for these kinds too and accepting delayed (periodic-reconcile-only) pickup of changes — real engineering, not in scope here.
- Narrowing the manager's cache to only this install's namespaces for kinds other than `OrbitTenant`. Correctness for those (Connection, OAuthClient, MCPServer) is achieved with namespace-derived filtering already present in their aggregators (see Decision); only `OrbitTenant`'s own cache/RBAC is narrowed here, because it alone lives in a single, fixed, known-at-install-time namespace.
- **Sandbox egress isolation between installs.** Discovered installing a real second release side by side: `charts/orbit/templates/egress-gateway/envoyfilter.yaml`'s `egress-gateway-connect-outbound` and `transparent-egress.yaml`'s `egress-transparent-encap` both select `orbit.io/role: sandbox` cluster-wide, with no install qualifier, while routing matched traffic to *this* install's own egress-gateway. A second install's differently-named copy would match every sandbox cluster-wide too — whichever one wins Istio's patch ordering could route another install's tenant sandbox egress through this install's gateway, crossing tenant-CA/credential-brokering boundaries. These two are deliberately left with their original, colliding names (not given `orbit.clusterSuffix` like every other cluster-scoped/root-namespace object in this chart) — the literal-name collision is the only thing stopping a second install from creating them at all today. Real multi-install support here needs `workloadSelector` scoped per-install (e.g. a label the operator stamps on tenant/sandbox namespaces, matching the `orbit.io/system-namespace` convention already used for pod admission) before these two can be duplicated safely. Until then, a second install's sandboxes have no enforced egress chokepoint of their own — treat two-install sandbox egress as unverified/unsafe.

---

## Decision Drivers

- A `CustomResourceDefinition`'s `scope` is immutable once it exists, and a cluster can only ever have one CRD definition per group/kind — so "cluster-scoped" and "namespace-scoped" installs can never coexist on the same cluster. Whatever we do, it's a per-cluster choice, not a per-install one.
- No existing deployment (dev, prod) should need to change anything to keep working exactly as it does today.
- Minimize new concepts: reuse the install's system namespace rather than inventing an "install ID."

---

## Considered Options

### Option 1: Force every cluster to namespace-scoped tenants

**Pros:** One code path, no `global.tenantScope` conditional.
**Cons:** Requires deleting and recreating `orbittenants.orbit.io` (cascade-deletes every existing `OrbitTenant`) on every cluster running Orbit today, whether or not it needs multi-install. Unnecessary blast radius for clusters that will only ever run one install.

### Option 2: Dual-mode, gated by `global.tenantScope` (cluster | namespaced)

**Pros:** Zero behavior change for existing installs (default `cluster`); only a cluster that actually wants multi-install opts in and pays the one-time CRD migration.
**Cons:** A conditional in the CRD template, the Go aggregators, and four RBAC/webhook templates. More surface than Option 1, but each conditional is small and defaults to today's exact behavior.

### Option 3: Full per-install operator (separate controller-runtime cache/manager, no shared reconcile loop)

**Pros:** Strongest isolation — installs literally can't see each other's objects, cache included, from day one.
**Cons:** Substantially more work (dynamic per-namespace informer management in `cmd/operator/main.go`) for no correctness benefit over Option 2 — a reconcile-time predicate already prevents cross-install reconciliation with a cluster-wide cache. Better done as a later, separable hardening pass once Option 2 is proven.

---

## Decision

**Chosen Option:** Option 2 — dual-mode, gated by `global.tenantScope`.

### Rationale

Option 1 forces a destructive migration on clusters that never asked for multi-install. Option 3 solves a problem (cache-level isolation) that a predicate already solves at the reconcile level, for much more implementation cost. Option 2 is the minimal change that makes multi-install possible without disturbing anyone who doesn't need it, and leaves Option 3's cache-narrowing available as a future, independent hardening step.

---

## Consequences

### Positive

- A cluster can run N independent Orbit installs (own database, own tenants, own trust domain if desired), each identified by its own system namespace.
- Existing single-install clusters (dev, prod) render byte-identical object names and env vars by default — verified via `helm template` diff between default and `tenantScope: namespaced` renders.

### Negative

- `orbittenants.orbit.io` scope is immutable: a cluster that later wants multi-install must delete and recreate the CRD, destroying every existing `OrbitTenant` object first (reapplied immediately after with the same spec — no workload disruption, but a real migration step, not a config flip).
- `global.tenantScope` must be set consistently across the `orbit` and `orbit-crds` releases (two separate Helm charts/releases) and across `orbit-agents`' own copy of the setting (`internal/agents` cannot import `internal/operator`, so it carries an independent `TenantScope`/`TenantNamespacePrefix` pair that must agree with the operator's).

### Neutral

- `orbittenants`/`orbittenants/status` and the leader-election `Lease` do shrink to a namespaced `Role`/`RoleBinding` in namespaced mode (both live in exactly one, fixed, install-time-known namespace — the system namespace). Everything else the operator touches (ConfigMaps, Secrets, Pods, Events, Connection, OAuthClient, MCPServer, NetworkPolicy, AuthorizationPolicy, DestinationRule) stays on the `ClusterRole` in both modes, per the Non-Goals above.

---

## Implementation

- `charts/orbit-crds`: `global.tenantScope` (default `cluster`) drives `crd-orbittenant.yaml`'s `scope:` field.
- `charts/orbit`: same `global.tenantScope` value (must match the `orbit-crds` release it's paired with) drives:
  - `ORBIT_TENANT_SCOPE` env var on the operator (`templates/operator/deployment.yaml`) and orbit-agents (`templates/agents/deployment.yaml`), read by `backend/internal/operator/settings.go`'s `tenantScope`/`namespacedTenants()` and `backend/cmd/orbit-agents/config`'s `Kubernetes.TenantScope` respectively.
  - `ORBIT_TENANT_NAMESPACE_PREFIX` on orbit-agents in namespaced mode only, set to `<systemNamespace>-tenant-`, matching the operator's own `tenantNamespace()` derivation (`internal/operator/util.go`) so both binaries agree on where a tenant's workloads live.
  - A `{{ include "orbit.clusterSuffix" . }}` template helper (empty in cluster mode, `-<systemNamespace>` in namespaced mode) applied to the four cluster-scoped object kinds: `ClusterRole`/`ClusterRoleBinding` `orbit-operator` and `orbit-agents`, both admission webhook configs, and the `orbit-sandbox-headroom` `PriorityClass`.
  - An `orbit.io/system-namespace: <systemNamespace>` label, always stamped on every tenant namespace by `tenant_controller.go`'s `tenantNamespaceLabels()`, matched by both webhooks' `namespaceSelector` in namespaced mode only, so one install's webhook never admits another's tenant pods.
  - `templates/operator/namespaced-rbac.yaml` (new, namespaced mode only): a `Role`/`RoleBinding` in the system namespace granting `orbittenants`/`orbittenants/status`/leader-election `Lease` — carved out of `clusterrole.yaml`'s existing rules, which keep everything else (ConfigMaps, Secrets, Pods, Events, Connection, OAuthClient, MCPServer, NetworkPolicy, AuthorizationPolicy, DestinationRule) cluster-wide in both modes, per the Non-Goals above. Applied by Helm/ArgoCD, not created by the operator's own reconcile loop — see the Non-Goals section on why the operator can't self-grant this for a tenant namespace.
- `backend/cmd/operator/main.go`'s `initManager`: in namespaced mode, `cache.Options.ByObject` restricts the `OrbitTenant` watch to `install.SystemNamespace` only — required once its RBAC actually shrinks, since an unrestricted cache would otherwise try (and fail) to watch it cluster-wide with no permission to do so. No other watched kind is restricted.
- `backend/internal/operator`: every raw `client.List(ctx, &OrbitTenantList{})` call now takes `tenantListOpts()` (adds `client.InNamespace(systemNamespace)` in namespaced mode, no-op in cluster mode). `tenant_controller.go`'s `SetupWithManager` adds `tenantNamespacePredicate()` to its `For(&OrbitTenant{})` builder, dropping reconcile events for another install's `OrbitTenant` objects even though the cache remains cluster-wide.
- `backend/internal/agents/k8s_owner_references.go`: `tenantOwnerRef` skips ownership entirely (returns the zero value) in namespaced mode, rather than fetching anything. Cross-namespace owner references aren't supported by Kubernetes' garbage collector — it treats such an owner as absent, which can delete the dependent — and every caller applies the returned reference to resources in the tenant's own workload namespace, never the system namespace where `OrbitTenant` now lives. `charts/orbit/templates/agents/clusterrole.yaml`'s `orbittenants: get` grant is dropped in namespaced mode to match (the code path that used it no longer runs).
- `backend/internal/operator/mcpserver_controller.go`: `MCPServerReconciler.Reconcile` checks `belongsToThisInstall` (this install's system namespace, or a tenant namespace carrying its `orbit.io/system-namespace` label) before patching status or creating/owning NetworkPolicy/AuthorizationPolicy/DestinationRule objects — `MCPServer` itself stays cluster-wide cached/watched in both modes (see Non-Goals), so without this check one install's operator would mutate another's MCPServers, not just read them.
- `backend/internal/operator/tenant_controller.go`'s `connectionToTenants`/`oauthClientToTenants` watch-mapping functions now carry the matched tenant's `Namespace` on the generated `reconcile.Request`, not just its `Name` — needed once `OrbitTenant` is itself namespaced, or the subsequent `Get` looks up an empty namespace and silently finds nothing.
- `backend/internal/operator/webhookcert`: `Webhooks.Mutating`/`Validating` (consumed by the cert manager, drift reconciler, and preflight's webhook checks) are no longer fixed constants — `Configure()` overrides them at startup from `cmd/operator/main.go` to the chart's actual rendered names (with `orbit.clusterSuffix` applied) in namespaced mode, so these components patch/check the objects that actually exist instead of the unsuffixed literals.

### Migration Path

A cluster opting into `tenantScope: namespaced` for the first time:

1. `kubectl get orbittenant <name> -o yaml` for every existing tenant (backup).
2. Delete and recreate `orbittenants.orbit.io` with the new chart values (destroys existing `OrbitTenant` objects — expected, confirmed acceptable for this migration).
3. Reapply each backed-up tenant spec, now namespaced into the install's own system namespace (pin `spec.namespace` explicitly to today's resolved namespace so existing tenant workload namespaces are unaffected).
4. Verify the `orbit-tenants` registry ConfigMap and identity login before adding a second install.

### Timeline

Land the code/chart change first (this ADR), gated off by default. The first cluster to actually flip to `namespaced` mode is Constellation's prod cluster, to add a demo tenant install — tracked in `constellation-iac`, not here.

---

## Validation

### Success Metrics

- `helm template` diff between default values and `tenantScope: namespaced` values shows only the intended differences: the four cluster-scoped kinds gain the `orbit.clusterSuffix`; a new namespaced `Role`/`RoleBinding` (`namespaced-rbac.yaml`) and `orbit.io/system-namespace` webhook `namespaceSelector` clause appear; and three env var entries differ (`ORBIT_TENANT_SCOPE` on both operator and agents, `ORBIT_TENANT_NAMESPACE_PREFIX` on agents). No namespaced object *other than* that new `Role`/`RoleBinding` changes name or content.
- A rendered two-install test (two system namespaces, two `OrbitTenant`s, same tenant name in each) shows each install's `orbit-tenants` ConfigMap containing only its own tenant.

### Monitoring

- Existing operator/agents unit test suites (`internal/operator/...`, `internal/agents/...`) pass unchanged in default mode — regression signal for accidental behavior change to the existing single-install path.

---

## Related Decisions

- [install-and-distribution-model](2026-09-04-install-and-distribution-model.md) — made `OrbitTenant` cluster-scoped in the first place; this ADR amends that choice to be configurable rather than fixed.
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — original cluster-scope rationale ("the operator installs cluster-scoped resources on behalf of operators"), superseded on the Istio/SPIRE point by the 2026-09-04 ADR already; this ADR narrows it further to just `OrbitTenant`.

---

## Notes

Constellation-iac's Orbit demo-tenant work is downstream of this landing and releasing — see that repo's PR onboarding the demo tenant for the consuming side.

**Last Updated:** 2026-09-29

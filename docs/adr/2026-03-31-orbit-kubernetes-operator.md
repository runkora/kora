# kora Kubernetes Operator

**Status:** Superseded by [install-and-distribution-model](2026-09-04-install-and-distribution-model.md)
**Date:** 2026-03-31
**Deciders:** Engineering Team
**Technical Story:** [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) established two Helm charts as the distribution artifact for kora. The current installation experience requires operators to use Helmfile, manage install ordering (SPIRE CRDs → SPIRE → Istio → orbit-platform → orbit), imperatively create ConfigMaps and Secrets, and understand the dependency graph to add a tenant. As kora targets both self-hosted enterprise clusters and our own production EKS deployment, this complexity is an adoption barrier and an operational gap. A Kubernetes operator that manages the complete platform lifecycle — from SPIRE and Istio installation through to tenant provisioning — closes both gaps with a single CRD surface. The operator owns the full release sequence; Helmfile is no longer required.

---

## Context

### Background

[helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) defined the two-chart model (`charts/orbit-platform` + `charts/orbit`) and established `deploy/local/` as the reference operator deployment. It deferred chart publishing and operator tooling as "release pipeline concerns". That deferral was correct at the time — the chart structure needed to be validated locally first. The charts are now validated and working.

The current operator experience is:

1. Write six sequential `helm upgrade --install` commands in the correct dependency order
2. Imperatively create `orbit-config` ConfigMap in each namespace (with the right key-value pairs)
4. Imperatively create `kora-secrets` Secret in each namespace (from a secrets manager or manually)
5. Imperatively create `kora-tenants` ConfigMap from the tenant registry file
5. To add a tenant: create a namespace manually, add it to `platform-values.yaml`, re-run `helm upgrade orbit-platform`, add config to `config/tenants.yaml`, re-apply `kora-tenants` ConfigMap, restart the identity service
7. To upgrade kora: coordinate a version bump across two charts with a dependency between them

This is not a viable operational model for self-hosted enterprise deployments, and it is not the model we want for our own EKS deployment. Every step above is a potential misconfiguration.

### The Two Deployment Contexts

The operator must work in both contexts:

**Local development (k3d):** Engineers run `make dev-cluster` today to create the k3d cluster. With the operator, the next step is `make cluster-operator` (installs the bootstrap chart) followed by `kubectl apply -f deploy/local/orbit-platform.yaml`. The operator installs SPIRE, Istio, and the kora charts in the correct order — no Helmfile, no `make cluster-platform`. The same `kubectl` workflow used in EKS is exercised locally from day one.

**Production (EKS and customer self-hosted):** Operators have their own GitOps tooling (ArgoCD, Flux, or Helmfile). They install the operator bootstrap chart once, then declare `OrbitPlatform` and `KoraTenant` resources in their repository. Git becomes the source of truth for platform state. Secrets come from their existing secret management solution.

### How [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) and [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) Relate

[tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) defines the tenant registry as an operator-mounted YAML file (`config/tenants.yaml` in local dev, a ConfigMap-mounted file in cluster). That model is correct and is not replaced — it is the **identity service's view** of tenants: a file it reads at startup and watches for changes.

The operator is the **cluster management view** of tenants: it manages the namespace, the per-namespace Secrets and ConfigMaps, OIDC configuration validation, and ultimately writes the ConfigMap that the identity service reads. The two ADRs describe different layers of the same system:

```
KoraTenant CR (operator input)
        ↓  operator reconciler
kora-tenants ConfigMap ([tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) tenant registry file)
        ↓  identity service reads
Per-tenant OIDC provider ([tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) runtime)
```

### Secret Management Across Environments

Secrets (DB password, JWT signing key, per-tenant OIDC client secrets) must work in two modes:

**Local development:** Secrets are created by `scripts/cluster/create-secrets.sh` from environment variables. This is acceptable for local dev — engineers run `make cluster-secrets` once before applying the `OrbitPlatform` CR. The operator reads from these pre-existing Secrets.

**Production (EKS / self-hosted):** Secrets come from external secret management. The three common patterns are:
- **External Secrets Operator (ESO)** — a separate controller that syncs secrets from AWS Secrets Manager, Vault, GCP Secret Manager, etc. into Kubernetes `Secret` objects. The orbit operator reads the resulting `Secret` — it does not care how the `Secret` was populated.
- **Vault Agent / CSI provider** — injects secrets directly into pods or creates `Secret` objects. Same result: operator reads a pre-existing `Secret`.
- **Manual / GitOps sealed secrets** — `SealedSecret` or SOPS-encrypted secrets in the deployment repository. Same result.

The operator's contract is: **it reads `secretRef` references pointing at pre-existing `Secret` objects. It does not own secret creation, rotation, or sourcing.** This makes it compatible with every secrets management solution without any integration work.

This means the operator works identically across all environments — the only difference is HOW the `Secret` was populated before the operator ran.

### What Stays the Same

The Helm charts (`charts/orbit-platform/` and `charts/orbit/`) are not replaced by the operator. The operator **drives them** using the Helm Go SDK (`helm.sh/v3`). All the template logic, values API, and resource definitions remain in the charts. The operator is an installation and lifecycle management layer on top of them.

### Problem Statement

We need:
1. A single CRD (`OrbitPlatform`) that declares the desired state of the kora platform in a cluster
2. A CRD (`KoraTenant`) that declares a tenant — its OIDC configuration, the namespace it operates in, and references to its secrets
3. A controller (the operator) that reconciles those CRDs against cluster state, driving the Helm charts and managing all ancillary resources
4. A bootstrap chart (`charts/kora-operator/`) that is the only thing operators need to install manually — everything else is managed by the CRDs
5. A secret model that works with environment variables locally and with any secrets manager in production

### Goals

- Operators deploy the entire kora platform by applying two things: the operator bootstrap chart, and an `OrbitPlatform` CR — the operator handles SPIRE, Istio, and all kora charts from there
- Adding a tenant requires applying one `KoraTenant` CR — no namespace creation, no ConfigMap patching, no manual helm commands
- The operator works identically in local k3d and production EKS — same CRDs, same controller binary, no divergent tooling paths
- Secrets are managed via `secretRef` pointing at pre-existing `Secret` objects — compatible with ESO, Vault, manual, or any other secret source
- Upgrade kora by bumping `spec.version` in the `OrbitPlatform` CR — the operator sequences all chart upgrades in dependency order
- `deploy/local/` and the Makefile remain the local dev workflow — they install the bootstrap chart and apply CRDs using `kubectl apply`
- Existing `charts/orbit-platform/` and `charts/orbit/` are unchanged — the operator drives them, not replaces them

### Non-Goals

- Building a web UI or API for tenant self-service registration — this is infrastructure for operators, not end users
- Replacing the identity service's runtime tenant registry model ([tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)) — the operator populates the ConfigMap that the identity service reads; [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) governs how the identity service consumes it
- Secret rotation or secret creation — the operator reads pre-existing Secrets; sourcing and rotating them is the operator's responsibility
- Multi-cluster federation — each cluster has its own `OrbitPlatform` instance
- Supporting clusters with no service mesh (Istio) or no workload identity (SPIRE) — [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) Non-Goals apply here too

---

## Decision Drivers

* **Operational simplicity**: Six Helmfile releases and imperative ConfigMap/Secret creation is not viable for self-hosted enterprise operators
* **Tenant lifecycle**: Adding a tenant today requires coordinated changes across three config surfaces (Helmfile values, tenants.yaml, Secret creation) — it should be one CRD apply
* **Environment parity**: The same operational model must work in k3d and EKS — not two divergent workflows
* **Secrets compatibility**: Must work with ESO, Vault Agent, manually created Secrets, and local env-var-sourced Secrets — no single secret management opinion
* **Chart preservation**: Two years of chart development and local validation should not be discarded — the operator drives the charts, it does not replace them
* **GitOps compatibility**: All state must be declarable as Kubernetes resources committed to a deployment repository — `OrbitPlatform` and `KoraTenant` CRs fulfil this
* **Upgrade safety**: Bumping a chart version across two dependent charts must be sequenced and validated — the operator's reconciler provides this

---

## Considered Options

### Option A: Helmfile + documented operator guide (status quo)

**Description:** Publish Helmfile as a reference template (with OCI chart refs replacing local paths). Document the install sequence and imperative commands. Operators follow the guide.

**Pros:**
- No new code to write
- Operators who already use Helmfile, ArgoCD, or Flux can adapt the pattern
- No new CRDs in the cluster

**Cons:**
- Every operator must understand and replicate the six-release dependency graph
- Adding a tenant is a multi-step manual process — a class of misconfigurations that will occur in practice
- No validation that the operator's values are consistent (e.g. trust domain mismatch between orbit-platform and orbit)
- Cannot work identically in local k3d and production EKS without two divergent tooling paths
- SPIRE/Istio enforcement is documentation, not code — operators who skip steps get silent failures

### Option B: kora Kubernetes Operator with `OrbitPlatform` and `KoraTenant` CRDs (chosen)

**Description:** A `controller-runtime`-based Go operator that watches `OrbitPlatform` and `KoraTenant` custom resources and reconciles cluster state. It installs/upgrades the Helm charts in the correct order, manages per-tenant namespaces and secrets, validates OIDC configuration, and surfaces health via CR status fields. A minimal bootstrap chart (`charts/kora-operator/`) installs the operator itself.

**Pros:**
- Single operational model for local dev and production — same CRDs, same controller
- Adding a tenant is a single `kubectl apply` — operator handles namespace, ConfigMap, Secret projection, and identity service registry update
- When `spec.spire.enabled: true` or `spec.istio.enabled: true`, the operator installs them — no external prerequisite. When set to false (pre-existing installation), the reconciler validates they are present before proceeding. Either way, enforcement is code, not documentation.
- Upgrade sequencing is the reconciler's responsibility — operators bump `spec.version`
- Status conditions on the CR give operators full visibility (`kubectl get orbitplatform`)
- Fully GitOps-compatible — `OrbitPlatform` and `KoraTenant` CRs are committed to the deployment repository
- Compatible with any secret management solution via `secretRef`
- Existing charts are unchanged — the operator adopts them as-is

**Cons:**
- Significant implementation scope (~4 weeks, detailed in Implementation section)
- Adds `controller-runtime` and `helm.sh/v3` as new dependencies to the Go module
- Broad RBAC permissions required — the operator needs cluster-wide write access to manage namespaces, ClusterRoles, and CRDs
- A new binary (`cmd/operator/`) to build, test, and publish alongside the existing binaries

### Option C: Terraform / Pulumi provider

**Description:** An infrastructure-as-code provider that manages the kora platform as Terraform or Pulumi resources.

**Pros:**
- Operators already familiar with Terraform have a low adoption barrier
- State management and drift detection built in

**Cons:**
- Does not fit the Kubernetes-native GitOps model — `kubectl apply` is the universal primitive; Terraform is a separate toolchain
- Terraform state files introduce a second source of truth alongside the Kubernetes API
- Does not run inside the cluster — no continuous reconciliation, only point-in-time applies
- Cannot react to Kubernetes events (e.g. SPIRE pod becoming unhealthy)
- Two toolchains to maintain (Terraform provider + Helm charts)

---

## Decision

**Chosen Option:** Option B — kora Kubernetes Operator with `OrbitPlatform` and `KoraTenant` CRDs

### Rationale

The operator model is the correct abstraction for a platform with mandatory infrastructure dependencies (SPIRE, Istio), multiple installation surfaces (six Helm releases), and a tenant lifecycle that involves coordinated changes across namespaces, ConfigMaps, and Secrets. Option A (Helmfile guide) delegates this complexity to every operator individually — the bugs will be in the operators' configurations, not in ours. Option B puts the complexity in one place and tests it once.

The decision to drive the existing Helm charts (rather than replacing them with raw `client-go` resource management) is deliberate: the charts encode the values API that operators already understand. The operator is a lifecycle management layer, not a reimplementation.

---

## CRD Design

### `OrbitPlatform` — Cluster-scoped

One per cluster. Declares the desired state of the entire kora platform.

```yaml
apiVersion: runkora.io/v1alpha1
kind: OrbitPlatform
metadata:
  name: orbit                       # conventionally "orbit"; one per cluster
spec:
  # Chart version to install. The operator sequences upgrades across all
  # releases when this is bumped. Must be a published chart version.
  version: "0.1.0"

  # SPIFFE trust domain. Must be consistent across SPIRE and the identity service.
  trustDomain: "orbit.prod.example.com"

  # Controls whether the operator installs SPIRE via the orbit-platform chart.
  # Set to false if SPIRE is already installed in the cluster.
  spire:
    enabled: true

  # Controls whether the operator installs Istio via the orbit-platform chart.
  # Set to false if Istio is already installed in the cluster.
  istio:
    enabled: true

  # Secrets for the Orbit identity service. The operator reads these Secrets
  # and projects them into kora-system. The Secrets must exist before the
  # operator reconciles — they are not created by the operator.
  secrets:
    # Secret containing: DB_PASSWORD, REDIS_PASSWORD, KORA_TOKEN_SIGNING_KEY
    platformSecretRef:
      name: orbit-platform-secrets
      namespace: kora-system       # defaults to kora-system if omitted

  # Non-secret platform configuration. Merged with chart defaults.
  config:
    db:
      host: "orbit-postgres.prod.example.com"
      name: "orbit"
      port: 5432
      user: "orbit"
    redis:
      host: "elasticache.prod.example.com"
      port: 6379

  # Escape hatch: raw Helm values merged (last-write-wins) into orbit-platform
  # and orbit chart values. Use sparingly — prefer structured spec fields.
  platformValues: {}
  orbitValues: {}

  # Upgrade strategy. See [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) for rationale.
  upgradeStrategy:
    mode: Explicit                  # Explicit | Automatic
    # Explicit: only upgrade when spec.version is changed.
    # Automatic: operator polls the chart registry and upgrades when a new
    #            patch version (semver patch only) is published. Minor/major
    #            bumps always require explicit spec.version change.

status:
  phase: Ready                      # Pending | Installing | Upgrading | Ready | Degraded | Failed
  version: "0.1.0"                  # installed version
  conditions:
    - type: PlatformReady
      status: "True"
    - type: SPIREReady
      status: "True"
    - type: IstioReady
      status: "True"
    - type: IdentityServiceReady
      status: "True"
  tenantCount: 3
  lastReconciledAt: "2026-06-15T10:00:00Z"
```

### `KoraTenant` — Cluster-scoped

One per tenant. Declares a tenant's existence, its OIDC configuration, and references to its secrets.

```yaml
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: acme-corp                   # tenant ID — used in the tenant registry
spec:
  # Human-readable display name surfaced in the identity service tenant registry.
  displayName: "ACME Corporation"

  # Whether this tenant is active. The operator sets state: suspended in the
  # generated tenant registry entry when this is false, without deleting any
  # cluster resources.
  active: true

  # The namespace this tenant's workloads run in.
  # The operator creates this namespace with the correct labels:
  #   runkora.io/role: tenant
  #   istio-injection: enabled
  #   spiffe.io/spire-managed: "true"
  namespace: "acme-corp-tenant"

  # OIDC configuration for this tenant. Binds the tenant to a specific IdP.
  oidc:
    issuerURL: "https://acme.okta.com/oauth2/default"
    clientID: "kora-platform"
    # secretRef points at a pre-existing Secret with key OIDC_CLIENT_SECRET.
    # For public clients (device flow only), omit this field.
    # The Secret can be created by ESO, Vault Agent, SealedSecret, or manually —
    # the operator does not care how it was populated.
    clientSecretRef:
      name: acme-oidc-secret
      namespace: "acme-corp-tenant"
    scopes:
      - openid
      - profile
      - email
    # Optional claim-based tenant membership scoping (see [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)).
    # claimBinding:
    #   claim: "hd"
    #   value: "acme.com"

  # Note: database credentials and other application secrets for tenant workloads
  # are NOT managed by the Orbit operator. They are the tenant's own concern —
  # managed via ESO, Vault Agent, SealedSecrets, or whatever the tenant operator
  # chooses. The Orbit operator only manages Orbit platform secrets (OIDC client
  # secret, signing keys) — not the tenant's application infrastructure.

status:
  phase: Ready                      # Pending | Provisioning | Ready | Degraded | Suspended
  namespace: "acme-corp-tenant"
  lastReconciledAt: "2026-06-15T10:05:00Z"
  conditions:
    - type: NamespaceReady
      status: "True"
    - type: RegistryUpdated
      status: "True"
```

### How `KoraTenant` Drives [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)

The operator maintains the `kora-tenants` ConfigMap consumed by the identity service ([tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)). When an `KoraTenant` CR is created, updated, or deleted, the operator:

1. Creates/updates the tenant namespace with the required labels (`runkora.io/role: tenant`, `istio-injection: enabled`, `spiffe.io/spire-managed: "true"`)
2. Rebuilds the `kora-tenants` ConfigMap in `kora-system` by assembling the current state of all `KoraTenant` CRs into the [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) tenant registry YAML format
3. Marks the tenant `Ready` and records `status.lastReconciledAt` — the identity service picks up ConfigMap changes via its file watcher

The operator does NOT manage the tenant's own application secrets (database passwords, API keys, etc.). Those are the tenant's infrastructure concern, managed independently of kora.

[tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)'s `config/tenants.yaml` file format is preserved exactly — the operator writes it, rather than a human editing it manually.

---

## Architecture

### Component: `cmd/operator/`

A new binary in the existing multi-binary structure. Follows all existing conventions (no global loggers, `*logging.Logger` injected, `pgx/v5` for any state, `config.Load[T]` for config). Uses `controller-runtime` for the controller loop.

```
backend/cmd/operator/
├── main.go               # controller-runtime manager setup, leader election
├── config/
│   ├── base.yaml         # operator service config (port, leader election, metrics)
│   └── local.yaml        # local overrides
internal/operator/        # feature package (follows [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) structure)
├── platform_controller.go    # reconciler for OrbitPlatform
├── tenant_controller.go      # reconciler for KoraTenant
├── helm.go               # Helm SDK wrapper (install/upgrade/status)
├── registry.go           # builds kora-tenants ConfigMap from KoraTenant CRs
├── models.go             # Go types for CRD spec/status
└── rbac.go               # generates per-tenant RBAC
```

### Bootstrap Chart: `charts/orbit-operator/`

The only thing an operator installs manually. ~5 resources:

```
charts/kora-operator/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── deployment.yaml           # the operator binary
    ├── serviceaccount.yaml
    ├── clusterrole.yaml          # broad permissions — see RBAC section
    ├── clusterrolebinding.yaml
    └── crd-orbitplatform.yaml    # OrbitPlatform CRD
    └── crd-koratenant.yaml      # KoraTenant CRD
```

Install sequence from the operator's perspective:

```bash
# Step 1: install the operator (one time)
helm upgrade --install kora-operator oci://ghcr.io/orbitlabs/charts/kora-operator \
  --version 0.1.0 \
  --namespace kora-operator --create-namespace

# Step 2: declare the platform (all future management via kubectl)
kubectl apply -f orbitplatform.yaml
kubectl apply -f tenants/acme-corp.yaml
kubectl apply -f tenants/globex.yaml
```

That is the entire operator experience. No Helmfile. No imperative ConfigMap creation.

### Upgrade Strategy Decision

The [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) model (operator bumps `spec.version`) is Explicit mode. This is the default and is correct for most deployments: upgrades are a deliberate act, coordinated with tenant workload owners.

Automatic mode (patch-only) is available for operators who want automated security patch delivery. The operator polls the chart registry at a configurable interval and upgrades when a new patch version is available. Minor and major version bumps always require an explicit `spec.version` change — they may have breaking changes in values API or behaviour.

**Default is `Explicit`.** Operators opt into `Automatic` by setting `upgradeStrategy.mode: Automatic`.

### RBAC

The operator requires broad permissions because it installs cluster-scoped resources on behalf of operators. This is unavoidable — an operator that can create namespaces, ClusterRoles, and CRDs must have those permissions itself.

```yaml
rules:
  # Helm release state (Helm stores releases as Secrets)
  - apiGroups: [""]
    resources: ["secrets", "configmaps", "namespaces", "serviceaccounts"]
    verbs: ["*"]
  # CRD management (SPIRE CRDs)
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["*"]
  # ClusterSPIFFEID and SPIRE resources
  - apiGroups: ["spiffe.spiffe.io"]
    resources: ["*"]
    verbs: ["*"]
  # Istio resources
  - apiGroups: ["networking.istio.io", "install.istio.io"]
    resources: ["*"]
    verbs: ["*"]
  # RBAC for tenant namespaces
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["clusterroles", "clusterrolebindings", "roles", "rolebindings"]
    verbs: ["*"]
  # Own CRDs
  - apiGroups: ["runkora.io"]
    resources: ["orbitplatforms", "orbitplatforms/status", "koratenants", "koratenants/status"]
    verbs: ["*"]
  # Apps (Deployments, DaemonSets — for SPIRE, Istio, Orbit services)
  - apiGroups: ["apps"]
    resources: ["deployments", "daemonsets", "statefulsets"]
    verbs: ["*"]
  # Networking
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    verbs: ["*"]
```

This is standard for an operator that manages platform-level infrastructure. It is constrained to the kora-operator `ServiceAccount` — it does not affect other workloads in the cluster.

### Local Development with the Operator

The operator replaces both `make cluster-platform` (Helmfile) and `make cluster-deploy` (imperative kubectl/helm). The full local dev sequence becomes:

```bash
make dev-cluster         # create k3d cluster — unchanged

make cluster-secrets     # create-secrets.sh: populates orbit-platform-secrets Secret
                         # (local only; production uses ESO/Vault/SealedSecrets)

make cluster-operator    # helm upgrade --install kora-operator charts/kora-operator
                         # --namespace kora-operator --create-namespace
                         # (operator pod runs with sidecar injection disabled — it installs Istio)

kubectl apply -f deploy/local/orbit-platform.yaml
                         # OrbitPlatform CR: spec.spire.enabled: true, spec.istio.enabled: true
                         # operator sequences: spire-crds → spire → istio-base → istiod →
                         #                     gateway → orbit-platform → orbit
                         # waits for readiness at each step

kubectl apply -f deploy/local/tenants/local.yaml
                         # KoraTenant CR for the local dev tenant
                         # operator provisions namespace, validates OIDC, rebuilds registry
```

The operator's `OrbitPlatform` reconciler manages the full release dependency chain:

```
spire-crds      (helm upgrade --install, namespace: spire-system)
     ↓ wait: CRDs registered
spire           (helm upgrade --install, namespace: spire-system)
     ↓ wait: spire-agent DaemonSet Ready
istio-base      (helm upgrade --install, namespace: istio-system)
     ↓
istiod          (helm upgrade --install, namespace: istio-system)
     ↓ wait: istiod Deployment Ready
gateway         (helm upgrade --install, namespace: istio-system)
     ↓
orbit-platform  (helm upgrade --install, namespace: kora-system)
     ↓
orbit           (helm upgrade --install, namespace: kora-system)
     ↓ wait: kora-identity Deployment Ready
```

The operator pod itself runs with `sidecar.istio.io/inject: "false"` — it installs Istio, it does not depend on it.

`deploy/local/orbit-platform.yaml` is the `OrbitPlatform` CR for the local k3d cluster. It sets `spec.spire.enabled: true`, `spec.istio.enabled: true`, references the local registry image (`orbit-registry:5001/kora-identity:latest`), and sets `APP_ENV: local` via `spec.orbitValues`.

`scripts/cluster/create-secrets.sh` is retained for local dev — it creates the `orbit-platform-secrets` Secret that the `OrbitPlatform` CR references via `spec.secrets.platformSecretRef`. For EKS/production, operators use ESO, Vault Agent, or their preferred solution.

---

## Consequences

### Positive

- **One install command, two files**: Operators go from a six-step Helmfile sequence to `helm install kora-operator` + `kubectl apply orbitplatform.yaml`
- **Tenant lifecycle is a single CR apply**: No more coordinated changes across Helmfile values, tenants.yaml, and Secret creation
- **Environment parity**: Local k3d and production EKS use identical CRDs and the same controller binary — no divergent tooling paths
- **Prerequisite enforcement in code**: The reconciler validates SPIRE and Istio are present before proceeding. Silent failures from missing prerequisites are eliminated.
- **OIDC validation at CR apply time**: The operator validates OIDC issuer reachability when `KoraTenant` is applied — operators get immediate feedback, not a runtime failure hours later
- **GitOps-native**: `OrbitPlatform` and `KoraTenant` CRs are standard Kubernetes resources committable to any GitOps repository
- **Secret management flexibility**: Works with ESO, Vault Agent, SealedSecrets, or manual creation — operator reads `secretRef`, does not own secret sourcing
- **Status visibility**: `kubectl get orbitplatform` and `kubectl get koratenant` give operators full health visibility without `helm list` or log tailing

### Negative

- **Significant implementation scope**: ~4 weeks of focused engineering (see Implementation section for breakdown)
- **Broad RBAC**: The operator requires cluster-admin-equivalent permissions for its `ServiceAccount` — this is standard for platform operators but must be clearly documented and scoped to the operator's SA
- **New dependencies**: `controller-runtime` and `helm.sh/v3` (Helm Go SDK) are new additions to `backend/go.mod`
- **Operator availability is a new failure mode**: If the operator's pod is down, Helm upgrades and tenant additions are blocked until it recovers. Leader election mitigates this (the operator supports multi-replica with leader election).
- **Operator must bootstrap before Istio exists**: The operator installs Istio, so it must run without a sidecar. This is handled by `sidecar.istio.io/inject: "false"` on the operator Deployment — it is a permanent annotation, not a temporary workaround.

### Neutral

- `deploy/local/` structure changes: values files are supplemented by `OrbitPlatform` and `KoraTenant` CRs; `make cluster-platform` is replaced by `make cluster-operator` + `kubectl apply`
- `scripts/cluster/create-secrets.sh` is retained for local dev — it creates the Secret that `spec.secrets.platformSecretRef` points at
- Tenant application secrets (DB passwords, API keys) are explicitly out of scope — the operator manages only kora platform secrets (OIDC client secrets, signing keys)

---

## Implementation

### Phase 1: CRD types and operator scaffold (~1 week)

1. Add `controller-runtime` and `helm.sh/v3` to `backend/go.mod`
2. Create `backend/internal/operator/` package with `OrbitPlatform` and `KoraTenant` Go types (spec, status, conditions)
3. Create `backend/cmd/operator/main.go` — controller-runtime manager with leader election, metrics endpoint, health probe
4. Create `charts/kora-operator/` bootstrap chart — CRD yamls, Deployment, ServiceAccount, ClusterRole, ClusterRoleBinding
5. Register both controllers in the manager (empty reconcile stubs)
6. Write unit tests for type marshalling and status condition helpers

### Phase 2: `OrbitPlatform` reconciler — happy path (~1 week)

1. Implement `helm.go` wrapper — wraps `helm.sh/v3/pkg/action` for install, upgrade, status, rollback, and readiness polling
2. Implement `OrbitPlatform` reconciler with full release sequencing:
   - If `spec.spire.enabled: true`: install `spire-crds`, wait for CRDs to register, install `spire`, wait for DaemonSet Ready
   - If `spec.spire.enabled: false`: validate SPIRE is already present; set `SPIREReady: False` with a descriptive message if not
   - If `spec.istio.enabled: true`: install `istio-base`, install `istiod`, wait for Deployment Ready, install `gateway`
   - If `spec.istio.enabled: false`: validate Istio is already present; set `IstioReady: False` with a descriptive message if not
   - Install/upgrade `orbit-platform` chart with values derived from `spec`
   - Install/upgrade `orbit` chart with values derived from `spec`; wait for `kora-identity` Deployment Ready
   - Update `status.phase` and all status conditions at each step
3. Implement upgrade sequencing: when `spec.version` changes, replay the full sequence in order — each step waits for the previous to be healthy before proceeding
4. Write integration tests against a real k3d cluster (or `envtest`)

### Phase 3: `KoraTenant` reconciler — happy path (~1 week)

1. Implement `registry.go` — assembles `kora-tenants` ConfigMap content from all `KoraTenant` CRs in the [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) YAML format
2. Implement `KoraTenant` reconciler:
   - Create/update tenant namespace with required labels
   - Validate OIDC issuer reachability (HTTP GET to `/.well-known/openid-configuration`)
   - Project OIDC client secret env var name into the tenant namespace (reads `spec.oidc.clientSecretRef`, creates a projected Secret in the tenant namespace)
   - Rebuild `kora-tenants` ConfigMap in `kora-system`
   - Trigger identity service reload (update a ConfigMap annotation to force pod config reload)
   - Update `KoraTenant` status conditions
3. Implement suspension: `spec.active: false` sets `state: suspended` in the tenant registry without deleting cluster resources
4. Write unit tests for registry assembly, OIDC validation, and Secret projection

### Phase 4: Error handling, upgrade strategy, and hardening (~1 week)

1. Implement `Automatic` upgrade mode — poll chart registry at configurable interval, upgrade on new patch version
2. Implement reconciler error recovery — exponential backoff on transient failures (OIDC unreachable, Helm timeout)
3. Add finalizers to both CRDs — prevent deletion of `KoraTenant` while namespace has active workloads
4. Implement `KoraTenant` deletion — remove tenant from registry, optionally delete namespace (controlled by `spec.deleteNamespaceOnRemoval`)
5. Add `make cluster-operator` Makefile target and update `cluster-deploy` to apply `OrbitPlatform` CR
6. Create `deploy/local/orbit-platform.yaml` (local `OrbitPlatform` CR)
7. Create `deploy/local/tenants/local.yaml` (local `KoraTenant` CR for the dev tenant)
8. Write e2e test: fresh cluster → `make cluster-operator` → `kubectl apply orbit-platform.yaml` → operator installs full stack → identity service issues tokens

### Effort Breakdown

| Component | Effort |
|---|---|
| CRD types + scaffold + bootstrap chart | 1 week |
| `OrbitPlatform` reconciler (happy path + upgrades) | 1 week |
| `KoraTenant` reconciler (provisioning + registry assembly) | 1 week |
| Error handling + upgrade strategy + e2e test | 1 week |
| **Total** | **~4 weeks** |

### Migration Path

The operator supersedes the Helmfile workflow. The transition for an existing local cluster is:

1. `make dev-cluster-down` + `make dev-cluster` — fresh cluster; any previously manually managed state is discarded
2. `make cluster-secrets` — new target wrapping `create-secrets.sh`; creates `orbit-platform-secrets` Secret
3. `make cluster-operator` — installs the bootstrap chart; operator pod starts
4. `kubectl apply -f deploy/local/orbit-platform.yaml` — operator installs the full stack (SPIRE → Istio → orbit-platform → orbit) and surfaces progress in `OrbitPlatform.status`
5. `kubectl apply -f deploy/local/tenants/local.yaml` — operator provisions the dev tenant namespace and registry

The existing `make cluster-platform` and `make cluster-deploy` targets are removed once the operator is validated.

The imperative `kubectl create configmap orbit-config`, `kubectl create configmap kora-tenants`, and `kubectl create secret` commands in the current Makefile are replaced entirely by the operator's reconciler.

---

## Validation

### Success Metrics

- A fresh cluster deployment requires exactly three commands: `make cluster-secrets`, `make cluster-operator`, `kubectl apply -f deploy/local/`
- Adding a new tenant requires exactly one command: `kubectl apply -f tenant.yaml`
- `kubectl get orbitplatform orbit -o jsonpath='{.status.phase}'` returns `Ready` after a successful install
- `kubectl get koratenant -o wide` shows OIDC validation status and phase for all tenants
- The identity service issues tokens within 60 seconds of an `KoraTenant` CR being applied (end-to-end latency of the registry reconciliation)

### Monitoring

- Operator exposes Prometheus metrics via `controller-runtime` standard metrics (reconcile queue depth, reconcile errors, reconcile duration)
- `OrbitPlatform.status.conditions` and `KoraTenant.status.conditions` are the source of truth for cluster health
- Alert on `OrbitPlatform.status.phase != Ready` for more than 5 minutes

---

## Related Decisions

- [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) — Tenant registry YAML format; the operator generates this file's content from `KoraTenant` CRs
- [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) — Two-chart model; the operator drives these charts via the Helm Go SDK
- [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) — Multi-binary structure; `cmd/operator/` follows the same conventions
- [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) — SPIRE workload identity; the operator validates SPIRE readiness before accepting `KoraTenant` reconciliation
- [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — Istio service mesh; the operator validates Istio readiness before platform install
- [repository-pattern-and-code-organization](2026-02-17-repository-pattern-and-code-organization.md) — Feature package conventions; `internal/operator/` follows these

---

## References

- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) — Kubernetes operator framework used by most production operators
- [Helm Go SDK](https://pkg.go.dev/helm.sh/v3) — `helm.sh/v3/pkg/action` for programmatic chart install/upgrade
- [Kubebuilder Book](https://book.kubebuilder.io/) — Reference for CRD design, reconciler patterns, and status conditions
- [External Secrets Operator](https://external-secrets.io/) — The recommended secret management integration for production deployments
- [Operator pattern — Kubernetes docs](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

---

## Notes

**On the operator owning the full release sequence:**

The operator's `OrbitPlatform` reconciler drives ALL chart releases in order: `spire-crds`, `spire`, `istio-base`, `istiod`, `gateway`, `orbit-platform`, `orbit`. This is a deliberate break from the earlier [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) model where Helmfile owned the third-party charts and the operator owned only the kora charts.

The reason for giving the operator ownership of the full sequence is environment parity: if the operator does not install SPIRE and Istio, local dev must use a different toolchain (Helmfile) than production (operator), which creates the divergence problem [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) was designed to eliminate.

The operator acknowledges that SPIRE and Istio have their own upgrade considerations (CA rollover windows, control plane/data plane version skew). It handles this by waiting for readiness at each step rather than fire-and-forget helm installs, and by surfacing per-release health in `status.conditions`. SPIRE and Istio version pins live in the operator's own `version.go` constants alongside the orbit-platform and orbit chart versions — a single version bump in `spec.version` advances all of them together, which is what "kora version X requires SPIRE Y and Istio Z" actually means.

Operators who manage their own SPIRE and Istio upgrades independently set `spec.spire.enabled: false` and `spec.istio.enabled: false` — the operator validates they are present but does not touch them.

**On the `kora-system` namespace ownership problem:**

[helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md)'s `deploy/local/orbit-system-namespace.yaml` pre-creates the namespace without Helm ownership labels, causing `helm upgrade orbit-platform` to fail with an ownership conflict. The operator sidesteps this entirely: it creates the namespace directly via `client-go` (`CreateOrUpdate`), not via Helm's `--create-namespace` flag. The namespace is owned by the operator's reconciler, not by Helm. This eliminates the lookup guard workaround in `charts/orbit-platform/templates/namespaces.yaml`.

**On the `KORA_OIDC_CLIENT_ID` global variable:**

[tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) noted that `OIDCConfig` global vars (`KORA_OIDC_CLIENT_ID`, `KORA_OIDC_ISSUER_URL`) must be removed in the same PR that adds multi-tenant support. With the operator, these env vars are no longer placed in the `orbit-config` ConfigMap by the operator — they are derived from `KoraTenant` CRs and written into the `kora-tenants` ConfigMap. The `orbit-config` ConfigMap created by the operator contains only infrastructure config (DB host, Redis host) that is not tenant-specific. This aligns with [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md)'s requirement to remove the global OIDC vars.

**On ESO and secret management:**

For local development, `scripts/cluster/create-secrets.sh` creates the `Secret` that `OrbitPlatform.spec.secrets.platformSecretRef` points at. For production EKS, operators use ESO to sync secrets from AWS Secrets Manager into the same `Secret` name/namespace the `OrbitPlatform` CR references. The operator code has no awareness of how the `Secret` was created — it reads the `Secret` via `client-go` and uses the values. This is the correct abstraction: the operator is a consumer of secrets, not a manager of them.

**Last Updated:** 2026-06-15

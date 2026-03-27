# 0024. Helm Chart Distribution and GitOps Repository Structure

**Status:** Accepted
**Date:** 2026-06-04
**Deciders:** Engineering Team
**Technical Story:** ADR-0020 established that application manifests live in `k8s/` as raw Kustomize resources. As Orbit moves toward a model where operators deploy the platform into their own clusters via GitOps tooling (Flux, ArgoCD, Helmfile), the raw-Kustomize approach is the wrong distribution primitive. Operators need a versioned artifact with a documented values API they can override — not a copy of this repo's manifests. This ADR defines the target repository structure, two Helm charts as the distribution artifacts, and the "eat your own dog food" local deployment model.

---

## Context

### Background

The current repository structure conflates three distinct concerns across two folders:

**`infrastructure/`** contains two conceptually different things:
- `infrastructure/overlays/local/` — installs third-party charts (Istio, SPIRE) into a local k3d cluster via Kustomize `helmCharts:`. These are not developer bootstrap utilities — they are the **Core Infrastructure Layer** that underpins mTLS, SVID issuance, and workload attestation. Without them, Orbit cannot function. Calling this "bootstrap tooling" mischaracterises mandatory runtime dependencies as optional setup.
- `infrastructure/base/` — Orbit-owned cluster-wide resources: namespaces (with Istio injection and SPIFFE labels), `ClusterSPIFFEID` templates, and network policies. These are first-party Orbit resources that should be part of the operator-facing distribution artifact.

**`k8s/`** contains Orbit's own service workloads: the identity service `Deployment`, `ServiceAccount`, `Service`, Istio `DestinationRule`, Envoy `EnvoyFilter`, and example tenant manifests. These are also first-party Orbit resources that should be operator-facing.

Neither `infrastructure/base/` nor `k8s/` is structured in a way that allows operators to consume them cleanly. Operators cannot easily:
- Override image tags, replica counts, or resource limits without forking the manifests
- Mount their own tenant configuration at a path the service expects
- Pin a versioned release and upgrade on their own schedule
- Apply environment-specific overrides (staging vs prod) without duplicating the entire manifest tree

The `values.yaml` API surface of a Helm chart solves all of these exactly. It is the standard mechanism Kubernetes operators use to consume and configure platform software.

### Istio and SPIRE Are Core Infrastructure — Not Optional Tooling

Istio and SPIRE are not add-ons or developer conveniences. They are hard runtime dependencies of the Orbit platform:

- **SPIRE** issues SVIDs to every workload. The identity-proxy sidecar waits for SPIFFE Workload API readiness before accepting connections. The identity service's SPIFFE SVID exchange endpoint is meaningless without SPIRE attestation. `ClusterSPIFFEID` templates define the SPIFFE URI structure that the identity service's principal extractor parses (`/tenant/{id}/agent/{name}`).
- **Istio** injects the Envoy sidecar that runs the identity-proxy ext_proc process. `EnvoyFilter` resources configure the sidecar interception. `DestinationRule` resources enforce mTLS. Without Istio, the sidecar injection that the entire agent traffic interception model depends on does not occur.

An operator who installs `charts/orbit` without Istio and SPIRE gets a set of pods that cannot participate in the platform's identity or policy enforcement model. These dependencies must be distributed and managed as part of the Orbit platform, not assumed to exist in the target cluster.

Operators who already have Istio or SPIRE installed in their cluster can disable those sub-charts via a `values.yaml` flag — but Orbit's Istio-specific configuration (sidecar injection, `EnvoyFilter`, `DestinationRule`, `IstioMeshConfig`) and SPIRE-specific configuration (`ClusterSPIFFEID` templates, agent socket path) are always installed as part of the Orbit chart.

### The GitOps Operator Model

Orbit's intended operator experience is:

1. An operator has their own **deployment repository** (separate from this repo). It contains their Helm values overrides, their tenant configuration, and their GitOps sync configuration (Flux `HelmRelease`, ArgoCD `Application`, or `helmfile.yaml`).
2. They install the **`orbit-platform` chart** first (unless they already have Istio/SPIRE, in which case they disable those sub-charts). This installs the Core Infrastructure Layer.
3. They install the **`orbit` chart** against the running platform. This installs Orbit services plus Orbit's platform-level configuration (ClusterSPIFFEIDs, netpols, EnvoyFilters).
4. Their tenant-specific configuration (tenant registry, OIDC client secrets) lives in their deployment repo, not in the Orbit repo.
5. Orbit platform updates are consumed by bumping the chart version in their deployment repository.

The Orbit repository's job is to **produce the charts** and maintain the values API. The operator's deployment repository's job is to **consume the charts** with environment-specific overrides.

### The "Dog Food" Consequence

This model has a direct implication for how this repository itself deploys Orbit into the local k3d cluster: it should do so the same way an operator would — by consuming `charts/orbit-platform` and `charts/orbit` with local overrides in `deploy/local/`. There is no reason the local cluster deployment should use a different primitive (raw Kustomize `helmCharts:`) from what operators use (direct Helm). Doing so creates a class of bugs that only appear in operator deployments and makes the `k8s/` manifests and the Helm chart templates diverge over time.

### Problem Statement

We need to define:

1. The two-chart structure: what belongs in `charts/orbit-platform/` vs `charts/orbit/`
2. What happens to `k8s/` and `infrastructure/` — the existing manifest directories
3. How Istio and SPIRE are distributed as sub-charts with opt-out flags for operators with pre-existing installations
4. How the local k3d cluster deployment changes to use both charts ("dog food" model)
5. Where `deploy/local/` fits and how it relates to an operator's external deployment repository
6. How `config/tenants.yaml` (per ADR-0023) is wired in both Tier 1 (`make dev`) and Tier 2 (`make cluster-deploy`) development environments

### Goals

- Orbit is distributed as two versioned Helm charts with documented `values.yaml` APIs
- `charts/orbit-platform/` encapsulates Istio and SPIRE as conditional sub-charts — operators with pre-existing installations disable them via a flag
- Operators can consume both charts from their own deployment repository without touching the Orbit source repository
- The local k3d cluster deployment uses the same charts that operators use — no separate manifest tree to maintain
- `deploy/local/` is an explicit "we are an operator" folder that demonstrates correct chart consumption
- `config/tenants.yaml` is committed to the Orbit repo for local development, following the same pattern as `config/dex/config.yaml`

### Non-Goals

- Publishing the Helm charts to a public chart registry (OCI or Helm repo) — that is a release pipeline concern, deferred
- Defining the operator's deployment repository structure — operators choose their own layout; `deploy/local/` is a reference, not a template
- Changing the Go binary structure or `cmd/` layout — ADR-0020 governs that; this ADR governs only where the Kubernetes manifests live
- Supporting clusters with no service mesh at all — Orbit's enforcement model (ADR-0011, ADR-0015) is mesh-native; a meshless deployment is not a supported configuration

---

## Decision Drivers

* **Istio + SPIRE are mandatory runtime dependencies**: They are not optional tooling; distributing them as sub-charts makes this dependency contract explicit and operator-manageable
* **Operator experience**: Operators need versioned, configurable artifacts — not a fork of the Orbit manifest tree
* **Single source of truth**: A separate Kustomize tree (`k8s/`) and Helm chart templates would diverge; the charts must be the only source of truth for Orbit's Kubernetes resources
* **Dog food principle**: Running a different deployment mechanism locally than operators use creates hidden divergence and false confidence
* **Opt-out for pre-existing infrastructure**: Operators with Istio or SPIRE already installed must be able to disable those sub-charts without losing Orbit's platform-level configuration for those systems
* **GitOps compatibility**: The chart structure must be compatible with Flux `HelmRelease` and ArgoCD `Application` — standard Helm chart layout required

---

## Considered Options

### Option A: Raw Kustomize in `k8s/` as the distribution artifact

**Description:** Keep `k8s/` as-is. Document it as what operators should apply. Operators fork or vendor the `k8s/` directory. The local cluster continues to use it directly.

**Pros:**
- No structural change required
- Kustomize bases and overlays are well understood

**Cons:**
- Operators cannot consume updates without re-forking or re-vendoring — no versioning primitive
- No documented override API — operators must understand every manifest to know what to change
- Istio and SPIRE are not distributed with the artifact — operators must find, configure, and install compatible versions independently
- Environment-specific customisation (image tags, replicas, resource limits, secret refs) requires overlay trees that are messy to maintain
- `k8s/` and any future Helm chart would need to be kept in sync — guaranteed to diverge
- Local cluster deployment uses a different primitive from what operators use — breaks the dog food principle

### Option B: Single `charts/orbit/` chart; Istio and SPIRE remain as external prerequisites

**Description:** Create `charts/orbit/` as the operator-facing distribution artifact for Orbit services. Document Istio and SPIRE as prerequisites that operators must install themselves at compatible versions with compatible configuration. Keep `k8s/` raw Kustomize for the local cluster deployment.

**Pros:**
- Operators get a proper Helm chart for Orbit services
- No conflict with operators who already have Istio/SPIRE installed
- Simpler chart structure

**Cons:**
- Shifts a significant configuration burden onto operators — Istio must be configured with specific `IstioMeshConfig`, SPIRE must be configured with the correct trust domain and agent socket path; misconfigurations are silent and hard to diagnose
- No versioned compatibility guarantee — Orbit is tested against specific Istio/SPIRE versions; operators who install different versions get undefined behaviour
- Two manifest sources (`k8s/` + `charts/orbit/`) for the same services — they will inevitably diverge
- Bugs that exist only in the Helm chart rendering are not caught locally
- Violates the dog food principle — the local cluster is not exercising the operator artifact

### Option C: Two charts — `charts/orbit-platform/` + `charts/orbit/`; both used locally (chosen)

**Description:** Create two charts. `charts/orbit-platform/` wraps Istio and SPIRE as conditional sub-charts — operators with pre-existing installations disable them with a flag while still receiving Orbit's platform configuration for those systems. `charts/orbit/` contains Orbit services plus all Orbit-owned platform resources (ClusterSPIFFEIDs, netpols, EnvoyFilters, DestinationRules). Delete `k8s/`. Create `deploy/local/` with values files for both charts. The local cluster installs both charts — exactly as an operator would.

**Pros:**
- Core Infrastructure Layer (Istio, SPIRE) is a first-class part of the distribution — no external prerequisite hunting
- Operators with pre-existing Istio/SPIRE can disable those sub-charts; they still receive Orbit's validated platform configuration for those systems
- Single source of truth for all Orbit Kubernetes resources — no divergence possible
- Local cluster deployment exercises the exact same artifacts operators use
- `deploy/local/` serves as a concrete, always-valid reference for operator adoption
- Chart template bugs are caught during local development, not after operator deployment
- Versioned release requires no structural change — point chart sources at a published registry URL
- Operator deployment repository structure mirrors `deploy/local/` exactly

**Cons:**
- One-time migration cost: `k8s/` and `infrastructure/` contents migrate into `charts/`
- Two chart installs instead of one (mitigated by Helmfile or a Makefile wrapper)
- `spire-crds` must be installed as a separate prerequisite step before `orbit-platform` due to a Helm limitation on sub-chart CRD ordering (see Notes)

---

## Decision

**Chosen Option:** Option C — `charts/orbit-platform/` for the Core Infrastructure Layer plus `charts/orbit/` for Orbit services; both consumed locally via `deploy/local/`

### Rationale

Istio and SPIRE are not optional. Distributing them as conditional sub-charts makes the dependency contract explicit and version-pinned. Operators who already have them installed are not forced to accept a duplicate installation — they use a `values.yaml` flag to disable the sub-chart while retaining Orbit's platform configuration. This is the correct model for a platform that has hard infrastructure dependencies: include them, make them opt-out, pin the versions tested against.

The dog food principle is equally decisive. If the local cluster uses raw Kustomize and operators use Helm charts, the local cluster is not testing the operator artifact. Option C collapses this: the local cluster installs `charts/orbit-platform` and `charts/orbit` with `deploy/local/` value overrides, exactly as an operator would.

---

## Structure

### Target Repository Layout

```
charts/
  orbit-platform/                     # Core Infrastructure Layer chart
    Chart.yaml                        # dependencies: spire-crds, spire, istio-base, istiod
    values.yaml                       # operator-facing defaults with sub-chart conditions

  orbit/                              # Orbit services chart
    Chart.yaml                        # name: orbit, version: 0.1.0
    values.yaml                       # documented operator-facing defaults
    templates/
      platform/
        namespaces.yaml               # orbit-system + tenant namespaces with labels
        clusterSpiffeIDs.yaml         # all ClusterSPIFFEID templates (from infrastructure/base/spiffe.yaml)
        netpols.yaml                  # network policies (from infrastructure/base/net-pols.yaml)
      identity/
        serviceaccount.yaml
        deployment.yaml               # migrated from k8s/system/identity.yaml
        service.yaml
        destination-rule.yaml         # migrated from k8s/system/destination-rule.yaml
      identity-proxy/
        envoyfilter.yaml              # migrated from k8s/istio-system/envoyfilter-identity-proxy.yaml
      _helpers.tpl                    # shared template helpers

infrastructure/
  bootstrap/                          # Local k3d cluster creation only
    k3d-config.yaml                   # k3d cluster definition (node counts, port mappings, registry)
    README.md                         # "run this once to create the k3d cluster; then use deploy/local/"

deploy/
  local/                              # "We are an operator deploying to local k3d"
    platform-values.yaml              # orbit-platform local overrides (SPIRE trust domain, Istio mesh config)
    orbit-values.yaml                 # orbit local overrides (local registry, dev image tags, tenant config path)
    fixtures/                         # Non-Helm local-only resources — manual kubectl apply; NOT Helm-managed
      orbit-example-tenant.yaml       # example tenant namespace + RBAC (moved from k8s/orbit-example-tenant/)
    README.md                         # required content: install prerequisites, three-step sequence,
                                      #   how to customise values for non-local clusters,
                                      #   published chart registry URLs (once available),
                                      #   trust domain consistency requirement,
                                      #   fixtures/ disclaimer (not managed by Helm; local-only)

config/
  dex/
    config.yaml                       # unchanged — committed local Dex config
  tenants.yaml                        # committed local dev tenant registry (see ADR-0023)
                                      # NOT tenants.local.yaml — the *.local gitignore glob would match it
```

### What Dissolves

**`k8s/`** — deleted entirely. Contents migrate:
- `k8s/system/identity.yaml` → `charts/orbit/templates/identity/`
- `k8s/istio-system/envoyfilter-identity-proxy.yaml` → `charts/orbit/templates/identity-proxy/`
- `k8s/orbit-example-tenant/` → `deploy/local/fixtures/orbit-example-tenant.yaml` — a local-only fixture applied via `kubectl apply`, **not** part of either Helm chart. It is clearly labelled in `deploy/local/README.md` as "not managed by Helm — manual apply only" to prevent confusion when operators use `deploy/local/` as their adoption reference.

**`infrastructure/base/`** — deleted after migration:
- `namespace.yaml` → `charts/orbit/templates/platform/namespaces.yaml`
- `spiffe.yaml` → `charts/orbit/templates/platform/clusterSpiffeIDs.yaml`
- `net-pols.yaml` → `charts/orbit/templates/platform/netpols.yaml`

**`infrastructure/overlays/local/`** — renamed to `infrastructure/bootstrap/`. Its `helmCharts:` Kustomize directives (which installed Istio and SPIRE) are retired. Istio and SPIRE installation is now handled by `charts/orbit-platform/`. The bootstrap folder retains only the k3d cluster creation config.

### `charts/orbit-platform/` — Core Infrastructure Layer Chart

This chart owns the installation of Istio and SPIRE at versions validated against Orbit. It does not install any Orbit-owned resources — namespaces, ClusterSPIFFEIDs, and netpols belong in `charts/orbit/`.

**`charts/orbit-platform/Chart.yaml`:**

```yaml
apiVersion: v2
name: orbit-platform
description: Core Infrastructure Layer for Orbit — Istio service mesh and SPIRE workload identity
type: application
version: 0.1.0
dependencies:
  # SPIRE CRDs are a separate chart because the spiffe/spire chart creates ClusterSPIFFEID
  # resources directly in templates (not as CRD hooks) — CRDs must be present before install.
  # See ADR-0024 Notes for the spire-crds prerequisite constraint.
  - name: spire-crds
    repository: https://spiffe.github.io/helm-charts-hardened/
    version: "0.3.0"
    condition: spire.enabled
  - name: spire
    repository: https://spiffe.github.io/helm-charts-hardened/
    version: "0.20.0"
    condition: spire.enabled
  - name: base
    alias: istio-base
    repository: https://istio-release.storage.googleapis.com/charts
    version: "1.21.0"
    condition: istio.enabled
  - name: istiod
    repository: https://istio-release.storage.googleapis.com/charts
    version: "1.21.0"
    condition: istio.enabled
```

**`charts/orbit-platform/values.yaml`:**

```yaml
# Orbit Platform — Core Infrastructure Layer default values.
# These install Istio and SPIRE at versions validated against Orbit.
# Operators with pre-existing Istio or SPIRE installations set enabled: false
# for the relevant sub-chart. Orbit's platform configuration for those systems
# is always installed via charts/orbit regardless of these flags.

global:
  trustDomain: "orbit.local"   # SPIFFE trust domain — must match across orbit-platform and orbit

istio:
  # Set to false if Istio is already installed in the cluster.
  # Orbit's EnvoyFilter and DestinationRule resources are installed by charts/orbit
  # regardless of this flag — they configure the sidecar Istio already provides.
  enabled: true

  # Values passed to the istiod sub-chart
  istiod:
    meshConfig:
      accessLogFile: /dev/stdout
      enableEnvoyAccessLogService: false
      defaultConfig:
        holdApplicationUntilProxyStarts: true
    pilot:
      traceSampling: 1.0         # 100% sampling in local dev; override to 0.1 in production

spire:
  # Set to false if SPIRE is already installed in the cluster.
  # Orbit's ClusterSPIFFEID templates are installed by charts/orbit regardless of this flag.
  enabled: true

  # Values passed to the spire sub-chart (spiffe/spire v0.20.0 → SPIRE 1.9.4)
  spire-server:
    caTTL: 24h
    defaultX509SvidTTL: 4h
    defaultJwtSvidTTL: 1h
    controllerManager:
      enabled: true

  spire-agent:
    workloadAttestors:
      k8s:
        skipKubeletVerification: true
```

**`deploy/local/platform-values.yaml`** — local k3d overrides for orbit-platform:

```yaml
# deploy/local/platform-values.yaml
# Local k3d cluster overrides for orbit-platform.
# Global trust domain must match deploy/local/orbit-values.yaml.

global:
  trustDomain: "orbit.local"

istio:
  enabled: true
  istiod:
    pilot:
      traceSampling: 1.0   # 100% trace sampling for local dev

spire:
  enabled: true
```

### `charts/orbit/` — Orbit Services Chart

This chart owns all Orbit-authored Kubernetes resources: platform-level resources (namespaces with correct labels, ClusterSPIFFEIDs, netpols) and service workloads (identity, identity-proxy). It does not install Istio or SPIRE — it configures them (via EnvoyFilter, DestinationRule, ClusterSPIFFEID).

**`charts/orbit/values.yaml`:**

```yaml
# Orbit platform Helm chart default values.
# Operators override these in their deployment repository's values file.

global:
  trustDomain: "orbit.local"   # Must match the trust domain in orbit-platform values
  # IfNotPresent is correct for a distributed chart — avoids a registry round-trip on every
  # pod start. The local dev override (deploy/local/orbit-values.yaml) sets Always so that
  # local builds with floating `latest` tags are picked up without a pod delete.
  imagePullPolicy: IfNotPresent

identity:
  image:
    repository: ghcr.io/orbitlabs/orbit-identity
    tag: latest
  replicas: 1
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
  config:
    # Path inside the container where the tenant registry file is mounted.
    tenantsConfigPath: /etc/orbit/tenants.yaml
  # Name of an existing ConfigMap containing a `tenants.yaml` key with the tenant registry.
  # Operators create this ConfigMap in their GitOps tooling (e.g. as a ConfigMap resource in
  # their deployment repo, or via ExternalSecrets for the credential env vars).
  # The chart mounts it at identity.config.tenantsConfigPath.
  # When empty, no mount is added — the operator must ensure the file exists by other means.
  existingTenantsConfigMap: ""
  existingSecret: ""            # name of an existing k8s Secret containing orbit-secrets keys

platform:
  namespaces:
    # Additional tenant namespaces to create, labelled for Istio injection + SPIFFE.
    # Each entry creates a Namespace with orbit.io/role: tenant and istio-injection: enabled.
    tenants: []
    # - orbit-example-tenant

  spiffe:
    # ClusterSPIFFEID resources configure the SPIRE Controller Manager.
    # These are always installed — they are Orbit's definition of valid SPIFFE identities.
    # They do not conflict with a pre-existing SPIRE installation as long as the
    # trust domain matches the value in orbit-platform.
    enabled: true

networkPolicy:
  enabled: true
```

**`deploy/local/orbit-values.yaml`:**

```yaml
# deploy/local/orbit-values.yaml
# Local k3d cluster overrides for charts/orbit.
# This is what "we are an operator" looks like.
# Operators mirror this pattern in their own deployment repository.

global:
  trustDomain: "orbit.local"
  imagePullPolicy: Always

identity:
  image:
    repository: orbit-registry:5001/orbit-identity
    tag: latest
  config:
    tenantsConfigPath: /etc/orbit/tenants.yaml
  existingSecret: orbit-secrets

platform:
  namespaces:
    tenants:
      - orbit-example-tenant
```

### Local Cluster Install Sequence — Tier 2

```bash
# Step 1: SPIRE CRDs prerequisite (idempotent)
# Must be installed before orbit-platform because spiffe/spire creates ClusterSPIFFEID
# resources in templates — the CRD must exist before the chart renders.
helm upgrade --install spire-crds spiffe/spire-crds \
  --version 0.3.0 \
  --namespace spire-system --create-namespace

# Step 2: Core Infrastructure Layer
helm upgrade --install orbit-platform ./charts/orbit-platform \
  -f deploy/local/platform-values.yaml \
  --namespace orbit-system --create-namespace

# Step 3: Orbit services and platform configuration
# Local convenience: create the tenants ConfigMap imperatively (idempotent via --dry-run=client).
# Operators using GitOps tooling (Flux, ArgoCD, Helmfile) should instead commit a ConfigMap
# resource to their deployment repository and set identity.existingTenantsConfigMap to its name.
# See "Operator Deployment Repository Pattern" for the GitOps-compatible path.
kubectl create configmap orbit-tenants \
  --from-file=tenants.yaml=./config/tenants.yaml \
  -n orbit-system --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install orbit ./charts/orbit \
  -f deploy/local/orbit-values.yaml \
  --set identity.existingTenantsConfigMap=orbit-tenants \
  --namespace orbit-system
```

This sequence is wrapped in a single `make cluster-deploy` target.

### Local Development Wiring — Both Tiers

**Tier 1 — `make dev` (non-cluster, air hot-reload on host):**

`ORBIT_TENANTS_CONFIG=./config/tenants.yaml` is set in `.env` (committed) and `.env.example`. The identity service reads this path at startup via `config.LoadFile[TenantRegistry]`. No cluster, no ConfigMap — the file is read directly from the filesystem.

**Tier 2 — `make cluster-deploy` (local k3d cluster):**

The install sequence above. The ConfigMap mount at `/etc/orbit/tenants.yaml` inside the pod matches `tenantsConfigPath`. This is the same mount pattern an operator uses with their own ConfigMap or Secret.

### `config/tenants.yaml` — Committed Local Dev Tenant Registry

This file follows the same commit model as `config/dex/config.yaml`:

- Committed to the repository
- Contains only local dev fixture data (the local Dex OIDC provider, test tenant IDs)
- Does NOT contain real secrets — OIDC client secrets for public clients are empty; non-public client secrets come from `.env`
- Is NOT named `tenants.local.yaml` — the `.gitignore` `*.local` glob would silently prevent it from being committed

```yaml
version: "v1"
tenants:
  - id: "local"
    displayName: "Local Development Tenant"
    state: "active"             # active | suspended; defaults to active if omitted
    oidc:
      issuerURL: "http://dex.orbit.local:5556/dex"
      clientID: "orbit"
      # Public client — no secret required. ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET is set to ""
      # in .env.example. Non-public clients set this to the env var holding the secret.
      clientSecretEnv: "ORBIT_TENANT_LOCAL_OIDC_CLIENT_SECRET"
      scopes:                   # defaults to [openid, profile, email] if omitted
        - openid
        - profile
        - email
      # Optional: restrict membership by an OIDC claim value within a shared issuer.
      # Flat struct matching ADR-0023: one claim/value pair per tenant.
      # claimBinding:
      #   claim: "email"
      #   value: "dev@orbit.local"
```

### Operator Deployment Repository Pattern

An operator's external deployment repository mirrors `deploy/local/` in structure:

```
deployment-repo/
  environments/
    prod/
      platform-values.yaml    # orbit-platform overrides: istio.enabled: false (pre-existing Istio)
      orbit-values.yaml       # orbit overrides: image tags, replicas, resource limits
      tenants-configmap.yaml  # their tenant registry as a ConfigMap or ExternalSecret
    staging/
      platform-values.yaml
      orbit-values.yaml
      tenants-configmap.yaml
  helmfile.yaml               # OR FluxCD HelmRelease / ArgoCD Application
```

Example: operator with pre-existing Istio:

```yaml
# environments/prod/platform-values.yaml
global:
  trustDomain: "orbit.prod.example.com"

istio:
  enabled: false    # Istio already installed — do not re-install

spire:
  enabled: true     # SPIRE managed by Orbit
```

Their `helmfile.yaml` references the published charts by version:

```yaml
releases:
  - name: spire-crds
    chart: spiffe/spire-crds
    version: "0.3.0"
    namespace: spire-system

  - name: orbit-platform
    chart: oci://ghcr.io/orbitlabs/charts/orbit-platform
    version: "0.1.0"
    namespace: orbit-system
    values:
      - environments/prod/platform-values.yaml
    needs:
      - spire-system/spire-crds

  - name: orbit
    chart: oci://ghcr.io/orbitlabs/charts/orbit
    version: "0.1.0"
    namespace: orbit-system
    values:
      - environments/prod/orbit-values.yaml
    needs:
      - orbit-system/orbit-platform
```

The operator never touches the Orbit source repository. Version upgrades are a one-line bump in their deployment repository.

### `infrastructure/bootstrap/` — k3d Cluster Creation Only

`infrastructure/bootstrap/` (renamed from `infrastructure/overlays/local/`) is scoped to a single responsibility: **create and configure the local k3d cluster**. It contains the k3d cluster definition (node counts, port mappings, local registry configuration). Its job ends when `k3d cluster create` completes.

It does NOT install Istio or SPIRE. That is `charts/orbit-platform/`'s responsibility. Engineers running `make cluster-up` get a bare k3d cluster ready to receive Helm chart installations — they do not get a configured platform. The platform installation step is `make cluster-deploy`.

The `helmCharts:` Kustomize directives that previously installed Istio and SPIRE from `infrastructure/overlays/local/kustomization.yaml` are retired entirely. Those charts are now declared as sub-chart dependencies in `charts/orbit-platform/Chart.yaml`.

---

## Consequences

### Positive

- **Core Infrastructure Layer is a first-class distribution concern**: Operators do not need to independently source, configure, and version-match Istio and SPIRE. The charts ship at validated versions with Orbit's required configuration.
- **Opt-out without configuration loss**: Operators with pre-existing Istio or SPIRE disable those sub-charts via a flag but still receive Orbit's `EnvoyFilter`, `DestinationRule`, and `ClusterSPIFFEID` configuration — the resources that actually make the platform work with those systems.
- **Single source of truth**: All Orbit Kubernetes resources live in `charts/orbit/templates/`. There is no `k8s/` vs `charts/` split to maintain.
- **Dog food validity**: The local cluster exercises the exact same operator artifacts. Chart template bugs surface during development, not after operator deployment.
- **`deploy/local/` is the reference operator guide**: It is always a working, tested example of how to consume both charts.
- **Versioned upgrades**: Once published, operators pin chart versions and upgrade on their own schedule.
- **`config/tenants.yaml` pattern is consistent**: Follows the established `config/dex/config.yaml` model — committed, fixture data only, no real secrets.

### Negative

- **Two chart installs instead of one**: Mitigated by Helmfile's `needs:` ordering or a Makefile wrapper — operators do not need to manually sequence the installs.
- **`spire-crds` is a mandatory separate step**: A Helm limitation means CRDs cannot be reliably installed via sub-chart before other sub-charts render against them. This adds one prerequisite command to the install sequence (see Notes).
- **One-time migration cost**: `k8s/` and `infrastructure/` contents migrate into `charts/`. This is a moderate-sized but mechanical task.
- **Helm template syntax overhead**: Simple namespace and RBAC manifests gain `{{ }}` templating even where they have no variable content yet — minor but adds visual noise.

### Neutral

- `k8s/orbit-example-tenant/` moves to `deploy/local/` — it was always a local fixture, never operator-facing. The move makes that explicit.
- `make cluster-deploy` replaces `kubectl apply -k k8s/` and the old bootstrap Kustomize sequence with two `helm upgrade --install` calls plus a ConfigMap create.
- `infrastructure/base/` is a transient state during migration — it exists until the chart is complete, then is removed.
- `infrastructure/overlays/local/` becomes `infrastructure/bootstrap/` with all Helm chart installation removed — it retains only the k3d cluster definition.

---

## Implementation

### Migration Path

1. **Create `charts/orbit-platform/` skeleton** — `Chart.yaml` with SPIRE and Istio sub-chart dependencies, `values.yaml` with `spire.enabled` and `istio.enabled` conditions and all required sub-chart values
2. **Run `helm dependency update charts/orbit-platform/`** — vendor sub-charts into `charts/orbit-platform/charts/`
3. **Create `charts/orbit/` skeleton** — `Chart.yaml`, `values.yaml`, `templates/` directory structure, `_helpers.tpl`
4. **Migrate `infrastructure/base/`** — copy `namespace.yaml`, `spiffe.yaml`, `net-pols.yaml` into `charts/orbit/templates/platform/`; add Helm templating for values-driven fields (trust domain, tenant namespace list)
5. **Migrate `k8s/system/`** — copy `identity.yaml` (split into `serviceaccount.yaml`, `deployment.yaml`, `service.yaml`), `destination-rule.yaml` into `charts/orbit/templates/identity/`; parameterise image repo/tag, resource limits, secret name, `tenantsConfigPath` via values
6. **Migrate `k8s/istio-system/`** — copy `envoyfilter-identity-proxy.yaml` into `charts/orbit/templates/identity-proxy/`
7. **Create `deploy/local/`** — `platform-values.yaml` and `orbit-values.yaml` with local overrides; move `k8s/orbit-example-tenant/` here as a `kubectl apply` fixture (not part of either chart)
8. **Rename `infrastructure/overlays/local/`** to **`infrastructure/bootstrap/`**; remove all `helmCharts:` directives from `kustomization.yaml`; retain only the k3d cluster definition
9. **Update Makefile** — `cluster-up` runs k3d create using `infrastructure/bootstrap/k3d-config.yaml`; `cluster-deploy` runs the three-step install sequence (spire-crds → orbit-platform → orbit)
10. **Delete `k8s/`** — once all content is accounted for in `charts/orbit/templates/` or `deploy/local/`
11. **Delete `infrastructure/base/`** — once all content is in `charts/orbit/templates/platform/`
12. **Add `ORBIT_TENANTS_CONFIG=./config/tenants.yaml`** to `.env.example` and create `config/tenants.yaml` (per ADR-0023 Phase 3)

### New Service Checklist (post-migration)

When a new platform service is introduced:

1. Add `charts/orbit/templates/{service}/` — `serviceaccount.yaml`, `deployment.yaml`, `service.yaml`
2. Add values keys for the service under the service name key in `charts/orbit/values.yaml` (image, replicas, resources)
3. Update `deploy/local/orbit-values.yaml` with local overrides for the new service
4. No change to `charts/orbit-platform/` — SPIFFE identity is label-driven (ADR-0020); the generic `ClusterSPIFFEID` templates in `charts/orbit/templates/platform/` issue the SPIFFE ID from the pod label automatically

---

## Validation

### Success Metrics

- The three-step install sequence (`spire-crds` → `orbit-platform` → `orbit`) with `deploy/local/` values produces a working Orbit identity service in the local k3d cluster with no additional manual `kubectl apply` commands
- `helm template orbit-platform ./charts/orbit-platform -f deploy/local/platform-values.yaml` and `helm template orbit ./charts/orbit -f deploy/local/orbit-values.yaml` both render valid YAML with no missing keys or template errors
- `k8s/` directory no longer exists in the repository
- `infrastructure/base/` directory no longer exists in the repository
- An operator following `deploy/local/README.md` can adopt both charts in a minimal external cluster without referencing the Orbit source repository
- An operator with pre-existing Istio can set `istio.enabled: false` and deploy successfully — Orbit's `EnvoyFilter` and `DestinationRule` resources still render and apply

### Monitoring

- Chart rendering is validated in CI via `helm template` on every PR that touches `charts/` or `deploy/local/`
- Local cluster end-to-end test (`make cluster-deploy && make cluster-test`) exercises the full Helm install path
- Sub-chart versions in `charts/orbit-platform/Chart.yaml` are pinned; Dependabot or equivalent tracks upstream releases. Sub-chart version bump PRs (e.g. SPIRE `0.20.0` → `0.21.0`) must pass `make cluster-deploy && make cluster-test` in CI before merge — a SPIRE minor version bump can silently change SVID issuance or mTLS behaviour. Until `make cluster-test` exists as a defined target, this is a prerequisite for the Monitoring section to be meaningful: the first implementation task is defining at minimum a smoke test that verifies the identity service can issue a token via the SPIFFE SVID exchange endpoint after a fresh `make cluster-deploy`.

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — SPIRE as the workload identity infrastructure; SPIRE distribution via `charts/orbit-platform/` is the operator-facing realisation of this decision
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Istio as the sidecar control plane; Istio distribution via `charts/orbit-platform/` is the operator-facing realisation of this decision
- [ADR-0016](0016-local-development-environment.md) — Two-tier local dev strategy; Tier 2 cluster deployment is updated by this ADR to use the two-chart Helm install sequence
- [ADR-0018](0018-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — Label-driven SPIFFE ID model; `ClusterSPIFFEID` templates migrate into `charts/orbit/templates/platform/`
- [ADR-0020](0020-multi-service-backend-deployment-structure.md) — Go binary structure and the convention that "application manifests live in `k8s/`"; this ADR supersedes the `k8s/` manifest location convention in favour of `charts/orbit/templates/`
- [ADR-0023](0023-tenant-registration-and-oidc-provider-binding.md) — Tenant registry; defines `config/tenants.yaml` and `ORBIT_TENANTS_CONFIG` env var; this ADR defines how the file is mounted in both dev tiers

---

## References

- [Helm Chart Best Practices](https://helm.sh/docs/chart_best_practices/)
- [Helm Sub-charts and Global Values](https://helm.sh/docs/chart_template_guide/subcharts_and_globals/)
- [Flux HelmRelease API](https://fluxcd.io/flux/components/helm/helmreleases/)
- [ArgoCD Applications](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)
- [spiffe/spire Helm chart](https://github.com/spiffe/helm-charts-hardened) — `orbit-platform` sub-chart dependency
- [istio/istiod Helm chart](https://istio.io/latest/docs/setup/install/helm/) — `orbit-platform` sub-chart dependency
- [charts/orbit-platform/](../../charts/orbit-platform/) — Core Infrastructure Layer chart (created during implementation)
- [charts/orbit/](../../charts/orbit/) — Orbit services chart (created during implementation)
- [deploy/local/](../../deploy/local/) — Local operator reference deployment (created during implementation)

---

## Notes

**On the `spire-crds` prerequisite:**

The `spiffe/spire` Helm chart (v0.20.0) creates `ClusterSPIFFEID` resources directly in its templates — it does not include CRDs as Helm hooks. This means the `spire.spiffe.io/v1alpha1` CRD must already exist in the cluster before `helm install spire` renders its templates, or the install fails with `no matches for kind "ClusterSPIFFEID"`.

Helm does not guarantee sub-chart installation order for CRDs within a single `helm install`. Therefore, `spire-crds` is installed as a separate, prior `helm upgrade --install` command. This is idempotent and adds one step to the sequence. Helmfile's `needs:` ordering handles this automatically in the operator's deployment repository; locally it is wrapped in `make cluster-deploy`.

**On operators with pre-existing Istio:**

When `istio.enabled: false` in `platform-values.yaml`, `charts/orbit-platform/` does not install `istio-base` or `istiod`. However, `charts/orbit/templates/identity-proxy/envoyfilter.yaml` and `charts/orbit/templates/identity/destination-rule.yaml` are always rendered — they configure the sidecar that the operator's pre-existing Istio provides. The operator's Istio must have sidecar injection enabled for the `orbit-system` namespace, matching the `istio-injection: enabled` label that `charts/orbit/templates/platform/namespaces.yaml` applies.

**On trust domain consistency:**

`global.trustDomain` must be set to the same value in both `deploy/local/platform-values.yaml` and `deploy/local/orbit-values.yaml` (and in the operator's equivalent files). SPIRE uses this value as the SPIFFE trust domain; Orbit's `ClusterSPIFFEID` templates embed it in SPIFFE URI templates; the identity service's principal extractor validates SVIDs against it. A mismatch produces silent authentication failures — the identity service will reject all SVIDs because the trust domain in the SVID URI will not match the one it was configured to expect.

**Mitigation — Helmfile shared globals block:**

Operators using Helmfile should define `global.trustDomain` once at the top level and reference it in both releases via the `values:` block or Helmfile's `environments.values` mechanism. The `deploy/local/` reference deployment uses a shared `platform-values.yaml` and `orbit-values.yaml` that both hardcode `global.trustDomain: "orbit.local"` — operators must keep these in sync. For Flux/ArgoCD, the trust domain value should come from a shared `ConfigMap` or a Helm `--set` parameter applied to both releases from the same source of truth.

**Mitigation — `make cluster-deploy` validation:**

A post-install trust domain assertion can be added to validate the running SPIRE configuration: `kubectl exec -n spire-system spire-server-0 -c spire-server -- /opt/spire/bin/spire-server bundle show` reads the active trust domain and can be compared against the deployed identity service configuration. A mismatch should fail the deploy with a clear error before it manifests as a silent auth failure. This assertion is not currently implemented.

**On chart versioning:**

Until a chart registry publishing pipeline exists, both charts are consumed from the local filesystem in `deploy/local/` and the Makefile. The `version:` fields in `Chart.yaml` are maintained manually and bumped with each release. When a publishing pipeline is added, the only change in an operator's deployment repository is replacing `./charts/orbit-platform` and `./charts/orbit` with OCI registry URLs — no structural change to `charts/` or `deploy/local/` is required.

**Last Updated:** 2026-06-04

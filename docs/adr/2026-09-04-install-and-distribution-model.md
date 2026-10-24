# Install and Distribution Model: Published Charts and a Tenant-Only Operator

**Status:** Proposed (amended 2026-09-10: schema application moved into the `kora` chart, see [Database Migrations](2026-09-10-database-migrations.md). Amended 2026-10-24: Amendment 1 Accepted, the operator projects declared resources)
**Date:** 2026-09-04
**Deciders:** Justin Wilkin
**Technical Story:** Implements the [Install and Distribution PRD](../prd/install-and-distribution.md). Supersedes [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) and amends [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md). The companion decision on how kora's sidecars reach pods in a mesh kora did not install is [Enforcement Sidecar Injection, Preflight and Restricted Pod Security](2026-09-04-enforcement-sidecar-injection-and-preflight.md).

---

## Context

### Background

kora ships three Helm charts (`charts/kora-operator`, `charts/orbit-platform`, `charts/orbit`) and installs them through the operator. The [operator ADR](2026-03-31-orbit-kubernetes-operator.md) made the operator the single install entry point: an `OrbitPlatform` custom resource drives seven Helm releases (spire-crds, spire, istio-base, istiod, gateway, orbit-platform, orbit) with chart versions pinned as Go constants in `backend/internal/operator/util.go`. The operator image bakes `charts/` and `deploy/local/` in at build time (`Dockerfile.operator`), and `loadValuesFile` reads the local development values as the production fallback.

The operator's second job grew after that ADR: tenant namespaces, per-tenant CA minting, per-tenant OPA configuration, the sandbox egress NetworkPolicy, six aggregators (tenants, OIDC client secrets, tenant CAs, connections, OAuth clients, MCP servers) and a pod mutating webhook that stamps tenant identity and injection templates onto every tenant pod. None of that appears in the ADR, and all of it is continuous reconciliation of state that changes after install.

Several documented behaviours do not exist in code. `spec.version` is echoed into status and drives nothing. `upgradeStrategy` is read by nothing. `spire.enabled: false` and `istio.enabled: false` skip the install with no validation of what is present. `deleteNamespaceOnRemoval` has no finalizer. `docs/guides/deployment.md` points at `oci://ghcr.io/runkora/charts/kora-operator`, which has never been published.

### Problem Statement

kora has no installable release. The install model it documents fails the three requirements enterprise platform teams put first: do not install my mesh or identity system, reconcile everything from Git with no imperative steps, and separate cluster-scoped from namespaced concerns. Istio removed its own in-cluster operator in 1.24 for the same reasons (see References).

### Goals

- Two published, signed charts that install with Helm or Argo CD with no imperative steps.
- Istio and SPIRE as documented, verifiable prerequisites. kora never installs them.
- The operator does tenant lifecycle and enforcement attachment only, with RBAC to match.
- One reference install for blank clusters that local development also uses.
- A release pipeline that cuts charts and images together with pinned digests, signatures and an image inventory.
- A compatibility statement per release.

### Non-Goals

- Database migration mechanics (separate work).
- Istio ambient mode, `EnvoyFilter` removal, Gateway API `HTTPRoute` ([#265](https://github.com/clear-route/orbit/issues/265)), corporate egress proxies.
- A classic Helm repository index, a CLI installer, OpenShift packaging, multi-cluster.
- Managing PostgreSQL or Redis.

---

## Decision Drivers

- **Enterprise adoption blockers**, in the order they block: BYO mesh and identity, GitOps transparency, cluster-scoped versus namespaced split, restricted pod security, registry and pull-secret overrides, signed and pinned artefacts, existing-secret conventions.
- **Evaluator time to first agent run** on a blank cluster.
- **One tested path.** Whatever installs kora locally must be what installs it for customers.
- **Least privilege for the operator.** Its permissions follow from what it reconciles.
- **Established practice** for a platform with hard infrastructure dependencies: charts as the product, prerequisites documented, an operator only for continuous day-2 work.

---

## Considered Options

### Option 1: Operator-driven install (status quo)

**Description:** Keep `OrbitPlatform` driving SPIRE, Istio, gateway and the kora charts.

**Pros:**
- One custom resource on a blank cluster.

**Cons:**
- Fails the three leading enterprise blockers.
- BYO Istio or SPIRE is a skip flag with no validation and no way to deliver kora's mesh configuration.
- Every chart value is reachable only through `orbitValues` passthrough.
- Operator holds wildcard verbs across seventeen API groups permanently.
- Version pins in Go; an Istio bump is an operator release.

### Option 2: Charts only, no operator

**Description:** Publish the charts; tenants become values or scripts.

**Pros:**
- Matches how comparable platforms distribute.

**Cons:**
- Per-tenant CA minting must not churn, aggregation of tenant-authored CRs is continuous, and the admission webhook needs a running server. None of that fits Helm.
- Pushes tenant lifecycle back to imperative steps, the blocker being removed.

### Option 3: Charts as the product, operator for tenant lifecycle only

**Description:** Publish `kora-crds` and `kora`. The operator keeps tenant namespaces, tenant CA, OPA config, sandbox NetworkPolicy, the aggregators and the webhook, and installs nothing. SPIRE and Istio are prerequisites with a contract and a preflight. A reference Helmfile composes prerequisites and charts for evaluators and local development.

**Pros:**
- Clears BYO mesh, GitOps transparency, RBAC split, registry and secret conventions by construction.
- Operator privilege follows from tenant duties.
- One artefact set for local, evaluator and enterprise paths.

**Cons:**
- Deletes the platform reconciler and the `OrbitPlatform` CRD.
- Blank-cluster install depends on Helmfile rather than a CR.
- Rewrites the deployment guide and supersedes two ADRs.

### Option 4: Option 3 plus an optional operator-managed prerequisites mode

**Description:** Keep `spire.enabled` and `istio.enabled` installing upstream charts for evaluators.

**Pros:**
- No new tool for evaluators.

**Cons:**
- Two install paths for one team; the secondary path decays.
- Keeps the broad RBAC and the version-pins-in-Go problem alive.

---

## Decision

**Chosen Option:** Option 3, Charts as the product, operator for tenant lifecycle only.

The decision decomposes into the sub-decisions below.

### Decision 1: Distribution model and operator scope

The published Helm charts are the product. The operator is part of the `kora` chart, not the install entry point. It reconciles `KoraTenant` and `MCPServer`, watches `Connection` and `OAuthClient` to trigger the owning tenant's reconcile, runs the aggregators, serves the admission webhooks, and runs the preflight. It does not watch Secrets; a rotated Connection or OIDC client Secret is picked up on the periodic reconcile instead. `PlatformReconciler`, `helm.go`, `istio_templates.go`, `loadValuesFile`, `orbitImageRef`, `chartVersions` and the `OrbitPlatform` type are deleted, and the Helm SDK leaves `backend/go.mod`.

The operator's ClusterRole, complete, including the rows the companion ADR's webhook and preflight need:

| API group | Resources | Verbs | Reason |
|---|---|---|---|
| core | namespaces | get, list, watch, create, patch | tenant namespaces, label repair, PSA labels |
| core | configmaps, secrets | get, list, watch, create, patch | aggregated registries, tenant CA, OPA config, pull-secret replication, webhook TLS |
| networking.k8s.io | networkpolicies | get, list, watch, create, patch | sandbox egress policy |
| runkora.io | koratenants, connections, oauthclients, mcpservers | get, list, watch | reconciliation inputs; the operator never writes CR bodies |
| runkora.io | koratenants/status, mcpservers/status | get, patch | conditions (the other two CRDs have no status subresource) |
| admissionregistration.k8s.io | mutatingwebhookconfigurations, validatingwebhookconfigurations | get, patch (scoped to the two named webhooks); list, watch (unscoped) | `caBundle` injection and failure-policy switch |
| apps | daemonsets | get | preflight: CNI presence (istiod itself is read from the injector ConfigMap) |
| spire.spiffe.io | clusterspiffeids | list | preflight: CSI driver and CRD presence proven indirectly (SVID fetch, List error), default ClusterSPIFFEID absent, kora registrations accepted |
| coordination.k8s.io | leases | get, create, update | leader election |
| core | events | create, patch | controller-runtime |

Secrets and ConfigMaps are excluded from the informer cache entirely (`client.CacheOptions.DisableFor`), so the operator never holds an in-memory watch of every Secret in the cluster; every read goes straight to the API server. This bounds memory, not authorization: RBAC (Decision 1's table) is what actually limits which Secrets and ConfigMaps the operator can touch, and today that table is a cluster-wide grant, not one scoped to kora's namespaces (tracked as [#269](https://github.com/clear-route/orbit/issues/269)). The tenant reconciler does not watch Secrets at all; a rotated OIDC client Secret or Connection credential is picked up on the periodic reconcile.

### Decision 2: Chart topology

Two charts.

**`kora-crds`** carries the `runkora.io` CRDs (`KoraTenant`, `Connection`, `OAuthClient`, `MCPServer`) as templates, not a `crds/` directory, each annotated `helm.sh/resource-policy: keep`. Templated CRDs are ordinary resources to Helm and Argo CD and upgrade with the release; a `crds/` directory never upgrades and forces a manual apply step on every user. Installing first keeps the in-chart `MCPServer` CR in `orbit` valid on a fresh cluster. Uninstalling `orbit` leaves tenant definitions in place. `deleteNamespaceOnRemoval` is removed from the `KoraTenant` schema; it was never implemented and tenant namespaces are always retained.

**`kora`** carries the operator, cluster-scoped identity and gateway resources, and every service. Chart version and application version are one number for one product. A chart-only fix therefore requires a full release; that cost is accepted for one version to reason about.

Folding today's `kora-operator` and `orbit-platform` templates into `kora` is a rewrite of their helpers, not a move: the operator's selector labels include the release name, its namespace comes from a value, `orbit-platform` labels lack `app.kubernetes.io/name`, and helper names collide with `orbit.*`. Objects that are recreated rather than upgraded: the operator Deployment (namespace changes), the webhook Service, the ClusterRole and ClusterRoleBinding, the webhook configurations, and the `kora-operator` namespace is deleted.

No toggles on required parts. The services, the ClusterSPIFFEIDs and the operator are one system; switches on required components produce untested combinations. Switches exist only where the customer has a substitute:

| Value | Default | Purpose |
|---|---|---|
| `networkPolicy.enabled` | `true` | CNIs that do not enforce policy |
| `gateway.create` / `gateway.name` | `true` / `kora-gateway` | `true`: the chart deploys kora's own Istio ingress gateway pods in the system namespace (upstream `istio/gateway` as a subchart) plus the `Gateway` resource; `false`: bind VirtualServices to a customer-owned Istio Gateway named by `gateway.name` |
| `gateway.ingress.enabled`, `frontend.ingress.enabled` | `false` | Kubernetes Ingress in front of the Istio gateway |
| `agents.imageWarmer.enabled`, `agents.warmHeadroom.enabled` | `false` | existing sandbox warm-start opt-ins |

kora deploys its own ingress gateway rather than depending on a shared one in the Istio namespace. Istio's guidance is a gateway deployed with the application, injected by the shared control plane. The `istio/gateway` subchart, pinned to the supported Istio range and enabled by `gateway.create`, renders in the system namespace with the SPIRE proxy wiring (Decision 4 of the companion ADR) carried by the `kora` chart, so a consumer adds nothing to their mesh for kora's gateway to obtain its identity, and no kora registration reaches into the Istio namespace. A team fronting kora with a shared gateway sets `gateway.create: false` and applies the documented wiring to that gateway.

The three-chart split (operator, platform, services) was dropped: its justification is a cluster-admin versus app-team RBAC boundary, and the services chart already carries gateway `EnvoyFilter`s and cluster-exported `DestinationRule`s, so no consumer installs it with namespace RBAC.

### Decision 3: Prerequisites contract

Istio and SPIRE are prerequisites. Per release kora publishes:

| Prerequisite | Requirement |
|---|---|
| Kubernetes | minors exercised by CI |
| Istio | current and previous minor; sidecar mode for kora's namespaces; `pilot.cni.enabled: true` on istiod with the Istio CNI DaemonSet running; `meshConfig.trustDomain` equal to kora's `global.trustDomain`; sidecar injection on kora's namespaces through `istio-injection: enabled` or an `istio.io/rev` label matching a live revision (`istio.revision` value) |
| SPIRE | `spiffe/spire` chart within the stated range; trust domain equal to kora's; SPIFFE CSI driver `csi.spiffe.io` registered; spire-controller-manager present with the `ClusterSPIFFEID` CRD served; the chart's default ClusterSPIFFEID absent or `fallback: true`; agent SDS enabled with `defaultSVIDName: default`, `defaultBundleName: ROOTCA`, `defaultAllBundlesName: ALL`; `spire.className` value equal to the controller manager's class name |
| PostgreSQL, Redis | reachable from the system namespace; credentials in the platform Secret |
| OIDC provider, model provider | as today; the reference install ships Dex for evaluation |

kora's ClusterSPIFFEIDs are scoped to what kora owns: the system namespace (which now contains kora's gateway pods) and namespaces labelled `runkora.io/role: tenant`. The `istio-sidecar` registration that today matches every non-Orbit namespace carrying `spiffe.io/spire-managed-identity` and the `istio-gateway` registration matching every `app: gateway` pod in `istio-system` are removed; a customer-owned gateway (`gateway.create: false`) is registered by its owner, and a customer's own default registration remains theirs.

No kora-specific Istio control-plane configuration is required. The injection templates the operator rendered into istiod are replaced by kora's own webhook and by chart-rendered pod specs (companion ADR). Istio CNI is required because the restricted pod security profile rejects the `istio-init` container's `NET_ADMIN`/`NET_RAW`; with `pilot.cni.enabled` istiod renders the unprivileged `istio-validation` container instead.

The preflight that verifies this contract is specified in the companion ADR.

### Decision 4: `OrbitPlatform` removal and operator configuration

The `OrbitPlatform` CRD is deleted. Its surviving inputs become chart values consumed by both the services and the operator:

```yaml
global:
  trustDomain: orbit.example.com     # required
  systemNamespace: ""                # defaults to .Release.Namespace
  imageRegistry: ghcr.io/runkora
  imageTag: ""                       # empty = chart appVersion; a release stamps per-component digests
  imagePullSecrets: []
  logFormat: json                    # applied to every service
  postgres:
    sslMode: require
    captureQueryText: false
  observability:
    otlpInsecure: false
config:
  db:    { host: "", port: 5432, name: orbit, user: orbit }
  redis: { host: "", port: 6379 }
  platformSecretName: kora-secrets
istio:
  namespace: istio-system
  revision: ""                       # empty = default revision, label istio-injection: enabled
  outboundTrafficPolicy: ALLOW_ANY   # REGISTRY_ONLY renders ServiceEntries for Orbit's external destinations
spire:
  className: spire-system-spire
```

Settings shared by every service live on `global`: log format, the Postgres SSL mode and query-text capture, the OTLP insecure flag, and the image registry and tag. A component block overrides one where it must. The chart reads the `global` value directly, so these fields are no longer repeated per component.

The system namespace is variable. The chart renders every namespaced object into `.Release.Namespace` and the operator and kora-agents receive `KORA_SYSTEM_NAMESPACE`; the literals in `registry.go`, `tenant_netpol.go`, `tenant_controller.go`, `agents/k8s.go` and `cmd/kora-agents/config/base.yaml` are removed. `kora-system` remains the default.

The chart renders the `orbit-config` ConfigMap from `config.*`. `config.platformSecretName` is the single Secret reference; the per-component `existingSecret` values collapse into it, with `frontend.existingSecret` kept because the BFF session secret is a different Secret. `platform.namespaces.tenants` is removed: tenant namespaces are created only by the operator from `KoraTenant`, or adopted when `spec.namespace` names an existing one, which removes the coupling that made the platform release depend on tenant CR state.

The `kora-tenants` ConfigMap stays operator-owned and is never rendered by the chart; a chart-owned object the operator patches is a perpetual GitOps diff that self-heal reverts. The identity Deployment's `tenants-config` volume becomes `optional: true` so identity starts before any tenant exists.

Status that lived on `OrbitPlatform` moves to operator metrics, events and `KoraTenant` conditions. There is no platform status CRD.

### Decision 5: GitOps cleanliness and ownership

The charts render identically under `helm template` with no cluster access and produce no perpetual diff:

- No `lookup`, `genCA`, `genSignedCert`, `randAlphaNum` or `now`. Today `lookup` appears in `charts/orbit-platform/templates/namespaces.yaml` and `charts/kora-operator/templates/webhook.yaml`; both templates go. Webhook TLS moves to the operator (companion ADR).
- No Helm hooks required for correctness.
- Every credential is an existing Secret; the charts create none.
- The system namespace is created and labelled outside the chart. Helmfile: `createNamespace` creates it and a `presync` hook labels it with `istio-injection: enabled` (or the `istio.io/rev` label) and `pod-security.kubernetes.io/enforce: restricted`. Argo CD: `managedNamespaceMetadata`. The deployment guide gives both.
- A published ownership table: every object the chart or operator creates or changes, its owner, and the fields the operator writes on objects it does not own (tenant namespace labels, webhook `caBundle` and `failurePolicy`, PSA labels). Argo CD `ignoreDifferences` follows from it.
- A NetworkPolicy `allow-apiserver-webhook` admitting the API server to the operator's webhook port; the API server is host-networked so the source is `networkPolicy.apiServerCIDRs` (default `0.0.0.0/0` on the webhook port only). Without it the default-deny in the system namespace blocks admission and, with a fail-closed webhook, every tenant pod.
- A published flow table in the deployment guide: every flow kora's shipped policies permit (istiod xDS and webhook ports, API server to the operator webhook, system namespace to and from tenant namespaces by port, sandboxes to the egress gateway, DNS, and egress to PostgreSQL, Redis, the identity provider and model providers), so a platform team authoring its own default-deny policies can permit them. `networkPolicy.enabled: false` is documented with the isolation it removes.
- `ServiceEntry` templates for the external destinations kora's services call (model providers, the identity provider, PostgreSQL and Redis when outside the cluster), rendered when `istio.outboundTrafficPolicy` is `REGISTRY_ONLY`, so kora works on meshes that restrict egress to registered hosts.

### Decision 6: Reference install and local development

The reference install is an example to copy, not a second configuration system. Its parts separate by owner: the `kora` chart's own values are the only kora configuration surface, the Helmfile only sequences releases, and everything specific to the local k3d cluster is a separate overlay.

**Chart values.** `deploy/reference/values/kora.yaml` is the file: chart values with the required fields set for a cloud cluster, the same set `helm show values` prints. There is no kora-specific environment schema and no template between it and the chart. Cross-cutting settings a translation layer would fan out across components are `global` values (Decision 4).

**The Helmfile sequences releases and passes the facts they share.**

```yaml
releases:
  - name: spire-crds  { namespace: spire-system, chart: spiffe/spire-crds }
  - name: istio-base  { namespace: istio-system, chart: istio/base }
  - name: istio-cni
    namespace: istio-system
    chart: istio/cni
    needs: [istio-system/istio-base]
    values: []                                # local adds values/cni.local.yaml (k3s CNI paths); a cloud cluster needs none
  - name: spire
    namespace: spire-system
    chart: spiffe/spire
    # Waits on istio-cni so SPIRE's agents land on nodes whose CNI plugin is in
    # place; a pod scheduled during the CNI install gap gets no network sandbox.
    needs: [spire-system/spire-crds, istio-system/istio-cni]
    values: [values/spire.yaml]               # what Orbit requires of SPIRE
    set:                                       # trust domain and cluster name from the environment
      - { name: global.spire.trustDomain, value: '{{`{{ .Values.trustDomain }}`}}' }
      - { name: global.spire.clusterName, value: '{{`{{ .Values.clusterName }}`}}' }
  - name: istiod
    namespace: istio-system
    chart: istio/istiod
    needs: [istio-system/istio-base, istio-system/istio-cni]
    values: [values/istiod.yaml]              # what Orbit requires of istiod: pilot.cni.enabled, discoveryAddress
    set: [{ name: meshConfig.trustDomain, value: '{{`{{ .Values.trustDomain }}`}}' }]
  - name: kora-crds  { namespace: '{{`{{ .Values.systemNamespace }}`}}', chart: '{{`{{ .Values.kora.crdsChart }}`}}' }
  - name: kora
    namespace: '{{`{{ .Values.systemNamespace }}`}}'
    chart: '{{`{{ .Values.kora.chart }}`}}'  # oci://ghcr.io/runkora/charts/kora in production
    needs: ['{{`{{ .Values.systemNamespace }}`}}/kora-crds', spire-system/spire, istio-system/istiod]
    values: [values/kora.yaml]               # the consumer's chart values
    set:                                       # the two facts SPIRE, Istio and Orbit share
      - { name: global.trustDomain,    value: '{{`{{ .Values.trustDomain }}`}}' }
      - { name: global.systemNamespace, value: '{{`{{ .Values.systemNamespace }}`}}' }
```

The Helmfile orders the releases and injects the trust domain, cluster name and system namespace each chart needs. SPIRE and istiod take a plain values file stating only what kora requires of them, with the shared facts set from the environment; the `orbit` release takes `values/kora.yaml`. No values file in the reference is templated. Version numbers, chart sources and the two shared facts come from the environment. A consumer already running SPIRE and Istio installs the `kora` chart directly with their own values and needs no Helmfile at all.

There is no separate gateway release. kora's ingress gateway pods, their `Gateway` resource and their identity registration are all in the `orbit` release. The reference install depends on nothing outside kora's ownership except the prerequisites. `deploy/reference/values/gateway-byo.yaml` documents the wiring a customer-owned gateway needs when `gateway.create` is `false`.

**Environments carry cluster facts, not chart configuration.** `deploy/reference/environments/{local,example}.yaml` hold the Helmfile's release inputs: chart versions, trust domain, cluster name, system namespace, kube context, and for local the chart paths and registry. `example.yaml` targets a cloud cluster; `local.yaml` targets k3d.

**Local development is an overlay, not the shipped reference.** The image digests `redeploy.sh` records, the k3s CNI paths, and the identity `hostAliases` and external-service CIDRs `resolve-docker-ips.sh` generates are local concerns. They live in `values/kora.local.yaml`, `values/cni.local.yaml` and the gitignored `local.generated.yaml`, which the local environment layers onto the base values. The base `values/kora.yaml` carries none of them.

`deploy/reference/argocd/` holds an app-of-apps: one `Application` per release with `argocd.argoproj.io/sync-wave` on the `Application` objects, since an `ApplicationSet` gives no ordering between the Applications it generates. The `orbit` Application points its values at the consumer's own GitOps repository, the values `values/kora.yaml` demonstrates. While `ghcr.io/runkora/charts` is private, pulling `kora-crds`/`orbit` needs OCI registry credentials the app-of-apps does not provide; a comment marks where they belong, and the repository credential itself is the consumer's Argo CD setup (`argocd repo add` or a Repository credential template). Flux is not a supported install path in v1; the charts are standard Helm charts and would work under it, but no example is published or maintained.

`make up` becomes: `dev-cluster` (k3d, Postgres, Redis, Dex), create and label the system namespace, `create-secrets.sh`, `resolve-docker-ips.sh`, `helmfile -e local apply`, then tenant fixtures. `cluster-operator`, `cluster-operator-chart` and the Makefile's readiness waits are removed. `redeploy.sh` changes: the operator's namespace comes from the system namespace, `cluster-update` no longer calls `cluster-operator-chart`, and after each push `redeploy.sh` pins the workload with `kubectl set image` and records the digest under `<component>.image.digest` in `local.generated.yaml`; the next `helmfile apply` renders the same image. `make up` runs `helmfile apply` unconditionally; Helmfile diffs before applying, so an unchanged environment is a no-op.

`deploy/local/` is reduced to tenant fixtures and connection examples. `Dockerfile.operator` copies the binary and the per-binary `config/*.yaml` defaults, no charts or `deploy/` content. kora-agents' sandbox and session-manager images and trust domain move from `base.yaml` literals to env overrides in the existing `ORBIT_SESSION_MANAGER_*` pattern, set by the chart from `agents.config.*`; the `production.yaml` file overlay is not used.

### Decision 7: Publishing, versioning and verification

A `release.yml` workflow on `v*` tags, reusing `docker.yml` through `workflow_call` so the two do not race for the same build cache:

1. Build and push every image at the tag for `linux/amd64` and `linux/arm64`, capturing each digest.
2. Resolve digests for the third-party images the chart references (OPA).
3. Set `version` and `appVersion` on both charts to the tag, and write every digest into the packaged chart's `values.yaml` at `helm package` time. Source `values.yaml` keeps empty digests so local development uses tags and the tag matches the published source.
4. `helm push` both charts to `oci://ghcr.io/runkora/charts/`.
5. Sign charts and images with keyless cosign, using the release workflow's GitHub OIDC identity. Signatures go to the public Sigstore transparency log. Attach SLSA provenance. The workflow holds `id-token: write`, `packages: write`, `attestations: write`.
6. Publish release assets: `images.txt` (every image the release publishes, plus the third-party images the chart references), `compatibility.yaml` (supported Kubernetes, Istio, SPIRE, Helm, Helmfile and helm-diff versions), and release notes generated from conventional commits with a breaking-changes section.

Chart CI on every pull request touching `charts/`, `deploy/reference/`, `backend/internal/operator/` or `backend/internal/agents/k8s*.go`: `ct lint` with `values.schema.json` (`check-version-increment: false`, versions are stamped at release), `helm template | kubeconform` per supported Kubernetes minor with `istioctl analyze --use-kube=false` on the rendered output for the Istio resources kubeconform has no schema for, the restricted pod security check from the companion ADR, and a k3d matrix that runs the reference Helmfile against Istio N and N-1 (k3d rather than kind so CI and local development use the same cluster tooling). The Argo CD example is render-validated; exercising it as an install is a future improvement. Renovate tracks the Istio and SPIRE versions in `deploy/reference/`.

The GHCR packages are public. The charts and images pull without credentials.

### Decision 8: Compatibility statement, upgrades and rollback

Each release names: Kubernetes minors (CI matrix), Istio minors (current and previous, sidecar mode, CNI required), the SPIRE chart range, Helm 4, and the Helmfile and helm-diff minimums that support it. Istio ambient and managed meshes are listed as unsupported until validated. Minor version skipping is unsupported for chart upgrades. CRD schemas are backward compatible within a major version, and `kora-crds` upgrades before `orbit` on every upgrade, not only on install.

Rollback is `helm rollback orbit` (or the GitOps equivalent). CRDs are never rolled back; a release that adds a field keeps the field. Schema migration ordering is outside this ADR.

---

## Architecture

### Install topology

```
                    ┌──────────────────────────────────────────────────────────┐
  customer-owned    │  Prerequisites (customer installs, or reference Helmfile)  │
                    │  spire-crds → spire (+CSI driver, controller-manager)      │
                    │  istio-base → istio-cni → istiod (pilot.cni.enabled)       │
                    └──────────────────────────────────────────────────────────┘
                                             │  contract verified by preflight
                    ┌──────────────────────────────────────────────────────────┐
  kora-crds         │  KoraTenant, Connection, OAuthClient, MCPServer CRDs      │
                    └──────────────────────────────────────────────────────────┘
                                             │
                    ┌──────────────────────────────────────────────────────────┐
  kora              │  operator (tenant reconcile, aggregators, webhooks,       │
                    │            preflight)                                      │
                    │  platform: ClusterSPIFFEIDs (Orbit-scoped), Gateway        │
                    │            resource, ingress gateway pods (gateway.create),│
                    │            NetworkPolicies incl. apiserver → webhook       │
                    │  services: identity, policy, inference, agents, egress,    │
                    │            tool-gateway, kora-mcp, triggers, frontend,    │
                    │            opa-gateway                                     │
                    └──────────────────────────────────────────────────────────┘
                                             │  KoraTenant CRs (GitOps)
                    ┌──────────────────────────────────────────────────────────┐
  per tenant        │  namespace (PSA restricted), tenant CA, OPA config,       │
  (operator-owned)  │  sandbox netpol, replicated pull secrets,                  │
                    │  agent StatefulSets + sandboxes (kora-agents at runtime) │
                    └──────────────────────────────────────────────────────────┘
```

### `kora` chart values, top level

```yaml
global:
  trustDomain: ""                 # required
  systemNamespace: ""             # defaults to .Release.Namespace
  imageRegistry: ghcr.io/runkora
  imageTag: ""                    # empty = chart appVersion
  imagePullSecrets: []
  imagePullPolicy: IfNotPresent
  logFormat: json                 # every service; component overrides where needed
  postgres:
    sslMode: require
    captureQueryText: false
  observability:
    otlpInsecure: false
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []
  priorityClassName: ""
frontendHost: ""                  # required; api.<frontendHost> is the API host
publicHosts: []
config: { db: {...}, redis: {...}, platformSecretName: kora-secrets }
istio: { namespace: istio-system, revision: "", outboundTrafficPolicy: ALLOW_ANY }
spire: { className: spire-system-spire }
platform: { spiffe: { enabled: true } }
networkPolicy: { enabled: true, externalServices: [], apiServerCIDRs: ["0.0.0.0/0"] }
gateway: { create: true, name: kora-gateway, ports: {...}, tls: {...}, ingress: {...}, byo: {...} }   # create=false: bind to an existing Gateway named here; service.type lives under ingressGateway
ingressGateway: { name: kora-gateway, service: {type: LoadBalancer}, env: {...}, volumes: [...] }     # upstream istio/gateway subchart values, used when gateway.create is true
operator: { image: {...}, replicas: 2, podDisruptionBudget: {...}, webhook: {...}, injection: {...}, preflight: {...} }
identityProxy: { image: {...}, resources: {...} }   # the injected sidecar's own image and resources; operator.injection has no image block (Decision 4 of the companion ADR)
identity: ...
inferenceGateway: ...             # includes bedrock.accounts[], serviceAccount.annotations
agents: ...                       # includes config.sessionManagerImage, config.sandboxImage, extraEnv, manifestPatches, imageWarmer, warmHeadroom
policyService: ...
policyGateway: ...
egressGateway: ...
toolGateway: ...
koraMcp: ...
triggers: ...
frontend: ...                     # includes existingSecret, ingress
opaGateway: ...                   # the gateway-level OPA's own image, distinct from the tenant sidecar OPA the webhook injects
```

Every component block accepts `image.{registry,repository,tag,digest,pullPolicy}`, `replicas`, `resources`, `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName`, `podAnnotations`, `podLabels`, `extraEnv`, `extraVolumes`, `extraVolumeMounts`, `serviceAccount.annotations`, and the service-config keys (log format, Postgres SSL mode, OTLP) that default from `global`. Global values are defaults; component values override. Pods kora-agents creates at runtime (not managed by Helm directly) take scheduling, storage, and other field-level customisation through `agents.manifestPatches`, a map of operator-authored kustomize patches mounted into kora-agents via a ConfigMap (see [Config-Driven Kustomize Manifest Patches for orbit-agents](2026-09-15-manifest-patches-configmap.md)), plus a first-class `ORBIT_AGENTS_IMAGE_PULL_SECRETS` setting for `global.imagePullSecrets`. The operator replicates the named pull Secrets into every tenant namespace during tenant reconcile so that runtime-created and webhook-injected containers can pull.

### Image reference helper

```
{{- define "kora.image" -}}
{{- $img := (index .root.Values .name).image -}}
{{- $reg := $img.registry | default .root.Values.global.imageRegistry -}}
{{- $tag := $img.tag | default .root.Values.global.imageTag | default .root.Chart.AppVersion -}}
{{- if $img.digest -}}
{{ $reg }}/{{ $img.repository }}@{{ $img.digest }}
{{- else -}}
{{ $reg }}/{{ $img.repository }}:{{ $tag }}
{{- end -}}
{{- end }}
```

Takes `(dict "root" $ "name" "<values key>")`, the values key being the component's top-level block. Tag resolution is component `image.tag`, then `global.imageTag`, then the chart appVersion, so local development pins every deployed image with one `global.imageTag` and a release pins each with a `digest`. This covers the twelve chart-deployed images. The two images kora-agents runs at runtime, `orbit-session-manager` and `kora-sandbox-base`, are referenced by full string in `agents.config`. A release pins `orbit-session-manager` through `agents.config.sessionManagerDigest`, and `kora-sandbox-base` resolves by tag. A sibling `kora.imageRef` takes `(dict "root" $ "image" <image map>)` for a nested image block that isn't a top-level values key, such as `identityProxy.image` or `opaGateway.image`.

---

## Implementation

### Phase 1: Chart reshaping

1. Create `charts/kora-crds` from the CRD templates in `charts/kora-operator/templates/crd-*.yaml` minus `crd-orbitplatform.yaml`; remove `deleteNamespaceOnRemoval` from the `KoraTenant` schema; annotate `keep`; add `Chart.yaml`, `values.schema.json`, `NOTES.txt` stating the install order.
2. Rewrite the operator and platform templates into `charts/orbit/templates/operator/` and `templates/platform/` with `kora.*` helpers and `.Release.Namespace`; delete `namespaces.yaml`; scope the ClusterSPIFFEIDs to kora's namespaces; add `allow-apiserver-webhook`; add the `istio/gateway` subchart dependency under `gateway.create` with kora's SPIRE proxy wiring, service type and scheduling passed through values. Delete `charts/kora-operator` and `charts/orbit-platform`.
3. Introduce `kora.image`, `kora.scheduling` and `kora.imagePullSecrets` helpers; apply to every Deployment, DaemonSet, StatefulSet template and to the operator. Add the cross-cutting `global` settings (`logFormat`, `postgres.sslMode`, `postgres.captureQueryText`, `observability.otlpInsecure`, `imageTag`) and default every component's service-config and image to them, removing the per-component repetition of these fields from `values.yaml`.
4. Close the configuration gaps: `agents.config.*` rendered as env overrides for session-manager image, sandbox image, git author and trust domain; `agents.extraEnv`; `agents.runtimePods.*`; `inferenceGateway.bedrock.accounts[]`; `inferenceGateway.serviceAccount.annotations`; identity mounts `kora-tenant-oauth-clients`; identity's tenants mount `optional: true`; ext_proc `message_timeout: 1s`; policy-service bundle AuthorizationPolicy `ports`; `gateway.create`/`gateway.name`; `gateway.ingress` and `frontend.ingress`, which when enabled also set `x-forwarded-proto: https` on API routes because the fronting Ingress terminates TLS; warmer and headroom templates.
5. Render `orbit-config` from `config.*`. Remove `platform.namespaces.tenants`. Collapse `existingSecret` values into `config.platformSecretName`.
6. Switch defaults to `ghcr.io/runkora`; remove every `orbit-registry:5001` default outside `deploy/reference/environments/local.yaml`.
7. `values.schema.json` for both charts; helm-docs README; the ownership table in `docs/guides/deployment.md`.

### Phase 2: Operator narrowing

1. Delete `platform_controller.go`, `helm.go`, `istio_templates.go`, the `OrbitPlatform` types in `models.go`, `loadValuesFile`, `orbitImageRef`, `chartVersions`. Remove the Helm SDK from `backend/go.mod`.
2. Operator reads `ORBIT_TRUST_DOMAIN`, `KORA_SYSTEM_NAMESPACE`, `ORBIT_ISTIO_NAMESPACE`, `ORBIT_ISTIO_REVISION`, `ORBIT_SPIRE_NAMESPACE`, `ORBIT_IMAGE_PULL_SECRETS` from the chart; every namespace literal in Go is removed; tenant namespaces get the injection label matching `istio.revision` and the PSA labels; pull Secrets are replicated.
3. Scope the informer cache; shrink `clusterrole.yaml` to the table in Decision 1.
4. `Dockerfile.operator` copies the binary and the per-binary `config/*.yaml` defaults, no charts or `deploy/` content.
5. Webhook injection, validating webhook, failure policy, TLS and preflight per the companion ADR.

### Phase 3: Reference install and local development

1. `deploy/reference/helmfile.yaml.gotmpl` (release sequencing and the shared-fact `set:` blocks), `values/kora.yaml` (the consumer's chart values), `values/spire.yaml` and `values/istiod.yaml` (what kora requires of each prerequisite, shared facts set from the environment), `values/gateway-byo.yaml`, `environments/{local,example}.yaml` (cluster facts for the Helmfile), `argocd/` app-of-apps.
2. `make up` sequence per Decision 6; remove `cluster-operator`, `cluster-operator-chart`; keep `cluster-deploy` for fixtures; `redeploy.sh` changes per Decision 6; `resolve-docker-ips.sh` writes `local.generated.yaml` as plain chart values; k3d's Kubernetes version moves inside the supported window.
3. Local-only chart values (k3d registry, `imagePullPolicy: Always`, k3s CNI paths, recorded digests, generated host aliases and external-service CIDRs) live in `values/kora.local.yaml` and `local.generated.yaml`, layered onto `values/kora.yaml` by the local environment. Delete `deploy/local/orbit-platform.yaml` and the old `deploy/local/{orbit,platform,operator}-values.yaml`.
4. Update `docs/contributing/local-development.md` and `deploy/local/README.md`.

### Phase 4: Release and verification

1. `.github/workflows/release.yml` and `charts-ci.yml` per Decision 7; `docker.yml` gains `workflow_call`.
2. Rewrite `docs/guides/deployment.md`: prerequisites contract with the constraints on customer systems, quickstart (tools, minimum cluster size, credentials, reference Helmfile, teardown), production install (Helm, Argo CD, namespace labelling per path), the supported ingress topologies with DNS and TLS requirements, private preview step, upgrade and rollback, uninstall, ownership table, outbound destinations, compatibility statement, verification against a mirror, migration.
3. Update `AGENTS.md`, `docs/code-organization.md`, `ARCHITECTURE.md` and the diagrams under `docs/diagrams/` where they show the operator installing the platform.
4. Mark the 2026-03-31 operator ADR Superseded and add an amendment note to the chart-distribution ADR when this ADR is accepted.

### Migration Path

Local development: `make down && make up` after Phase 3 lands. No data migration.

A deployment driven from its own GitOps repository with copied charts:

1. Annotate the four CRDs, the ClusterRole, ClusterRoleBinding and webhook configuration with `helm.sh/resource-policy: keep`.
2. Remove the `kora-operator` and `orbit-platform` release records (delete their `sh.helm.release.v1.*` Secrets) so no uninstall runs against them.
3. Relabel the kept cluster-scoped objects with `meta.helm.sh/release-name` and `meta.helm.sh/release-namespace` for `kora-crds` (CRDs) and `orbit` (the rest).
4. Replace the three chart sources with `oci://ghcr.io/runkora/charts/kora-crds` and `oci://ghcr.io/runkora/charts/kora`; move environment values into the single `kora` values file; install.
5. Recreated objects: the operator Deployment, webhook Service, ClusterRole, ClusterRoleBinding and webhook configurations. Deployment `spec.selector` is immutable, so any service Deployment whose selector changes is deleted and recreated in a maintenance window.
6. Delete the replicated istiod injection templates and the vendored charts; delete the `kora-operator` namespace.

Tenant CRs, tenant namespaces, tenant CAs and aggregated ConfigMaps are untouched throughout.

---

## Consequences

### Positive

- An installable, signed, versioned release exists.
- kora has one configuration surface: the `kora` chart's own values. The reference carries no second schema and no template translating one schema into another, so a consumer writes chart values directly in their own repository.
- Cross-cutting settings are `global` values set once, not fields repeated per component in the chart and again in a fan-out layer above it.
- BYO Istio and SPIRE become real, with a contract kora can verify rather than a skip flag.
- One artefact set and one install path from local k3d to enterprise GitOps.
- Operator RBAC drops from wildcard verbs on seventeen API groups to the table in Decision 1, and it no longer holds an in-memory cache of every Secret in the cluster. The RBAC grant on Secrets and ConfigMaps is still cluster-wide, not scoped to kora's namespaces ([#269](https://github.com/clear-route/orbit/issues/269)).
- Helm SDK, chart baking and local-values-in-production-image all leave the operator.
- kora's ingress gateway is a kora workload: consumers add nothing to their mesh for it, and kora's identity registrations stay in kora's namespaces.

### Negative

- `OrbitPlatform` is removed; local development, the deployment guide and any deployment applying it change at once.
- Blank-cluster install requires Helmfile. The quickstart lists it alongside Helm and kubectl. A consumer who already runs SPIRE and Istio installs the `kora` chart alone and needs neither the Helmfile nor the reference environments.
- Two charts means an ordering rule on install and upgrade; `kora` fails with a clear message when `kora-crds` is absent.
- A chart-only fix needs a full release.
- The `kora` chart pins the `istio/gateway` subchart version, which must move with the Istio compatibility window.
- The Makefile's readiness choreography is replaced by Helmfile `--wait`; early failures surface as Helm errors on a named release.

### Neutral

- Chart version and application version become the same number.
- `deploy/local/` shrinks to fixtures and helpers.
- Release names change once for existing deployments.

---

## Validation

### Success Metrics

- `make up` on a fresh k3d cluster produces a completed agent run in under thirty minutes wall clock, including image pulls.
- `helm template` of both charts succeeds with no cluster access; `kubeconform` and `istioctl analyze` pass for every supported Kubernetes minor.
- The k3d matrix installs the reference Helmfile on Istio N and N-1 with CNI and SPIRE, the preflight reports the contract satisfied, and kora's gateway pods hold an SVID in kora's trust domain.
- The matrix uninstalls `orbit`, confirms `KoraTenant` objects, tenant namespaces and the CRDs remain, and reinstalls without manual steps.
- The matrix runs on k3d with its NetworkPolicy controller enabled and asserts that a sandbox cannot reach the internet except through the egress gateway (`scripts/cluster/test-egress-lockdown.sh`) and that a documented flow succeeds.
- `cosign verify` succeeds for both charts and every kora image in `images.txt`, including against a mirrored copy.
- Operator ClusterRole contains no wildcard verbs, and the operator's informers list Secrets only in kora's namespaces.

### Monitoring

- Chart CI on every pull request touching the paths in Decision 7.
- Release workflow fails if any image lacks a digest or any chart lacks a signature.
- Renovate tracks the Istio and SPIRE versions in `deploy/reference/`; a bump PR must pass the k3d matrix.

---

## Amendment 1 (2026-10-24): The operator projects declared resources

**Status:** Accepted

[Declarative Resources](2026-10-24-declarative-resources.md) makes the operator the single projector for `VirtualModel`, `Environment`, `Agent`, `TriggerSource`, `Trigger` and `PolicyRole` resources. This touches *What can it access?* The four owning services gain no Kubernetes access. *Who is this agent?* and *How does it connect?* are unchanged.

**Decision.**

*   The operator's role widens from tenant reconciliation to tenants and declared resources.
*   The operator joins the mesh for outbound calls. Its webhook port is excluded from inbound interception, so the kube-apiserver still reaches it without a mesh certificate.
*   The operator calls each owning service's `/internal/declared/` endpoints, under the service's base path, with a platform token from its SPIFFE identity.
*   The operator ClusterRole gains get/list/watch on the six kinds and get/patch on their status.

**Consequences.**

*   The operator's start depends on the mesh issuing it a certificate.
*   Declared changes wait while the operator is down. Tenant reconciliation and the webhook are unaffected by an owning service being down.

---

## Related Decisions

- [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md), superseded: the platform reconciler, `OrbitPlatform`, version pins in Go and chart baking are removed; tenant reconciliation, aggregators and the webhook are retained.
- [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md), amended: two charts instead of two-with-subcharts; Istio and SPIRE are prerequisites rather than subcharts; publishing is no longer deferred; the dog-food principle is kept through the reference Helmfile.
- [Declarative Resources](2026-10-24-declarative-resources.md): Amendment 1.
- [Enforcement Sidecar Injection, Preflight and Restricted Pod Security](2026-09-04-enforcement-sidecar-injection-and-preflight.md), companion: how kora's sidecars and mesh annotations reach pods without istiod injection templates, the preflight, and restricted profile compliance.
- [North-South Ingress and External Service Exposure](2026-07-04-north-south-ingress-and-external-service-exposure.md): `frontendHost` and the `api.` convention are unchanged; `gateway.create`/`gateway.name` and the Ingress options bind to it.
- [Kubernetes Manifest Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md): the mechanism through which chart-level scheduling values reach runtime-created pods.

---

## References

- Istio in-cluster operator deprecation: https://istio.io/latest/blog/2024/in-cluster-operator-deprecation-announcement/
- Istio CNI and pod security: https://istio.io/latest/docs/setup/additional-setup/cni/
- Helm CRD best practices: https://helm.sh/docs/chart_best_practices/custom_resource_definitions/
- Argo CD Helm behaviour (hooks, `lookup`, random values): https://argo-cd.readthedocs.io/en/stable/user-guide/helm/
- Argo CD sync options and app-of-apps ordering: https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/
- Helmfile: https://helmfile.readthedocs.io/
- cosign: https://docs.sigstore.dev/cosign/

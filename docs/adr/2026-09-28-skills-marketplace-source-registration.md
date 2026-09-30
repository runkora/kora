# Skills Marketplace: GitOps Source Registration via Namespaced SkillMarketplaceSource CRDs


**Status:** Proposed
**Date:** 2026-09-29
**Deciders:** Platform Engineering, Operator Engineering, Product Owner

---

## Context

A tenant admin needs a way to declare which GitHub repositories are Skills
Marketplace sources for their tenant, through GitOps, and have that declaration
reach `skills-service` in the shape the backend has already fixed.

How `skills-service` *reads* those sources is settled: it mounts a ConfigMap,
`orbit-skill-marketplace-sources`, as a read-only file
(`marketplace-sources.json`), never imports the CRD type, and never calls the
Kubernetes API. The shape of one tenant's source list is also fixed — an array
of plain objects, with no `tenant_id` field inside any of them:

```json
marketplace-sources.json=
[
  { "name": "Clear Route Skills", "org": "clear-route", "repo": "skills-marketplace", "branch": "main" },
  { "name": "ClearRoute Core",    "org": "clearroute",  "repo": "core-skills",        "branch": "main" },
  { "name": "Partner Reference",  "org": "partner",     "repo": "reference-skills",   "branch": "main" }
]
```

What is *not* settled is how that array gets produced, or how a single mounted
file carries more than one tenant's array when no entry names its tenant.

### Background

Orbit already has three CRDs that solve "many tenant-namespaced CRs → one
`orbit-system` ConfigMap": `Connection`, `MCPServer` and `OAuthClient`. Each has
an aggregator (`connection_aggregator.go`, `mcpserver_aggregator.go`,
`oauthclient_aggregator.go`) that lists every active `OrbitTenant`, maps
`namespace -> tenant_id`, collects every CR cluster-wide, and renders one
ConfigMap in `orbit-system`.

All three render a **flat array with `tenant_id` on every entry** — the shape
the backend's confirmed JSON explicitly does not use. Reconciling "reuse the
established aggregator mechanics" with "match the backend's confirmed envelope
shape" is the substance of this decision.

`skills-service` reaches GitHub through the backend's own `TokenProvider`
rather than the Egress Gateway, so a source declaration carries no credential
and no `Connection` reference. The operator never contacts GitHub and never
handles a credential.

### Problem Statement

Define how a tenant admin registers a GitHub repository as a Skills Marketplace
source through GitOps, producing a single `orbit-skill-marketplace-sources`
ConfigMap whose content matches the backend's confirmed per-tenant array shape,
without inventing a fourth, differently-shaped aggregation mechanism.

### Goals

- **REQ-1** — Sources are declared through GitOps, so every change is reviewed,
  versioned and can be rolled back.
- **REQ-2** — Sources are per-tenant, and a tenant can declare more than one.
- **REQ-3** — Only approved people can add or change a tenant's sources. One
  tenant cannot register or change sources for another.
- **REQ-4** — The output is one ConfigMap, `orbit-skill-marketplace-sources`, in
  `orbit-system`, with key `marketplace-sources.json`, keyed by tenant, each
  value an array of `{ name, org, repo, branch }`. No `tenant_id` inside the
  entries.
- **REQ-5** — `skills-service` reads a mounted file only. It does not call the
  Kubernetes API or import a CRD type, and it hot-reloads the file.
- **REQ-6** — No credentials appear in the declaration or in the ConfigMap.
- **REQ-7** — An invalid declaration is rejected visibly, and never blocks
  another tenant's sources.
- **REQ-8** — Adding, changing or removing a source takes effect without
  redeploying or upgrading Orbit.
- **REQ-9** — Every declared source's registration state (accepted, or rejected
  with a reason) reaches `skills-service` without Kubernetes API access, so the
  Sources view can show it — including rejected sources. Delivered outside
  `marketplace-sources.json`, so REQ-4's shape is untouched.
- **REQ-10** — Source names are unique within a tenant **after slugifying**,
  because the backend builds skill IDs and URLs from the slugified name.
  "ClearRoute Core" and "clearroute-core" collide.

### Constraints

- **Orbit grants tenant admins no RBAC.** The chart ships ClusterRoles only for
  the operator and `orbit-agents`. Whether a tenant admin can apply resources in
  their own namespace is decided by the customer's own cluster RBAC, so
  self-service by tenant admins is never automatic, and REQ-3 is bounded by it.
- **A second ConfigMap key needs the backend's agreement.** Registration state
  cannot go inside `marketplace-sources.json` without breaking its fixed shape,
  so it must travel separately — and `skills-service` reading a second key is
  the backend's call. REQ-9 is bounded by it.
- **The tenant key needs a definition.** Existing aggregators use the
  `OrbitTenant` resource name as the tenant ID. Nothing yet states that this is
  the same value `skills-service` resolves from the caller's identity, and the
  two must match, or every tenant resolves to an empty source list.
- **Registration state is not runtime health.** Whether a source was accepted is
  a separate question from whether GitHub is reachable and whether the crawled
  data is current, stale or degraded; the latter remains `skills-service`'s own
  runtime state.
- **ConfigMap updates are not instant.** The kubelet refreshes mounted
  ConfigMaps on its sync period, up to about a minute by default, and the mount
  must not use `subPath`, because a `subPath` mount never receives updates.
- **A ConfigMap is limited to 1 MiB.** At roughly 100 bytes per source this is
  far off, but it is a hard limit on a single ConfigMap shared by all tenants.

### Non-Goals

- Anything the backend already owns: the GitHub App `TokenProvider`, Redis
  caching, the skill data model, pagination, or the HTTP API.
- A platform-wide / `orbit-system`-scoped marketplace source — approval is
  scoped to "which GitHub repositories are Marketplace sources for **my
  tenant**."
- Validating that the declared repository, branch, or
  `.claude-plugin/marketplace.json` actually exist — the operator has no GitHub
  credential; that check belongs to `skills-service`, at crawl time.
- Dashboard-driven source management — GitOps only.
- A `crds/` directory or a second Helm chart — `SkillMarketplaceSource` is a
  templated CRD in the existing operator chart, same as `Connection` and
  `MCPServer`.

---

## Decision Drivers

* **The backend's confirmed JSON shape is a fixed constraint**, not a starting
  point to negotiate from — whatever is designed here must render exactly that
  per-tenant array.
* **No self-declared tenant scope on the CRD.** A field a tenant admin fills in
  themselves is not an isolation boundary; the namespace a CR is applied into,
  gated by RBAC, is.
* **Reuse aggregation mechanics where they still fit**
  (`Connection`/`MCPServer`/`OAuthClient`'s `activeTenantNamespaces` + collect +
  render loop), without forcing their flat-array-with-`tenant_id` output shape
  onto a contract that has already been fixed differently.
* **Operator stays config-only** — it validates and aggregates; it never calls
  GitHub and never sees a credential.
* **Practicality of the resulting ConfigMap** — it must stay within Kubernetes'
  object size limits and remain something `skills-service` can parse with one
  `json.Unmarshal`.

---

## Considered Options

### Option 1: Namespaced `SkillMarketplaceSource` CRD, aggregated by the operator

**Description:** A new namespaced CRD, applied in the tenant's own namespace,
with spec fields `displayName`, `org`, `repo` and `branch` (defaulting to
`main`) and no tenant field. The tenant is resolved from the namespace via the
active-`OrbitTenant` lookup the existing aggregators already share. A new
aggregator validates every CR, renders the tenant-keyed ConfigMap, and sets each
CR's status in the same pass. The Kubernetes object name is unconstrained; the
slug-uniqueness rule is enforced by the aggregator, which claims slugs
tenant-by-tenant in a deterministic order and rejects a later claimant. A CR
applied in `orbit-system` or in a namespace with no active owning tenant is
rejected with its own distinct reason rather than silently dropped.

**Pros:**
- The tenant is never a value an admin types — it comes from which namespace the
  CR was allowed into, the boundary every other tenant-owned CR already relies on.
- Each CR carries its own phase and condition, so a rejection is visible in
  `kubectl get` with a reason (REQ-7).
- Suspending or deleting a tenant drops its sources automatically through the
  existing `TenantReconciler` re-sync hook, with no separate cleanup step.
- REQ-9 and REQ-10 both fall out of the one pass that already has to see every
  source in a tenant at once.
- Closely mirrors `OAuthClient`, which is the same shape of problem: many
  tenant-namespaced, credential-free, GitOps-declared CRs collected into one
  `orbit-system` ConfigMap that a feature service mounts.

**Cons:**
- A fifth CRD, controller and aggregator to maintain alongside `OrbitTenant`,
  `Connection`, `MCPServer` and `OAuthClient`.
- The render step is genuinely new code: the three existing aggregators emit a
  flat array with `tenant_id` per entry, which is the shape REQ-4 rules out.
- Sources are spread across namespaces, so "every source on the platform" needs
  a cluster-wide list; there is no single-namespace view.
- Moving a source between tenants means deleting the CR and reapplying it in the
  other namespace — there is no move operation.
- Status has to live in the aggregator rather than the per-CR reconciler,
  because the slug-collision check is inherently cross-CR. That is a departure
  from how `MCPServerReconciler` is structured and needs documenting.

### Option 2: `SkillMarketplace` modelled on `MCPServer`, adjusted to the backend's shape

**Description:** Follow `MCPServer`'s registration path closely — types,
reconciler, tenant re-sync hook, RBAC, optimistic-lock patch — and change three
things. The render step groups entries by tenant instead of emitting
`tenant_id`. `metadata.name` must equal the slugified `displayName`, so
Kubernetes' own per-namespace name uniqueness enforces REQ-10. `MCPServer`'s
global (`orbit-system`) scope is retained but *expanded* into every active
tenant's array at render time, behind an operator value that is off by default;
with it off, a CR in `orbit-system` is rejected, and a cross-namespace check
rejects a tenant CR whose name is already held by an accepted global. `branch`
is required rather than defaulted.

**Pros:**
- Follows Orbit's existing conventions about as literally as possible: anyone
  who knows how `MCPServer` is registered already knows this.
- Slug uniqueness within a namespace is enforced by Kubernetes rather than by
  operator code — two objects cannot share a name.
- A display-name change that would break every skill ID under that source is
  rejected outright, turning an accidental breaking change into a deliberate one.
- Platform-wide sources become available later by flipping the flag, with no
  backend change.
- Each CR has its own status and condition, visible in `kubectl get`.

**Cons:**
- The name-equals-slug rule is one no other Orbit CRD has: an admin must compute
  `clearroute-core` from `ClearRoute Core` by hand, and a rename becomes a
  delete-and-recreate.
- It makes the shared slug algorithm load-bearing for *applying a CR at all*,
  not merely for detecting a collision. Until the operator and `skills-service`
  are committed to an identical algorithm, a mismatch makes otherwise valid CRs
  unappliable.
- The global path carries cost before it carries value: with the flag on, each
  global source is crawled once per tenant, multiplying GitHub API calls against
  a single App's rate limit — a real risk at scale — and V1 does not want global
  sources at all.
- Requiring `branch` is stricter than the backend's shape needs.
- It is a pattern copy that deliberately deviates in two places, which must be
  documented clearly or someone will later "fix" it back toward `MCPServer`.

### Option 3: CRD in `orbit-system` with an explicit tenant field

**Description:** The same CRD, but every CR is applied in `orbit-system` and
names its tenant in a spec field. The operator checks that the named tenant
exists and is active, then renders the same tenant-keyed ConfigMap.

**Pros:**
- Every source lives in one namespace, so listing, reviewing and backing up the
  whole platform's sources is a single query.
- Fits an organisation where the platform team wants to approve every repository
  centrally for security review.
- Moving a source between tenants is a one-field edit.
- A source naming a deleted or suspended tenant can be reported as rejected with
  that as the reason, rather than vanishing.

**Cons:**
- The tenant is a plain string, and Kubernetes RBAC cannot restrict *which
  value* a writer may put in a field. Anyone who can apply the CR can name any
  tenant, so REQ-3 is not met by the resource itself.
- Closing that gap needs an admission webhook or policy engine that Orbit does
  not have today.
- Tenant admins have no write access to `orbit-system`, and granting it is a
  larger change than this feature warrants.
- Deleting or suspending a tenant leaves its CRs behind for the operator to mark
  orphaned.
- No existing Orbit CRD scopes tenants this way, so it would be the odd one out.

### Option 4: Labelled ConfigMaps per tenant namespace, aggregated by the operator

**Description:** Each tenant declares its sources in a plain ConfigMap in its
own namespace, marked with an agreed label and holding a sources array. The
operator watches labelled ConfigMaps cluster-wide, resolves the tenant from the
namespace, validates each entry, and merges them into the `orbit-system`
ConfigMap.

**Pros:**
- No new CRD and no CRD upgrade path to manage — it uses a built-in kind, so
  there is nothing to install before a tenant can declare anything.
- Tenant isolation comes from the namespace, the same boundary as Option 1.
- Deleting the namespace removes the sources.
- The operator still validates every entry, so the registration-state key and
  the slug-collision check are still possible in the same aggregation pass.

**Cons:**
- A ConfigMap has no schema and no status subresource. Validation results can
  only be surfaced as Events or annotations, so a rejected source is not visible
  in `kubectl get` — which is most of what REQ-7 asks for.
- ConfigMap write access in a namespace is typically broad and held by ordinary
  workloads, making it a wider permission than a dedicated CRD needs for what is
  effectively a security approval.
- The operator would have to watch ConfigMaps across all namespaces, widening
  its watch and cache well beyond the specific CRDs it watches today.
- It introduces a second, typeless registration mechanism alongside Orbit's
  typed ones.

---

## Decision
**Chosen Option:** Option 1 — Namespaced `SkillMarketplaceSource` CRD,
aggregated by the operator.

### Rationale

**Against Option 3.** The tenant must never be a value a tenant admin types. It
should come from which namespace the CR was allowed into, the boundary every
other tenant-owned CR in Orbit already relies on. Option 3's central listing and
one-field tenant moves are real advantages that Option 1 gives up, but they are
bought with a self-declared tenant string that Kubernetes RBAC cannot constrain
at field level. Closing that requires an admission webhook Orbit does not have,
and introducing one is a larger decision than this feature should make on its
own.

**Against Option 4.** The "no new CRD" saving is genuine, and the isolation
boundary is identical to Option 1's. But a ConfigMap has no status subresource,
and REQ-7 is specifically about a rejected source being visible with its reason;
Events are not that. It would also widen the operator's watch to ConfigMaps in
every namespace and make a security approval writable by anything holding
ordinary ConfigMap permissions.

**Against Option 2 — the close call.** On requirements the two are
near-equivalent, and Option 2's Kubernetes-enforced uniqueness is the more
elegant mechanism for REQ-10; rejecting slug-changing renames is a real answer
to the instability of skill IDs under a rename. It is not chosen for two
reasons. First, it makes the shared slug algorithm load-bearing for applying a
CR at all, rather than only for detecting a collision: until the backend commits
to an identical algorithm, Option 2 couples a hard, admin-facing failure to an
agreement that does not yet exist, whereas Option 1 degrades to a best-effort
check. Second, it carries `MCPServer`'s global scope into a V1 that is scoped to
"my tenant" — flag-gated off, but present, with a per-tenant crawl multiplier
behind that flag that is a live rate-limit risk. Option 1 pays for this with a
slug check written by hand and admin ergonomics it does not get for free; that
is the trade accepted here.

**For Option 1.** It reuses the most of what Orbit already has: `OAuthClient` is
the same shape of problem, with a status-bearing twist borrowed from
`MCPServer` (Pending/Aggregated/Invalid phases, an `Aggregated` condition). This
follows an established pattern rather than inventing a fourth aggregation
mechanism, which is exactly what the Problem Statement asked to avoid. Its one
deliberate departure — grouping by tenant instead of emitting a flat array with
`tenant_id` — exists precisely to satisfy the first decision driver, and is one
new render function rather than a new mechanism.

---

## Consequences

### Positive

- A tenant admin registers a source with one `kubectl apply` in their own
  namespace, using RBAC the customer already manages for every other
  tenant-owned CR.
- Every source has its own status and condition, so a rejected source — bad
  spec, a slug collision, or a CR applied where no active tenant owns the
  namespace — is visible with a reason rather than silently dropped.
- Deleting or suspending a tenant automatically drops its sources from the
  registry through the existing re-sync hook, with no separate cleanup step.
- The slug-collision check and the registration-state key both fall out of the
  same aggregation pass, because the aggregator — not a per-CR controller — is
  the one place that sees every source in a tenant at once.

### Negative

- A new CRD, controller and aggregator to maintain alongside the four that
  already exist.
- Seeing "every source on the platform" needs a cluster-wide list across
  namespaces; there is no single-namespace view.
- Moving a source to a different tenant means deleting the CR and reapplying it
  in the other tenant's namespace.
- The registration-state key is a second file contract with `skills-service`
  that this design writes but that `skills-service` does not yet read, so REQ-9
  is not closed on delivery.
- The shape of that key is undefined, and reasonable designs differ on it: at
  minimum it needs the source's display name, its state, and a reason when
  rejected. It should also carry the `namespace/name` of the CR itself —
  without that, the Sources view can name a rejected source but not the resource
  an admin has to go and correct.
- Two findings about the surrounding contract, neither caused by this choice but
  both affecting whether the feature can be observed working:
  - The wire shape described in prose and the shape
    `backend/internal/skills/registry.go` actually parses are different. The
    prose describes an object keyed directly by tenant, `{"<tenant>": [...]}`;
    the code expects one level deeper,
    `{"tenants": {"<tenant>": {"sources": [...]}}}`. These are not cosmetically
    different: `encoding/json` does not error on the wrong top-level shape, it
    simply populates nothing, so rendering the prose shape would produce a file
    that parses successfully and yields **zero sources for every tenant**, with
    no error anywhere in the stack. The aggregator must render the shape the
    code parses, and the prose should be corrected to match.
  - There is no Helm chart Deployment for `skills-service` yet — no Deployment,
    no ConfigMap volume mount, no Service. `backend/cmd/skills-service/` exists
    and compiles, but nothing in the cluster mounts the ConfigMap this feature
    renders. Out of scope here, but the feature cannot be observed end-to-end
    until that exists.

### Neutral

- Global (platform-wide) marketplace sources stay out of scope for V1. Option 1
  does not preclude adding them later; doing so would need a second scope and a
  fresh decision about whether a global source is expanded per-tenant or exposed
  as-is.
- The shared slug algorithm is added as `internal/platform/slug` and is used
  only by the operator. Until `skills-service` imports it, or the same rule is
  written down and agreed, REQ-10's guarantee holds only within the operator's
  own collision check. The rule is: lowercase, collapse each run of
  non-`[a-z0-9]` characters to a single hyphen, trim leading and trailing
  hyphens.

---

## Implementation

Lands in `orbit/backend/internal/operator/` and `orbit/charts/`. Each file
carries its own doc comment with the full rationale.

**Source resource.** `skillmarketplacesource_types.go` —
`SkillMarketplaceSource`, namespaced, `+kubebuilder:subresource:status`, short
name `sms`. `Spec`: `displayName`, `org` and `repo` required; `branch` optional,
defaulting to `main`. No tenant field, no credential field. `Status`: `phase`
(`Pending`/`Aggregated`/`Invalid`), `lastReconciledAt`, and an `Aggregated`
condition, mirroring `MCPServerStatus`. The CRD manifest is
`charts/orbit-crds/templates/crd-skillmarketplacesource.yaml`, mirroring
`crd-mcpserver.yaml`'s structure (status subresource, printer columns,
`helm.sh/resource-policy: keep`).

**Validation.** Per-CR checks — non-empty `displayName`/`org`/`repo`, a
`displayName` that does not slugify to empty, and `org`/`repo` that look like
plausible GitHub logins — live in the aggregator's spec validator. The
slug-collision check deliberately does not, because it needs to see every CR in
a tenant at once; it lives in the outcome builder, which claims slugs
tenant-by-tenant in CR-name-sorted order so the winner is stable across
reconciles, and rejects a later CR whose slug is already claimed. A CR applied
in `orbit-system`, or in a namespace with no active owning `OrbitTenant`, is
rejected with its own distinct reason rather than silently dropped.

**Aggregation.** `SkillMarketplaceSourceAggregator.Sync` mirrors
`activeTenantNamespaces` + collect + render from `oauthclient_aggregator.go` and
`mcpserver_aggregator.go`. It lists every active tenant, lists every
`SkillMarketplaceSource` cluster-wide, resolves each one's outcome, and then does
two things from that single pass: renders the ConfigMap with two data keys —
`marketplace-sources.json` (the fixed backend contract) and the
registration-state key — and patches every CR's status to match. The write is
skipped when both keys are unchanged, and uses an optimistic-lock merge patch,
because two callers can invoke `Sync`: this CR's own reconciler, and
`TenantReconciler` on tenant active/suspend/delete.

**Status.** `SkillMarketplaceSourceReconciler` is intentionally thin: it does not
validate or set status itself, only triggering `Sync` on create/update/delete.
The aggregator, having already computed every outcome to render the ConfigMap,
is the one place that can set every affected CR's status — including a CR that
has just lost a slug collision to a new sibling — in the same pass. This is the
one place the design departs from `MCPServerReconciler`, whose per-CR validation
cannot see sibling CRs and so has no equivalent of REQ-10 to enforce. Both the
CR's own reconciler and `TenantReconciler`'s aggregator wiring call the same
`Sync`, exactly as `MCPAggregator` and `OAuthAggregator` already do.

**Permissions.** In `charts/orbit/templates/operator/clusterrole.yaml`,
`orbit-operator` gains `get/list/watch` on `skillmarketplacesources` and
`get/patch` on `skillmarketplacesources/status`, alongside the existing
`mcpservers` rules. No new RBAC is granted to tenant admins: whether a tenant
admin can apply a `SkillMarketplaceSource` in their own namespace is entirely
the customer's own cluster RBAC, exactly as for `Connection`, `MCPServer` and
`OAuthClient` today.

### Migration Path

None needed: this is a new CRD with no prior data. A cluster upgrading to a
chart version carrying it gains the CRD and the initially empty
`orbit-skill-marketplace-sources` ConfigMap on the operator's next reconcile. No
existing resource changes shape.

### Timeline

The CRD, controller, aggregator, chart template and RBAC ship together as one
change. Two follow-ups are sequenced after it and are not gated on it: agreeing
the registration-state key with the backend and wiring `skills-service` to read
it, and adding a `skills-service` chart Deployment that mounts the ConfigMap.

---

## Validation

### Success Metrics

- A `SkillMarketplaceSource` applied in a tenant namespace appears, without a
  redeploy, in that tenant's `marketplace-sources.json` array within one kubelet
  ConfigMap-sync period (REQ-8).
- A structurally invalid CR, or one whose slug collides with a sibling, shows
  `status.phase: Invalid` with a human-readable reason in `kubectl describe sms`
  (REQ-7), and its registration-state entry reports it as rejected with the same
  reason once `skills-service` reads that key.
- Deleting or suspending an `OrbitTenant` removes its sources from
  `marketplace-sources.json` on the next `TenantReconciler` pass, with no action
  taken against the `SkillMarketplaceSource` CRs themselves.
- One tenant's invalid source never removes or blocks another tenant's valid
  sources in the rendered ConfigMap (REQ-7).

### Monitoring

- The operator logs a `Warn` for every excluded source, with its name, namespace
  and reason — the same level and shape as the other three aggregators' skip
  logging, so it is picked up by whatever already scrapes operator logs.
- The aggregated ConfigMap's two keys are the observable output; an unexpected
  empty array for a tenant that has CRs applied is the signal that tenant
  resolution or the wire shape has drifted.

---

## Notes

Two items remain open and are tracked outside this decision:

- The registration-state key's exact name and entry shape need the backend's
  agreement before REQ-9 can be considered closed.
- The slug algorithm needs to be adopted by `skills-service`, or written down
  and confirmed identically on both sides, before REQ-10's uniqueness guarantee
  extends beyond the operator.

**Last Updated:** 2026-09-29

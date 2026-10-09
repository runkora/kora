# Connection Scope Qualifiers — Grant-Time Resource Scoping for Connectors

<!-- Filename: 2026-07-22-connection-scope-qualifiers.md -->

**Status:** Accepted
**Date:** 2026-07-22
**Deciders:** Platform Engineering
**Technical Story:** Extracted from the [GitHub category-scoped actions ADR](2026-07-22-github-connection-category-scoped-actions.md) as a formal, connector-generic pattern: scoping an agent's connector grant down to specific resources (repositories, namespaces, …) at attachment time, without publishing a resource enumeration into the scope vocabulary.

---

## Context

### Background

Connection scopes today have exactly two dimensions: the connection and the action (`connection:<name>:<action>`). Any narrower resource reach lives in **connection config** — the GitHub adapter's `repositories` list, and nothing at all for Kubernetes (namespace authorization is delegated entirely to cluster RBAC on the injected identity). That knob is per-connection, so every agent attached to a connection has identical resource reach; narrowing one agent means editing shared state that affects all of them, or registering duplicate connections against the same upstream.

The GitHub connector work surfaced the fix — qualify the *grant*, not the connection — and the same shape immediately applies to Kubernetes ("this agent may touch the `staging` namespace only") and predictably to future connectors (an AWS connector will want scoping by account, region, or tag, exact axis to be determined). Rather than three ad-hoc implementations, this ADR defines the pattern once.

Two platform invariants constrain the design:

- **Hierarchy is `covers`, never `*`.** Scopes parse as `<resource_type>:<path…>:<action>`; breadth comes from the `scope_covers` prefix rule (same resource type and action, coverer's path is a prefix — `authz.rego`/`authz_gateway.rego`, mirrored in `policyservice/api/service.go`). There is no wildcard matching anywhere in the policy layer.
- **The vocabulary is static.** Scope vocabulary is registered by services from adapter metadata (`internal/platform/connections`). Resources like repositories and namespaces are unbounded, tenant-private, and change upstream without any kora-side event — they can never be enumerated into the vocabulary.

**Which of the three questions this touches:** *What can it access?* — grants gain a resource dimension. Identity flow and connection mechanics are unchanged.

### Problem Statement

An admin attaching a connector to an agent can choose *which actions* but not *on which resources*. There is no way to express "issues on these two repositories" or "workloads in the `staging` namespace" as a grant, and no way to express "all repositories" except by the absence of any constraint at all.

### Goals

- One scope grammar, one validation rule, one enforcement path in the broker — instanced per adapter by metadata and a qualifier extractor, not by new policy machinery per connector.
- "All resources" expressible as a single scope via `covers`, with no wildcard and no enumeration — and it must keep covering resources created after the grant was written.
- An attachment-time selection surface (multiselect fed by a live, brokered listing of the upstream's resources) and a config-time ceiling (a connection-level allowlist a tenant admin can set on the CR).
- Fail closed everywhere: unknown qualifier shapes rejected at grant time, out-of-ceiling requests denied at the broker, qualified grants never satisfying broader requirements.

### Non-Goals

- More than one qualifier segment per scope (e.g. AWS `account` + `region` simultaneously). The grammar reserves exactly one segment in v1; a second axis is a future amendment with its own ordering argument.
- Choosing the AWS qualifier axis — named here only as the motivating future instance.
- Qualifier-aware **tool** scoping in the MCP gateway — this ADR is about connection (egress credential) scopes.
- A new Hades multiselect atom. The attachment UI (defined in Surfaces below) composes existing Hades primitives at Tier 2; promoting a generic multiselect into Hades is a separate proposal through the ui-component process if the composition earns it.

---

## Decision Drivers

- **Delegation model.** The connection is the installation/cluster; the grant is one agent's slice of it. Resource reach is a property of the delegation, so it belongs on the grant.
- **`covers` is the only hierarchy.** Whatever encodes the resource must let prefix coverage express breadth — which dictates *where* in the path the qualifier can sit.
- **No dynamic vocabulary.** Registration stays static adapter metadata; anything unbounded must be grant data validated structurally, the precedent being `egress:<host>:connect`.
- **Adapters differ in enforcement depth.** GitHub can down-scope the minted token to the resource; Kubernetes (static bearer) cannot. The pattern must define which layers are invariant and which are per-adapter.

---

## Considered Options

### Decision A — Where the resource constraint lives

#### Option 1: An optional qualifier segment in the scope path (Chosen)

**Description:** `connection:<name>:<category>[:<qualifier>]:<read|write>` — path segments `[name, category, qualifier?]`. The qualifier is free-form grant data (per-adapter value space), validated structurally at grant time, never registered in the vocabulary. Omitting it *is* the all-resources grant, mechanically, because the shorter path covers every qualified form.

**Pros:**
- Zero new policy machinery: `scope_covers` already evaluates it, the audit log already records it (the required scope string carries the qualifier), effective-scope projection already expands it.
- One grammar serves every adapter; the adapter contributes only its value space and extraction logic.
- The all-resources grant is future-proof by construction — a new repository or namespace is covered the moment it exists.

**Cons:**
- Scope strings get longer and their segment positions become meaningful; the structural validator must be strict so qualifier and category segments cannot be confused.

#### Option 2: Structured grant conditions (a qualifier object on the grant, like the budgets JSONB)

**Description:** Keep scopes two-dimensional; attach `{"repositories": [...]}` / `{"namespaces": [...]}` metadata to the grant, evaluated as a condition.

**Why not chosen:** Conditions are a second authorization language. The rego would need new input plumbing and evaluation rules per condition type; `covers`, effective-scope projection, delegation checks, and the audit trail all operate on scope strings and would each need a parallel condition path. The scope path already *is* the platform's resource-hierarchy encoding — adding a second one forks the model the first time, and every adapter after that.

#### Option 3: Enumerate resources into the published vocabulary

**Why not chosen:** The vocabulary is static, gateway-published metadata; repositories and namespaces are unbounded and change without kora-side events. It would also still need the covering-parent form to express "all current and future resources" — at which point per-resource entries add registration churn and picker noise for nothing structural validation doesn't already give.

### Decision B — Segment ordering

#### Option 1: Category before qualifier — the unbounded dimension goes deepest (Chosen)

**Description:** `covers` expresses breadth only by omitting a suffix, so the dimension that sits deeper is the one that never needs enumerating. Categories are closed, small, adapter-owned sets; resources are open and unbounded. Therefore `[name, category, qualifier]`:

- `connection:gh:write` — everything (umbrella);
- `connection:gh:issues:write` — one category, **all** resources, one scope;
- `connection:gh:issues:webapp:write` — one category, one resource.

"All categories on one resource" costs one scope per category — bounded by the category count, compiled by the attachment flow, never typed by a human.

#### Option 2: Qualifier before category

**Why not chosen:** Makes "all actions on resource X" one scope but "category Y on all resources" *inexpressible* without enumerating every resource — inverting which dimension must be enumerated, which is exactly the unbounded one. Rejected on the ordering principle above.

### Decision C — The config-time ceiling

#### Option 1: Optional connection-level allowlist, enforced at the broker and reflected in the picker (Chosen)

**Description:** A connection's config may declare a qualifier allowlist (e.g. GitHub `repositories`, Kubernetes `namespaces`) — set on the GitOps CR by the tenant admin, shown read-only in the connections UI. Enforcement is adapter-agnostic in the broker: after classification, a request whose extracted qualifier falls outside a declared allowlist is denied *before* policy evaluation. The attachment picker offers only the intersection of the live upstream listing and the ceiling.

**Pros:**
- Gives every adapter real ceiling enforcement at the gate, including adapters with no mint-time down-scoping (Kubernetes).
- Preserves the two-tier authority split: tenant admin bounds the connection on the CR; agent admin selects within it at attachment.

**Cons:**
- Two places can deny a resource (ceiling vs. grant), which observability must keep distinguishable — the broker logs a ceiling deny as its own class, alongside the existing policy-deny and classification-deny classes.

#### Option 2: Ceiling enforced only at mint time

**Why not chosen:** Only works for adapters whose upstream supports scoped minting (GitHub). Kubernetes' static bearer would make the ceiling advisory — a grant mistake would sail through the gate and be caught only by cluster RBAC, which the tenant admin may not control.

### Decision D — Where per-adapter behavior lives

#### Option 1: Extend the existing adapter seams — metadata table plus `ClassifyAction` (Chosen)

**Description:** Three per-adapter contributions, each on an existing seam:

1. **Declaration** — `adapterMeta` in `internal/platform/connections` gains optional qualifier metadata: the dimension's name ("repository", "namespace"), description for the picker, and the structural shape of a valid value. Also declared there: **baseline actions** — unqualified scopes every attachment must include for the connector to function (GitHub `metadata:read`; Kubernetes `cluster:read` for API discovery, per the k8s ADR's Amendment 1 gotcha).
2. **Extraction** — `ClassifyAction` returns the qualifier alongside the action (GitHub: the repo from git/REST paths; Kubernetes: the namespace from `/…/namespaces/{ns}/…` paths; empty when the request names no resource). Adapters without a qualifier dimension return it empty always.
3. **Enumeration** — an optional adapter capability listing the upstream's current qualifier values through the connection's own credential (GitHub: the installation's repositories; Kubernetes: the cluster's namespaces), powering the attachment picker via a brokered, admin-initiated endpoint.

Mint-time application stays adapter-private: GitHub narrows `InstallationTokenRequest.Repositories` to the request's qualifier; Kubernetes has nothing to narrow (static bearer) and relies on the gate plus cluster RBAC.

**Why chosen over a new interface:** every piece lands on a seam that already exists and already proved it generalizes (the metadata table, `ClassifyAction`, the broker's adapter registry). A parallel "qualifier provider" interface would be a second registration path for the same adapters.

---

## Decision

**Chosen:** Qualifier path segment (A-1), unbounded-dimension-deepest ordering (B-1), broker-enforced config ceiling (C-1), adapter seams for declaration/extraction/enumeration (D-1).

### The invariant enforcement path (all adapters)

1. **Grant time** — policy service validates a qualified connection scope structurally: known scope shape plus one extra path segment (lowercase, no `:` or `/`), mirroring how `egress:<host>:connect` is validated without vocabulary enumeration. Qualified scopes never enter the vocabulary.
2. **Attachment time** — the connector attachment compiles the admin's resource selection into qualified grant scopes ("all resources" → the unqualified category scopes), always adding the adapter's declared baseline actions unqualified. `requires` edges live on vocabulary entries only, so baselines must come from attachment compilation, not `requires` (a qualified grant neither covers nor is covered by the vocabulary leaf).
3. **Request time** — the adapter classifies `(action, qualifier)`; the broker denies out-of-ceiling qualifiers, then evaluates policy with `PathSegments: [name, category, qualifier]`. Unqualified and parent grants cover by prefix; qualified grants must match. A request naming no resource produces an unqualified requirement, which a strictly qualified grant deliberately does **not** satisfy — fail closed. Extraction must be **canonical**: the extracted value must identify the same resource the upstream will serve under this connection's authority. A request naming a resource outside that authority (e.g. a foreign repository owner through a GitHub connection) is denied at classification, never qualified ambiguously — otherwise name collisions across authorities would let a qualified grant match resources it never meant (GitHub's public repos being the concrete case).
4. **Mint time** (per-adapter, where the upstream supports it) — the credential is narrowed to the request's qualifier, so the token bounds the resource even outside kora's gate.

### Instances

| Adapter | Qualifier | Extraction | Enumeration for picker | Mint-time narrowing | Backstop |
|---|---|---|---|---|---|
| `github_app` | repository (bare name; the installation fixes the owner) | git smart-HTTP and `/repos/{owner}/{repo}/…` paths | list installation repositories | yes — `InstallationTokenRequest.Repositories` | GitHub rejects the token elsewhere |
| `kubernetes` | namespace | `/api/v1/namespaces/{ns}/…`, `/apis/{g}/{v}/namespaces/{ns}/…` | list cluster namespaces | no (static bearer) | cluster RBAC on the injected identity |
| future (e.g. AWS) | TBD (account / region / tag) | adapter-defined | adapter-defined | adapter-defined | upstream IAM |

Kubernetes cluster-scoped resources (nodes, namespaces themselves, discovery) classify unqualified — the same rule as GitHub's `/graphql` and `/search/*`: resources the request cannot attribute to a qualifier require the unqualified grant. The GitHub instance's specifics (category table, permission down-scoping, repo-less endpoints, rename staleness) live in the [GitHub ADR](2026-07-22-github-connection-category-scoped-actions.md); the Kubernetes instance is an amendment to the [Kubernetes connection ADR](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md) when implemented.

### Surfaces

Defined here, not deferred, so the mechanism ships with its UI. All attachment surfaces already share one component — `ConnectionCards` (`src/components/connection-cards.tsx`, Tier 2), the motif's collapsible tri-state connection cards rendered by the agent builder's Tools step, the agent detail Configuration tab, and the environments form. One extension to that component covers every attachment surface; no new page and no new Hades component.

**Attachment time — the `ConnectionCards` extension:**

- For a connection whose adapter declares a qualifier dimension, the expanded card gains a **resource scope block** between the card header and the action rows, visible once the connection is selected. It follows the motif's radio pattern: two options, labeled from the adapter's qualifier metadata — **"All repositories"** (default; likewise "All namespaces" for `kubernetes`) and **"Selected repositories"**. The component reads the dimension's name from adapter metadata and stays adapter-generic.
- Choosing "Selected" reveals a multiselect **composed from existing Hades primitives** (`Popover` + `Input` filter + `Checkbox` rows + `Badge` chips) — Hades has no multiselect atom, and this composition stays Tier 2; if it proves generically reusable, promotion goes through the ui-component approval process separately, not assumed here.
- The multiselect is fed **lazily** (on first open) by the brokered enumeration endpoint, offering the intersection of the live upstream listing and the connection's ceiling. Manual entry is permitted (GitOps-managed grants may reference resources the enumeration call cannot see; structural validation still applies), with manually entered values not present in the enumeration visually flagged.
- **Scope previews tell the truth**, per the motif's existing rule (each action row already shows its compiled `connection:<name>:<action>` string in mono): under "All", rows keep the unqualified form; under a selection, rows show the qualified form (`connection:github-main:issues:orbit:read`), expanding one preview line per selected resource or a count when the selection is large. The card's summary line gains the qualifier state ("2 repositories").
- **Baseline actions** (`metadata:read`, `cluster:read`) render as an implied, non-uncheckable row marked "added automatically", so the compiled grant never contains a scope the admin didn't see.
- A selected value **absent from the current enumeration** (renamed or deleted upstream — the stranded-grant case) renders with a warning indicator rather than silently, on every surface that shows the attachment.
- Data model: the `Connector` shape (`{connection, actions}`) gains optional `resources?: string[]` — absent means all. `EffectiveAccessPanel` and the `previewEnvironmentAccess` path expand qualified scopes so the access preview shows exactly what attachment compilation will grant, baselines included.
- Storybook stories accompany the extension per Tier 2 conventions (qualifier-capable and qualifier-less adapters, all/selected states, stale-selection warning).

**Config time — connections surface:** the ceiling is a CR field (GitOps-managed, like all connection config), surfaced **read-only** on the connections page (qualifier allowlist as badges on the connection detail) and reflected in what the attachment multiselect offers. No editing UI — connections are declarative.

---

## Consequences

### Positive

- One pattern, three enforcement layers, no new policy machinery: every future connector gets grant-time resource scoping by declaring metadata and returning a qualifier from classification — registration, not code, extended to the resource dimension.
- "All resources" is a real, auditable scope that stays correct as the upstream grows — the covers-parent property, with no wildcard semantics introduced.
- The delegation story sharpens: connection = what the platform can reach (tenant admin, CR); grant = what this agent may touch (agent admin, attachment).

### Negative

- Qualifier values are point-in-time strings: renamed or deleted upstream resources strand their qualified grants silently (fail closed). The attachment UI should flag selections that no longer resolve; until then this is a support surface.
- Requests that cannot be attributed to a resource (GraphQL, search, cluster-scoped calls) are invisible to qualification — strictly qualified agents lose them, and admins must understand why (the baseline-actions mechanism covers the mandatory cases; the rest is deliberate).
- The broker gains a third deny class (ceiling), and adapters gain an interface change (`ClassifyAction` returning a qualifier) touching all existing adapters, including `passthrough`.

### Neutral

- Enforcement depth varies by adapter and is now explicit in the instance table rather than implicit in each adapter's code — GitHub's token narrows, Kubernetes' does not.
- One qualifier segment in v1; a future two-axis adapter (AWS account + region) forces the ordering argument again as an amendment here, not a per-connector improvisation.

---

## Validation

### Success Metrics

- The GitHub instance ships against this pattern with no GitHub-specific code in the policy service or broker beyond the generic qualifier path (measured: the diff).
- The Kubernetes namespace instance lands as adapter metadata + extraction only — no further policy-service or broker change (the proof the abstraction holds).
- A qualified grant denies a sibling resource at the gate; an out-of-ceiling request is denied with the distinct ceiling-deny log class; an unqualified category grant reaches a resource created after the grant.
- The attachment UI's scope previews match the compiled grant exactly — every scope string shown on a `ConnectionCards` row (including the implied baseline row) appears in the effective grant, and nothing else does. Verified per surface (builder, configuration tab, environments form) since all three render the same component.

### Monitoring

Broker structured logs gain the ceiling-deny class alongside the existing policy-deny, classification-deny, and authorized classes. Audit dashboards can group by qualifier since it is part of the evaluated scope string.

---

## Related Decisions

- [github-connection-category-scoped-actions](2026-07-22-github-connection-category-scoped-actions.md) — first instance; defines the GitHub category taxonomy and mint-time permission down-scoping this pattern's qualifier rides on.
- [kubernetes-connection-bearer-adapter-and-gateway-ca-trust](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md) — second instance (namespaces), to be amended there when implemented; its Amendment 1 discovery gotcha motivates the baseline-actions mechanism.
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the broker and adapter seams this pattern extends.
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — the `covers` hierarchy this pattern leans on.

## References

- `internal/platform/connections/adapters.go` — the metadata table gaining qualifier + baseline declarations
- `internal/egressgw/service.go` — the broker gaining the ceiling check and qualified `CheckInput`
- `internal/policyservice/api/service.go` (`scopeCovers`) / `authz_gateway.rego` (`scope_covers`) — the unchanged coverage rule doing the authorization work
- `internal/policyservice/bundle/compiler.go` (`expandRequires`) — why baselines compile at attachment instead of via `requires`

---

## Notes

- The dead `connection:<name>:*` vocabulary entry (no wildcard rule exists in `scope_covers`; its `*` action segment matches nothing) is removed as part of the GitHub ADR's rollout; the covering parents `connection:<name>:read`/`write` are its proper replacement.
- Qualifier normalization is per-adapter (GitHub repository names are case-insensitive → lowercased; Kubernetes namespaces are already DNS labels). The structural validator enforces only the shared shape.

**Last Updated:** 2026-07-22

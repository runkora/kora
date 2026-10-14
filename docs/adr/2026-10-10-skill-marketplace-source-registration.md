# Skill Marketplace Source Registration via a Tenant-Scoped CRD

<!-- Filename: 2026-10-10-skill-marketplace-source-registration.md -->

**Status:** Proposed
**Date:** 2026-10-10
**Deciders:** Platform Engineering, Product Owner
**Technical Story:** Builds on [PR #351](https://github.com/clear-route/orbit/pull/351) (`skills-service`: binary skeleton, models, registry, GitHub client) and its [Agent Marketplace backend ADR](2026-08-28-marketplace-adr-backend.md). Product context: [Agent Marketplace PRD](../prd/agent-marketplace.md).


---

## Context

### Background

PR #351's `skills-service` reads marketplace sources from `marketplace-sources.json` in the `orbit-skill-marketplace-sources` ConfigMap. The backend ADR says the operator fills this ConfigMap from `SkillMarketplaceSource` CRs, but does not design the CRD.

**Gap:** nothing fills the ConfigMap today — the chart renders it as a static `{"tenants": {}}`, and no `SkillMarketplaceSource` CRD exists anywhere in the codebase (no Go types, no CRD YAML, no scheme registration, no RBAC rule).

A tenant is a customer organisation. Each tenant owns one namespace (`orbit-tenant-<name>`), where its admins apply CRs. Tenants must not see, affect or learn about each other.

### Problem Statement

Tenants need to register their own marketplaces, and the open question is whose credential reads a private repository. PR #351 crawls every source with one platform-wide GitHub App installation. Once tenants register sources themselves, that breaks in two ways:

- **Reach:** an installation belongs to one GitHub account, so a customer's private repos in its own organisation cannot be read.
- **Isolation:** the platform token is not limited to any tenant's repos. A tenant that registers a private repo the platform installation can read gets its skills served, whoever owns it.

This ADR accepts that the chosen design does not close the Reach gap — see Rationale.

### Goals

- A tenant admin registers a marketplace by applying one CR in the tenant's namespace, naming a repository a platform admin has already allow-listed for that tenant.
- The operator renders exactly the JSON shape `skills-service` already reads, with no new fields. The API's routes and response formats, the BFF and the frontend do not change.
- A tenant can only surface repositories a platform admin has explicitly approved for it — the platform's one credential can reach more than any single tenant should be allowed to expose, so admission must be deliberate.
- Misconfigurations, including name clashes, are reported on the tenant's own CR.

### Non-Goals

- Global (platform-published) sources. Deferred; see Decision.
- The PRD's read-only Sources view (FR-13). Deferred; see Decision.
- Managing sources from the dashboard (also out of scope in the PRD).
- Providers other than GitHub, and GitHub Enterprise Server.
- A per-tenant cap on the number of sources.
- Automated, GitHub-verified public-repo approval. The operator cannot call GitHub (Kubernetes-only), so it cannot tell a harmless public repo from a sensitive private one — every repo gets the same manual gate for V1. A GitHub-calling admission helper that auto-approves confirmed-public repos is a plausible follow-up, not part of this decision.
- Revocation faster than the 12-hour cache lifetime.

---

## Decision Drivers

* **Fixed contract:** the start is a tenant registering a marketplace; the end is the JSON that `skills-service` already reads, with no new fields.
* **Minimal new surface:** ship self-service registration without changing `skills-service`'s credential handling or introducing new secret material.
* **GitHub quota is one shared, finite resource:** every tenant's crawl draws on the same installation, so admission into that shared pool must be a deliberate, reviewable act, not an automatic one.
* **The operator stays Kubernetes-only** and makes no GitHub calls, as the [install and distribution ADR](2026-09-04-install-and-distribution-model.md) scopes it — which is exactly why it cannot distinguish public from private repos on its own.

---

## Considered Options

### Option A: Platform Credential with a Per-Tenant Allowlist

**Description:** Keep PR #351's single platform installation. Platform admins list on each `OrbitTenant` which `owner/repo` the tenant may register, and the operator drops everything else.

**Pros:**
- No `skills-service` change.
- Tenants share no secret and set up nothing new.

**Cons:**
- Reaches only Orbit's own account and public repos.
- Isolation depends on Orbit's list being right, and a mistake exposes everything the platform installation can read.
- Every repository needs admin approval, public ones included, because the operator cannot tell public from private without calling GitHub.

### Option B: Tenant's Own GitHub App via its Connection

**Description:** Each source references a `github_app` `Connection` in the same namespace. `skills-service` crawls that source only with that Connection's App installation, and holds no platform credential.

**Pros:**
- GitHub caps each credential at one tenant's installation, so a bug in Orbit's credential selection exposes at most one tenant's repos.
- Tenants reach their own private repos.
- Each tenant uses its own GitHub rate limit, so no tenant can starve another's crawl quota.

**Cons:**
- `skills-service` changes and holds every tenant's App key, duplicating key material into a second `orbit-system` Secret.
- A credential is needed even for public repos.
- Meaningfully more operator and service code: per-source credential resolution, Connection validation, a new Secret lifecycle.

### Option C: Orbit's App Installed in Each Customer Account

**Description:** Customers install Orbit's App on chosen repos, and platform admins bind each GitHub account to one tenant.

**Cons:** Isolation depends on Orbit's binding being right, and one Orbit key opens every customer's selected repos. `skills-service` still changes, to find the installation for each repo.

---

## Decision

**Chosen Option:** Option A - Platform Credential with a Per-Tenant Allowlist

### Rationale

A ships the entire feature — self-service CR-based registration, the CRD, the dynamic ConfigMap — without touching `skills-service`'s credential handling at all. `skills-service` already falls back to its single platform-wide `TokenProvider` whenever a source carries no per-source credential reference; under Option A, every source is exactly that case, so zero new code is needed on the crawl path. No new Secret is introduced, no key material is duplicated, and tenants are asked to set up nothing — the only new workflow is internal, on the platform-admin side.

This trades away two things deliberately, and the team is accepting both as known, scoped risks rather than solving them here:

- **Reach.** A tenant cannot register a private repository that lives in its own GitHub organisation — only a repo Orbit's own installation can already read. For V1, registered sources are expected to be a small, explicitly vetted set agreed during tenant onboarding, not an open self-service catalogue of arbitrary tenant-owned repos. The admin-approval step is, for this phase, a feature that enforces that expectation, not just friction to route around later.
- **Isolation.** The boundary between tenants is an allowlist Orbit's own code maintains and checks, not something GitHub enforces. A wrong entry — a typo, a stale repo reference, a copy-paste from the wrong tenant's ticket — has no external backstop; it exposes whatever the platform installation can read to the wrong tenant. This is the one risk this ADR does **not** reduce to zero. It is bounded only by process rigor (review on every allowlist change, audit logging on every crawl, periodic reconciliation between the allowlist and what's actually registered), not by architecture.

If the product's trajectory moves toward tenants routinely registering their own private repositories at scale, or the admin-approval queue becomes an onboarding bottleneck, that is the trigger to revisit this decision — Option B (or a hybrid with multiple platform-owned installations sharded across tenants) becomes the better trade at that point. Nothing in this CRD's shape — the `SkillMarketplaceSource` type, its status lifecycle, the ConfigMap contract — needs to change to get there later; only the credential-resolution step and the allowlist mechanism would be replaced.

### The CRD — gap: does not exist today

```yaml
apiVersion: orbit.io/v1alpha1
kind: SkillMarketplaceSource
metadata:
  name: acme-internal
  namespace: orbit-tenant-acme   # tenant "acme" comes from the namespace, never the spec
spec:
  displayName: Acme Internal Skills
  github:
    owner: acme-corp             # organisation or user account
    repo: internal-skills
    branch: main                 # optional, defaults to main
```

No `connectionRef` field — there is nothing to reference. The CRD is namespaced, in `orbit.io/v1alpha1`, with a status subresource. The operator renders each source as:

| CRD | `marketplace-sources.json` |
|---|---|
| the namespace's `OrbitTenant` name | key under `tenants` |
| `displayName` | `name` |
| `github.owner` | `org` |
| `github.repo` | `repo` |
| `github.branch` | `branch` |

Field shapes are validated by the CRD schema, so a malformed source is rejected at `kubectl apply` rather than reported later in status:
- patterns for `github.owner` and `github.repo`, and length limits on every field;
- CEL rules for what a pattern cannot express: a valid git branch name, a repo name other than `.` or `..`, and a `displayName` containing at least one letter or digit, so that its URL segment is never empty.

Whether `owner/repo` is actually permitted for this tenant depends on something outside the CR's own spec — the allowlist, below — so the operator marks a source `Invalid`/`Pending` for that reason independently of schema validity.

### Allowlist — gap: new field on `OrbitTenant`, new operator logic

`OrbitTenant.spec` gains a new field, visible only to platform admins (tenant admins cannot edit their own `OrbitTenant`):

```yaml
apiVersion: orbit.io/v1alpha1
kind: OrbitTenant
metadata:
  name: acme
spec:
  active: true
  allowedMarketplaceRepos:
    - acme-corp/internal-skills
    - acme-corp/public-docs
```

A `SkillMarketplaceSource`'s `owner/repo` must appear in its tenant's `allowedMarketplaceRepos` before the aggregator will render it into the ConfigMap. This list is validated the same way (owner/repo pattern, length limits) but carries no CEL rule beyond that — admission policy, not shape, is the point of this field.

### Flow — gap: no registration path exists today; this is net-new end to end

**Registering a source**

1. A tenant admin applies a `SkillMarketplaceSource` CR in `orbit-tenant-<name>`, naming a repo.
2. The operator reconciles it immediately. It checks the namespace's `OrbitTenant.spec.allowedMarketplaceRepos` for this `owner/repo` — if absent (the common first-time case), the source is marked `Pending`/`NotAllowlisted`. This step is instant and automatic; no human is involved yet.
3. **Nothing in the system notifies anyone at this point.** The tenant admin sees the rejection via `kubectl describe`, and has to escalate out-of-band (a ticket, Slack, email) to a platform admin — this hand-off is not automated in this version.
4. A platform admin edits `OrbitTenant.spec.allowedMarketplaceRepos` to add the entry. This is a deliberate, manual, human action, gated entirely by that person's availability and queue — this is the real latency in the whole flow.
5. That edit itself triggers the tenant's reconcile loop (the same trigger already used for suspend/delete/periodic requeue), which re-runs the marketplace aggregator for every source in that namespace.
6. The previously-rejected source now matches the allowlist. Its status flips to `Aggregated`, and it's written into `orbit-skill-marketplace-sources`.
7. `skills-service` picks up the ConfigMap via its existing `fsnotify` watch — no restart required.

**Serving a request (unchanged from PR #351 today)**

8. FE → BFF → `skills-service`, bearer token forwarded.
9. `skills-service`'s auth chain resolves `tenantID` and checks the `marketplace:read` scope — **currently a no-op in every environment** (see Implementation; this fix is identical regardless of which option is chosen).
10. `marketplaceName` is resolved via the registry to `{org, repo, branch}`. On a cache hit, the cached skill list is served from Redis.
11. On a cache miss, `skills-service` crawls GitHub using its existing single platform-wide `TokenProvider` — **no per-tenant credential lookup exists or is needed**, since every source's credential reference is empty and the existing fallback handles it. This is the same code path PR #351 already ships.
12. Results are cached in Redis with the existing 12h TTL and served, capped at 200 skills.

### Credential reuse — no new Secret, no new `skills-service` code

`skills-service` keeps using exactly the `GITHUB_APP_ID` / `GITHUB_APP_INSTALLATION_ID` / private-key configuration PR #351 already ships. No `orbit-skill-marketplace-credentials` Secret is introduced, no key material is duplicated into a second `orbit-system` Secret, and no per-source credential resolution code is added — the existing `TokenProviderPool`'s fallback to the default platform `TokenProvider` already covers this case for free, because every source under this design simply has no credential reference to resolve.

### Status — gap: new CRD, status now depends on a sibling resource's field

Each source has a phase (`Pending`, `Aggregated` or `Invalid`) and an `Aggregated` condition whose reason explains it:

| Reason | Phase | When |
|---|---|---|
| `Aggregated` | Aggregated | `owner/repo` is on the tenant's allowlist; rendered for its tenant |
| `TenantNotActive` | Pending | The namespace has no active `OrbitTenant` |
| `NotAllowlisted` | Pending | `owner/repo` is not (yet) in this tenant's `allowedMarketplaceRepos` |
| `GlobalSourcesNotSupported` | Invalid | Applied in `orbit-system` |
| `DuplicateName`, `DuplicateRepository` | Invalid | An older source in this namespace already holds the name or the repo |

`Aggregated` means the source was handed to `skills-service`, not that GitHub can actually serve the repo at crawl time — the platform installation's own GitHub-side grants are still the final word, and a 503 at request time is possible even for an `Aggregated` source if GitHub denies the platform installation access.

**Keeping statuses current.** A source's verdict depends on its sibling sources (name/repo clashes) and on its tenant's `allowedMarketplaceRepos` field, not only on its own spec — so status cannot be set only when a CR's own generation changes. Every aggregation run computes a verdict for every source and patches the status of each source whose verdict changed. A run is triggered by: any `SkillMarketplaceSource` change; a change to its `OrbitTenant` (which is what an allowlist edit actually is); or the tenant reconcile (suspension, deletion, periodic requeue).

### Name clashes — gap: new logic, unaffected by the credential-model choice

Clashes are checked within one tenant only; two tenants may use the same name or the same repo.

- Two names clash when they produce the same URL segment, such as `Clear Route Skills` and `clear-route skills`. The operator uses the backend's own slug function.
- The oldest source keeps the name, ordered by `(creationTimestamp, metadata.name)`, and later ones are marked `Invalid`. Creation timestamps have one-second precision, so without the name as tiebreak, sources applied in one `kubectl apply` would win at random on each run and the marketplace would flicker.
- A second source for the same `owner/repo` is rejected, because the cache key has no branch.

### Global sources — deferred

A source applied in `orbit-system` is marked `Invalid`. Until global sources exist, Orbit's own catalogue reaches a tenant only if its repo is allow-listed for that tenant. Adding global sources later needs no change to this CRD or to `skills-service`.

### Sources view (PRD FR-13) — deferred

This ADR does not deliver it. Registration state and errors are on each CR, readable with `kubectl describe`. Allowlist state is on `OrbitTenant`, readable the same way. A UI for either needs a new read API and a frontend page, which belong in their own ADR.

---

## Consequences

### Positive

- Zero change to `skills-service`'s credential handling — the smallest possible diff to ship self-service registration.
- No new Secret, no duplicated key material, nothing new for `skills-service` to hold or rotate.
- Tenants need no new trust relationship or setup — no `Connection`, no App installation on their side. The only new workflow is internal to Orbit.
- The JSON file, the API, the BFF and the frontend are unchanged.
- Tenants can see why a source is pending or rejected with `kubectl describe`.

### Negative

- **Every repository needs a platform admin's manual approval, public ones included**, because the operator cannot call GitHub to tell them apart. This is a recurring cost, not a one-time setup step — it repeats for every repo a tenant ever wants to add, for the product's lifetime.
- **A tenant cannot register a private repository in its own GitHub organisation.** The stated motivating problem (tenants exposing their own private skills) is not solved by this decision.
- **Isolation is self-enforced, not GitHub-enforced.** A wrong or stale allowlist entry exposes whatever the platform installation can read to the wrong tenant, with no external backstop. This is the architecture's single largest known risk and is not reduced by better code elsewhere — only by review discipline and auditing.
- **All tenants share one GitHub App installation's rate limit and crawl capacity.** A large or heavy-crawling tenant can degrade or rate-limit the marketplace for every other tenant. There is no per-tenant quota isolation.
- **The platform-wide crawl ceiling is fixed and does not grow with tenant count.** Each tenant onboarded is a smaller effective share of the same number — the ceiling gets tighter as adoption succeeds, not looser.
- **No notification mechanism exists for a pending allowlist request.** The hand-off from a rejected CR to a platform admin's action happens entirely outside Orbit's own tooling (tickets, Slack, email) in this version.
- `OrbitTenant.spec.allowedMarketplaceRepos` has no cap and could grow unbounded as a tenant's approved list accumulates over time.
- The same repo cannot be registered twice in one tenant, for example on two branches.

### Neutral

- `kubectl get` shows registration and allowlist state; crawl health stays in `skills-service`.
- The frontend's copy of the slug function (`frontend/src/lib/marketplace.ts`) must keep matching the Go one — there is no shared source of truth yet, and this is unrelated to the credential-model choice.

---

## Implementation

The operator, `skills-service`, and chart and docs parts below ship together. Otherwise Helm overwrites the operator's output. Turning on authorization is the exception: it should land first, in PR #351 itself, and is entirely independent of the A vs. B decision.

- **Turning on authorization — gap: `skills-service` is unauthenticated in every environment today.** `main.go` passes a hardcoded, non-empty, non-env-gated `"tenant-a"` into `skills.NewController(...)`, which replaces the entire auth/delegation/scope-enforcement chain with a pass-through. This is a security fix, and removing it alone fails closed (every request returns 403) unless all of the following land in the same change:
  - set `policy.registerRoutes` to `true` for skills-service (`false` in `base.yaml` today, no environment overrides it);
  - add the identity-proxy sidecar to `skills-service`'s chart deployment, mirroring `inference-gateway`'s deployment template, since route registration authenticates through it;
  - confirm `marketplace:read` is registered in the live scope vocabulary once `registerRoutes` is on (the scope is already declared in code — this just needs to be live);
  - grant `marketplace:read` to a tenant-member role; `platform-admin` receives it automatically once the scope is registered;
  - verify the whole path, including the policy bundle fetch, with a `make auth` token.

- **Operator — gap: none of this exists.**
  - the CRD YAML, the Go types and their registration in `scheme.go`;
  - extend `OrbitTenant`'s CRD and Go type with `allowedMarketplaceRepos`, including its own field validation;
  - an aggregator that checks each source against its tenant's allowlist and sibling sources, writes the ConfigMap (no credentials Secret — there is none to write), and returns a verdict for every source;
  - a reconciler that patches the status of every source whose verdict changed, triggered as described under Status;
  - the aggregator added to the tenant reconcile (suspend, delete, the periodic run, and any change to `OrbitTenant.spec.allowedMarketplaceRepos`);
  - wiring in `cmd/operator/main.go`, and ClusterRole rules for the new resource and its status.

- **`skills-service` — gap: none. No code change is required here.**
  - every source's credential reference stays empty; the existing fallback to the default platform `TokenProvider` (already shipped in PR #351) requires no new code;
  - the existing 503 "temporarily unavailable" response already covers crawl failures and needs no change;
  - still move the shared JSON types and the slug function to `internal/platform/marketplace` (this package does not exist yet) — an organisational cleanup unrelated to the credential-model decision, following the precedent of `internal/platform/connections`.

- **Chart, local development and docs — gap: all net-new or currently wrong.**
  - delete the chart's `skills-service/configmap.yaml` (today it hardcodes `{"tenants": {}}`);
  - mount the ConfigMap with `optional: true`, taking its name from a new `skillsService.marketplaceSourcesConfigMapName` value (no such value exists today — the name is hardcoded in the deployment template); no equivalent secret-name value is needed, since this option introduces no new Secret;
  - in the same env block, fix `REDIS_PASSWORD` to read from an existing Secret instead of the chart's current hardcoded literal `"orbit"`, which violates the install ADR's "charts create no credentials" rule;
  - add an example source (no `connectionRef`) to `deploy/local/`, and add an example `allowedMarketplaceRepos` entry to the local `OrbitTenant` fixture;
  - add a tenant guide to `docs/guides/` explaining: declare your source, then request platform-admin approval for the `owner/repo`;
  - add a platform-admin runbook describing how to edit `OrbitTenant.spec.allowedMarketplaceRepos` and the expected turnaround;
  - correct `skills-service`'s port in `docs/code-organization.md` — it currently reads 8098; the service actually listens on 8085;
  - amend the backend ADR: tenant from the namespace, and allowlist-gated registration (replacing any assumption of per-tenant credentials).

---

## Validation

### Success Metrics

- A source applied by a tenant, once its `owner/repo` is allow-listed, is browsable by that tenant within one reconcile cycle, and by no other tenant.
- No code path in `skills-service` crawls a source whose `owner/repo` was never explicitly allow-listed for the requesting tenant.

### Monitoring

- The operator logs every excluded source with its reason, including every `NotAllowlisted` rejection — this is the primary signal for how much friction the approval step is causing.
- `skills-service` logs every failed crawl with its tenant, source and reason. It has no metrics pipeline today, so crawl counts are left for later.
- **New for this option:** monitor GitHub API rate-limit consumption against the single shared platform installation, and alert before it is exhausted — this did not matter under a per-tenant credential model, but is now a real shared-resource risk.

---

## Related Decisions

- [Agent Marketplace backend](2026-08-28-marketplace-adr-backend.md) - the service this feeds; amended as above
- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) - the CRD status lifecycle reused here
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md) - the operator's scope, and why it cannot call GitHub to distinguish public from private repos

This option does not depend on the `Connection` CRD or the egress gateway's credential-brokering model — unlike the Option B variant of this ADR, no `Connection`-related ADR is a prerequisite here.

---

## References

- [PR #351](https://github.com/clear-route/orbit/pull/351) - `skills-service`: binary skeleton, models, registry, GitHub client
- [Agent Marketplace PRD](../prd/agent-marketplace.md)
- [2026-10-10-skill-marketplace-source-registration.md](2026-10-10-skill-marketplace-source-registration.md) - the Option B variant of this decision
- [option-a-vs-b-credential-model.md](option-a-vs-b-credential-model.md) - the detailed comparison this choice is drawn from

**Last Updated:** 2026-10-13


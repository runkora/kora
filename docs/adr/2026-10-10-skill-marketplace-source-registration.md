# Skill Marketplace Source Registration via a Tenant-Scoped CRD

<!-- Filename: 2026-10-10-skill-marketplace-source-registration.md -->

**Status:** Proposed
**Date:** 2026-10-10
**Deciders:** Platform Engineering, Product Owner
**Technical Story:** Builds on [PR #351](https://github.com/clear-route/orbit/pull/351) (`skills-service`: binary skeleton, models, registry, GitHub client) and its [Agent Marketplace backend ADR](2026-08-28-marketplace-adr-backend.md). Product context: [Agent Marketplace PRD](../prd/agent-marketplace.md).

---

## Context

### Background

PR #351 adds `skills-service`, which serves skills from GitHub repositories declared as marketplace sources. It reads those sources from `marketplace-sources.json` in the `orbit-skill-marketplace-sources` ConfigMap, in this fixed, tenant-keyed shape (`internal/skills/registry.go`):

```json
{
  "tenants": {
    "acme":   { "sources": [ { "name": "Clear Route Skills", "org": "clear-route", "repo": "skills-marketplace", "branch": "main" } ] },
    "globex": { "sources": [ { "name": "Partner Reference",  "org": "partner",     "repo": "reference-skills",   "branch": "main" } ] }
  }
}
```

The backend ADR says the operator fills this ConfigMap from `SkillMarketplaceSource` CRs, but does not design the CRD. Today nothing fills it: the chart renders it as `{"tenants": {}}`.

A tenant is a customer organisation. Each tenant owns one namespace (`orbit-tenant-<name>`), where its admins apply CRs. Tenants must not see, affect or learn about each other.

### Problem Statement

Tenants need to register their own marketplaces, and the open question is whose credential reads a private repository. PR #351 crawls every source with one platform-wide GitHub App installation (`GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID` and one private key). Once tenants register sources themselves, that breaks in two ways:

- **Reach:** an installation belongs to one GitHub account, so a customer's private repos in its own organisation cannot be read.
- **Isolation:** the platform token is not limited to any tenant's repos. A tenant that registers a private repo the platform installation can read gets its skills served, whoever owns it.

### Goals

- A tenant admin registers a marketplace by applying one CR in the tenant's namespace.
- The operator renders exactly the JSON shape above. The API's routes and response formats, the BFF and the frontend do not change.
- A tenant can only surface repositories its own GitHub credential can read.
- Misconfigurations, including name clashes, are reported on the tenant's own CR.

### Non-Goals

- Global (platform-published) sources. Deferred; see Decision.
- The PRD's read-only Sources view (FR-13). Deferred; see Decision.
- Managing sources from the dashboard (also out of scope in the PRD).
- Providers other than GitHub, and GitHub Enterprise Server.
- A per-tenant cap on the number of sources.
- Revocation faster than the 12-hour cache lifetime.

---

## Decision Drivers

* **Fixed contract:** the start is a tenant registering a marketplace; the end is the JSON that `skills-service` already reads, with no new fields.
* **Bounded blast radius:** a bug in Orbit's code should expose at most one tenant's GitHub reach, never everything Orbit can read.
* **GitHub is the authority** on what a credential can read, as it is for the egress gateway's connections.
* **Reuse:** tenants already declare GitHub App credentials as `Connection` CRs, and the operator already aggregates tenant CRs into `orbit-system`.
* **The operator stays Kubernetes-only** and makes no GitHub calls, as the [install and distribution ADR](2026-09-04-install-and-distribution-model.md) scopes it.

---

## Considered Options

### Option A: Platform Credential with a Per-Tenant Allowlist

**Description:** Keep PR #351's single platform installation. Platform admins list on each `OrbitTenant` which `owner/repo` the tenant may register, and the operator drops everything else.

**Pros:**
- No `skills-service` change.
- Tenants share no secret.

**Cons:**
- Reaches only Orbit's own account and public repos.
- Isolation depends on Orbit's list being right, and a mistake exposes everything the platform installation can read.
- Every repository needs admin approval, public ones included, because the operator cannot tell public from private without calling GitHub.

### Option B: Tenant's Own GitHub App via its Connection

**Description:** Each source references a `github_app` `Connection` in the same namespace. `skills-service` crawls that source only with that Connection's App installation, and holds no platform credential.

**Pros:**
- GitHub caps each credential at one tenant's installation, so a bug in Orbit's credential selection exposes at most one tenant's repos.
- Tenants reach their own private repos.
- No new trust request, because tenants already declare these credentials for agents.
- Each tenant uses its own GitHub rate limit.

**Cons:**
- Which tenant's credential serves a request is still decided by Orbit's code.
- `skills-service` changes and holds every tenant's App key.
- A credential is needed even for public repos.

### Option C: Orbit's App Installed in Each Customer Account

**Description:** Customers install Orbit's App on chosen repos, and platform admins bind each GitHub account to one tenant.

**Pros:**
- Customers share no secret; it is the usual SaaS install flow.

**Cons:**
- Isolation depends on Orbit's binding being right, and one Orbit key opens every customer's selected repos.
- `skills-service` still changes, to find the installation for each repo.

---

## Decision

**Chosen Option:** Option B - Tenant's Own GitHub App via its Connection

### Rationale

B gives the smallest blast radius. Orbit's code still chooses which tenant's credential serves a request, so a selection bug could hand one tenant another tenant's token. GitHub caps every credential at one installation's reach, so such a bug exposes one tenant's repos. Under A or C, the same class of bug exposes everything Orbit's single credential can read.

A tenant's credential reaches what its installation was granted, plus public repos. Installation tokens serve public repos in any account, as the [GitHub connection ADR](2026-07-22-github-connection-category-scoped-actions.md) records. B also asks tenants for nothing new.

One rule keeps that bound: **there is no fallback**. `skills-service` keeps no platform GitHub credential, and a source whose credential is missing is not crawled.

Only GitHub App credentials are accepted. Personal access tokens are long-lived, tied to one person and cannot be narrowed. Pre-minted installation tokens expire after an hour.

### The CRD

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
    connectionRef: github        # a github_app Connection in this namespace
```

The CRD is namespaced, in `orbit.io/v1alpha1`, with a status subresource. The operator renders each source as:

| CRD | `marketplace-sources.json` |
|---|---|
| the namespace's `OrbitTenant` name | key under `tenants` |
| `displayName` | `name` |
| `github.owner` | `org` |
| `github.repo` | `repo` |
| `github.branch` | `branch` |

Field shapes are validated by the CRD schema, so a malformed source is rejected at `kubectl apply` rather than reported later in status:
- patterns for `github.owner`, `github.repo` and `github.connectionRef`, and length limits on every field;
- CEL rules, which the `Connection` CRD already uses, for what a pattern cannot express: a valid git branch name, a repo name other than `.` or `..`, and a `displayName` containing at least one letter or digit, so that its URL segment is never empty.

The operator marks a source `Invalid` only for what depends on other objects.

### Status

As with MCPServer, each source has a phase (`Pending`, `Aggregated` or `Invalid`) and an `Aggregated` condition whose reason explains it:

| Reason | Phase | When |
|---|---|---|
| `Aggregated` | Aggregated | Rendered for its tenant |
| `TenantNotActive` | Pending | The namespace has no active `OrbitTenant` |
| `GlobalSourcesNotSupported` | Invalid | Applied in `orbit-system` |
| `ConnectionInvalid` | Invalid | The Connection is missing or not `github_app`; its `baseHost` is not `github.com`; its `config` lacks `app_id`, `installation_id` or `owner`; or `config.owner` differs from `github.owner` (case-insensitive) |
| `CredentialUnavailable` | Invalid | The Connection's Secret or key is missing or unreadable |
| `DuplicateName`, `DuplicateRepository` | Invalid | An older source in this namespace already holds the name or the repo |

`Aggregated` means the source was handed to `skills-service`, not that GitHub can read the repo. The operator never calls GitHub.

`owner` sits inside the Connection's `config` and is optional there, because the egress gateway tolerates older Connections without it. A Connection used by a marketplace source must set it. The comparison is a typo guard, not a security boundary: the tenant declares `config.owner` itself, and only GitHub decides what the credential can read.

**Keeping statuses current.** Unlike MCPServer's, a source's verdict depends on its sibling sources, its Connection and that Connection's Secret, not only on its own spec. So status cannot be set only when a CR's own generation changes. Every aggregation run computes a verdict for every source and patches the status of each source whose verdict changed. A run is triggered by:

- any `SkillMarketplaceSource` change, so deleting the source that holds a name clears its rival's `DuplicateName` in the same run;
- a change to a `Connection` that a source references;
- the tenant reconcile, including suspension, deletion and its periodic 5-minute requeue. This is what sets `TenantNotActive`, and what notices a Secret that appeared or was rotated, since the operator does not watch Secrets.

### Credentials

So that the JSON file stays unchanged, the operator also writes a Secret, `orbit-skill-marketplace-credentials`, in `orbit-system`:

- `{tenant}.credentials.json`: for each `owner/repo`, the App ID, the installation ID and the key file name.
- `{tenant}.{connection}.key`: one copy of each referenced Connection's private key.

File names are built with the Connection aggregator's existing `connectionSecretRef` helper and its `secretRefSanitize` rule. Tenant and Connection names may contain dots; without sanitising, tenant `a` with Connection `b.c` and tenant `a.b` with Connection `c` would share a file name (`connection_aggregator.go`). After sanitising, the only dots are separators, so `skills-service`'s check that a key file carries the caller's tenant prefix is unambiguous.

`skills-service` reads the index for the tenant in the caller's token on every crawl, which only happens on a cache miss. It reads the key file on every token mint. A newly registered source therefore works without a restart. It builds one `TokenProvider` per installation, reusing PR #351's provider unchanged: tokens are requested read-only, and the key is re-read on every mint.

Reusing the egress gateway's `orbit-tenant-connections` ConfigMap and `orbit-tenant-connection-secrets` Secret was considered and rejected. They carry every tenant's Connections, including non-GitHub material such as Kubernetes bearer tokens, and GitHub Connections no marketplace uses. The new Secret holds only keys that a marketplace source references.

### Name clashes

Clashes are checked within one tenant only; two tenants may use the same name or the same repo.

- Two names clash when they produce the same URL segment, such as `Clear Route Skills` and `clear-route skills`. The operator uses the backend's own slug function.
- The oldest source keeps the name, ordered by `(creationTimestamp, metadata.name)`, and later ones are marked `Invalid`. Creation timestamps have one-second precision, so without the name as tiebreak, sources applied in one `kubectl apply` would win at random on each run and the marketplace would flicker.
- A second source for the same `owner/repo` is rejected, because the cache key has no branch.

### Global sources

Global sources are deferred, which differs from the PRD's scope. A source applied in `orbit-system` is marked `Invalid`. Until global sources exist, Orbit's own catalogue reaches a tenant only if its repo is public and the tenant registers it. Adding them later needs no change to the CRD or to `skills-service`.

### Sources view (PRD FR-13)

FR-13 asks for a read-only Sources view showing each source's location, visibility, freshness, package count, state and errors. This ADR does not deliver it, which also differs from the PRD's scope. Registration state and errors are on each CR, readable with `kubectl describe` by whoever administers the tenant's namespace. Freshness, package count and crawl errors are `skills-service` runtime state. Showing them needs a new read API and a frontend page, which belong in their own ADR.

---

## Consequences

### Positive

- A bug in Orbit's credential selection exposes at most one tenant's GitHub reach, because GitHub caps each credential at one installation.
- Tenants can register their own private repos, and repo owners control access through the App installation.
- The JSON file, the API, the BFF and the frontend are unchanged.
- Each tenant crawls under its own GitHub rate limit.
- Tenants can see why a source was rejected with `kubectl describe`.

### Negative

- `skills-service` holds every tenant's GitHub App key, so a compromise exposes every repo those installations can read.
- Each referenced key now exists in two `orbit-system` Secrets: the egress gateway's and this one.
- After an owner revokes access, cached content stays visible for up to 12 hours (the cache TTL). After that, opening the marketplace fails with the same generic error the API returns today. A distinct status, and a BFF change to show it, are left for later.
- A rotated key takes about 5 minutes to reach `skills-service`, because the operator does not watch Secrets.
- The ConfigMap and the Secret are projected into the pod separately. For a short time after registration, a marketplace can be listed but return 503 when opened, until its credential file arrives.
- Crawl concurrency inside `skills-service` is shared across tenants (100 parallel fetches per process), so one tenant's large crawl can delay another's.
- A tenant needs a GitHub App even to register a public repo.
- Local marketplace content needs the GitHub App key at `secrets/orbit-github-app.pem`, which `make up` treats as optional. Without it, the local marketplace stays empty, as it already is today.
- With no per-tenant cap, one tenant registering thousands of sources could fill the shared ConfigMap or Secret (1 MiB each). That would stall registration changes for every tenant.
- The PRD's global sources and Sources view are not delivered yet; source state is visible only through `kubectl`.
- The same repo cannot be registered twice in one tenant, for example on two branches.

### Neutral

- `kubectl get` shows registration state; crawl health stays in `skills-service`.
- The frontend's copy of the slug function (`frontend/src/lib/marketplace.ts`) must keep matching the Go one.

---

## Implementation

The operator, `skills-service`, and chart and docs parts below ship together. Otherwise Helm overwrites the operator's output, or `skills-service` has no credentials. Turning on authorization is the exception: it should land first, in PR #351 itself.

- **Turning on authorization:**
  - remove the hardcoded `"tenant-a"` passed to `skills.NewController` in `main.go`. A non-empty value replaces the controller's whole middleware chain (authentication, delegation and scope enforcement) with a pass-through, so every environment currently serves the marketplace unauthenticated. This is a security fix.
  - **Precondition:** removing it runs scope enforcement for the first time, and that enforcement fails closed. Every request would return 403 unless all of the following land in the same change:
    - set `policy.registerRoutes` to true. It is false in `base.yaml`, and no environment overrides it;
    - give `skills-service` the identity-proxy sidecar that route registration authenticates with, as `inference-gateway` has;
    - check that `marketplace:read` then appears in the scope vocabulary;
    - grant `marketplace:read` to tenant members through a role. `platform-admin` receives it automatically once the scope is registered;
    - verify the whole path, including the policy bundle fetch, with a `make auth` token.

- **Operator:**
  - the CRD YAML, the Go types and their registration in `scheme.go`;
  - an aggregator that writes the ConfigMap and the credentials Secret, and returns a verdict for every source;
  - a reconciler that patches the status of every source whose verdict changed, triggered as described under Status;
  - the aggregator added to the tenant reconcile (suspend, delete and the periodic run);
  - wiring in `cmd/operator/main.go`, and ClusterRole rules for the new resource and its status.
- **`skills-service`:**
  - replace the single `TokenProvider` with a per-source credential lookup;
  - when a credential is missing, return the existing 503 "temporarily unavailable" response;
  - replace the `GITHUB_APP_*` settings with `ORBIT_MARKETPLACE_CREDENTIALS_DIR`;
  - move the shared JSON types and the slug function to `internal/platform/marketplace`. This follows the precedent of `internal/platform/connections`, which exists so that the operator and other features can share metadata without importing each other.
- **Chart, local development and docs:**
  - delete the chart's `skills-service/configmap.yaml`;
  - mount the ConfigMap and the new Secret with `optional: true`, taking both names from new `skillsService.*` values as other consumers do (for example `toolGateway.mcpServersConfigMapName`);
  - in the same env block, read `REDIS_PASSWORD` from an existing Secret instead of the chart literal `"orbit"`, as the install ADR requires: charts create no credentials;
  - add an example source to `deploy/local/` and a tenant guide to `docs/guides/`;
  - correct `skills-service`'s port in `docs/code-organization.md` (8085, not 8098);
  - amend the backend ADR: tenant from the namespace, per-tenant credentials, and the revocation window.

---

## Validation

### Success Metrics

- A source applied by a tenant is browsable by that tenant within one reconcile cycle, and by no other tenant.
- No code path in `skills-service` crawls a source without the tenant's own credential.

### Monitoring

- The operator logs every excluded source with its reason.
- `skills-service` logs every failed crawl with its tenant, source and reason. It has no metrics pipeline (`observability.SetupMetrics` is opt-in, and it calls only `observability.Setup`), so crawl counts are left for later.

---

## Related Decisions

- [Agent Marketplace backend](2026-08-28-marketplace-adr-backend.md) - the service this feeds; amended as above
- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) - the CRD status lifecycle reused here
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) - the `Connection` CRD reused for credentials
- [GitHub Connection Category-Scoped Actions](2026-07-22-github-connection-category-scoped-actions.md) - the Connection `owner` field and public-repo behaviour
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md) - the operator's scope, and no Secret watch

---

## References

- [PR #351](https://github.com/clear-route/orbit/pull/351) - `skills-service`: binary skeleton, models, registry, GitHub client
- [Agent Marketplace PRD](../prd/agent-marketplace.md)

**Last Updated:** 2026-10-10

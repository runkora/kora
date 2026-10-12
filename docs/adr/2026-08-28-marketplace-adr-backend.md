# Agent Marketplace — Cache-Aside, Tenant-Scoped Redis Keys, and Resource-Aware Skills

**Status:** Proposed
**Date:** 2026-09-17
**Deciders:** Platform Engineering, Product Owner
**Technical Story:** Defines the backend architecture for the read-only Agent Marketplace: cache-aside reads through Redis, tenant-scoped Redis keys in a single shared Redis instance, a skill model that includes resources directory files, server-capped list responses, and an API contract that follows the platform's REST conventions.

---

## Context

### Background

The Orbit Marketplace is a read-only catalogue where users browse, search, and review skills — pre-packaged sets of Claude instructions hosted in approved GitHub repositories. Skills are never installed or executed from this surface.

A `skills-service` owns this capability. GitHub is the source of truth for all Marketplace content.

Marketplace sources are declared through GitOps — one `SkillMarketplaceSource` CRD per GitHub repository, scoped to a tenant. The `orbit-operator` watches all `SkillMarketplaceSource` CRDs and aggregates the valid entries into one ConfigMap (`orbit-skill-marketplace-sources`). `skills-service` reads that single ConfigMap to know which repos are declared for which tenant.

The service authenticates with GitHub using a **GitHub App**. A `TokenProvider` mints short-lived installation tokens on demand and refreshes them before expiry. All content fetches go through the platform GitHub client at `internal/platform/clients/github/`, which embeds `httpclient.Base` — the same convention every other service-to-service HTTP client follows.

A single Redis instance is shared across all tenants. Isolation is enforced by prefixing every key with the tenant ID. `skills-service` is the only service allowed to read or write these keys, and it lives in the `orbit-system` namespace — no other workload has network access to this Redis instance or to the `skills-service` API.

### Problem Statement

- The Marketplace requires server-side search/filtering with bounded list responses.
- A transient failure in one repository's GitHub fetch must not block other repositories from being served.
- Skill detail responses must include the `resources/` directory files that accompany `SKILL.md`.
- List responses must be bounded; the frontend must not hold or process the full dataset.
- The frontend needs a marketplace selector — a list of available marketplaces to display before any skills are shown.
- Tenants must be strictly isolated from each other; a tenant must not be able to see another tenant's skills.

### Goals

- Serve reads out of Redis when warm; on a miss, fetch from GitHub, populate Redis, and serve the request (cache-aside).
- Store skills per repository under separate Redis keys, all inside one shared Redis instance, with the tenant ID as the isolation prefix.
- Use a tenant-scoped internal marketplace slug (`{org}__{repo}`) for deterministic Redis key construction; generate it once in the registry.
- Include `resources/` directory files in the cached skill record and expose them in the detail API.
- Cap list responses at a server-side maximum (200 skills); the frontend never processes the full dataset.
- Keep both list and detail endpoints reading from the same Redis structure — no separate metadata-only key.
- Expose a marketplace listing endpoint so the frontend can populate the marketplace selector.
- Keep internal cache identifiers (skill slug, marketplace slug) out of API responses.
- Use the platform GitHub client (`internal/platform/clients/github/`) that embeds `httpclient.Base` — no raw `http.Client` construction inside the feature package.
- Keep Marketplace content read-only; never execute skill content.

### Non-Goals

- Package installation or execution.
- Agent runtime integration.
- Cache stampede protection — deferred; the first version uses plain cache-aside. Singleflight/lock-based deduplication is a follow-up if metrics show duplicate GitHub crawls under load.
- Startup preload — deferred; kept as a possible optimisation if the first-request latency turns out to be a real user problem.
- Webhook-driven cache invalidation — keys expire after 12 hours; the next request after expiry triggers a fresh fetch.
- Marketplace source management from the dashboard — sources are declared via `SkillMarketplaceSource` CRDs, not the UI.

---

## Decision Drivers

* **Strict tenant isolation** — one tenant must never be able to read another tenant's marketplace data. Enforced by the key prefix and by the fact that `skills-service` is the only service with access to the Redis instance.
* **Repository fault isolation** — a failing repository must not block the others.
* **Complete skill content** — the detail endpoint must return everything a consumer needs: the `SKILL.md` body and any accompanying resource files.
* **Bounded list responses** — list responses must be capped server-side; the frontend must not hold or process the full dataset.
* **Per-repository Redis keys** — cache granularity must match the unit of crawl: one repo, one key.
* **Stable internal cache identifiers** — the marketplace slug (`{org}__{repo}`) is generated once by the registry and reused for all cache operations; it is never exposed in API responses.
* **GitHub rate limits** — GitHub must not be contacted on every user interaction. A warm cache handles the common case; a miss falls through to GitHub and repopulates.
* **Platform client conventions** — GitHub calls go through a typed client under `internal/platform/clients/github/` that embeds `httpclient.Base`, matching every other service-to-service client in the codebase.
* **Correct HTTP semantics** — a cache miss is not a server failure; it is served transparently from GitHub. 5xx status codes are reserved for genuine server failures (GitHub unreachable and cache absent, Redis outage, etc.).

---

## Considered Options

### Option A: Fetch GitHub on Every Request

**Description:** Every list and detail request fetches directly from GitHub.

**Pros:**
- Always returns current data; no local storage.

**Cons:**
- Every user interaction generates GitHub API traffic.
- High latency; rate limit risk at scale.

**Rejected** — GitHub must not be contacted on every user interaction.

---

### Option B: Singleton In-Memory Store

**Description:** `skills-service` fetches all skill metadata at startup and holds it in a process-level in-memory Store. All API requests read from that Store.

**Pros:**
- No external storage or cache invalidation logic.
- Fast in-memory reads.

**Cons:**
- Store is lost on pod restart — reloaded from GitHub on next boot.
- Each Kubernetes replica holds its own independent Store — no cross-replica consistency.
- No isolation between repositories — a failing source blocks or corrupts the entire Store.

**Rejected** — cross-replica inconsistency and no per-repo fault isolation make this unsuitable beyond a prototype.

---

### Option C: Startup Preload with Per-Repo Redis Keys

**Description:** At service startup, `skills-service` crawls every configured repository in parallel and writes results to Redis before the readiness probe passes. HTTP handlers only ever read from Redis.

**Pros:**
- First user request always hits a warm cache.
- Per-repo keys isolate failures at crawl time.

**Cons:**
- Service startup is slow on first deploy or after full cache eviction — every declared repository must be crawled before the pod is ready, even repositories no one will use in a given window.
- Adds a second code path (startup crawl vs. runtime fetch) that has to be kept consistent with the request-time path.
- A missing key still has to be handled at request time (TTL expiry between preloads), so the request-time cache-miss path can't be avoided — preload just delays it.

**Rejected for V1** — cache-aside handles the same failure modes with one code path and no startup penalty. Preload can be layered on later if first-request latency proves to be a user-visible problem.

---

### Option D: Cache-Aside with Tenant-Scoped Per-Repo Redis Keys

**Description:** HTTP handlers read Redis first. On a hit, the response is served from cache. On a miss, `skills-service` crawls the repository via the GitHub Trees API, downloads `SKILL.md` and all `resources/` files, writes the `[]Skill` array to Redis with a 12h TTL, and serves the same result. All keys are prefixed with the tenant ID; one Redis instance is shared across tenants.

**Pros:**
- One code path — the request-time cache-miss path is the only cache-population path.
- No startup penalty; the pod is ready as soon as it can accept traffic.
- Only repositories a user actually requests get crawled — no wasted GitHub calls for declared-but-unused marketplaces.
- Per-repo keys isolate failures — a slow or failing repository does not affect any other cached repository.
- Tenant isolation is enforced by the key prefix; all tenants share one Redis instance, which is cheaper than one Redis per marketplace source or per tenant.
- TTL eviction remains the refresh mechanism; no dedicated refresh worker needed.

**Cons:**
- The first request for a cold repository pays a GitHub round-trip. Mitigated by the fact that `skills-service` is the only caller and GitHub responses for the full skill set are small enough to fetch inline.
- No stampede protection — two concurrent misses for the same key both hit GitHub. Deferred; will be revisited with `singleflight` if metrics show this is a real problem.

**Chosen.**

---

## Decision

**Chosen Option: D — Cache-Aside with Tenant-Scoped Per-Repo Redis Keys**

Every read goes to Redis first. On a hit, the cached `[]Skill` is used. On a miss, the handler crawls the repository through the platform GitHub client, writes `[]Skill` back to Redis with a 12h TTL, and serves the response from the same in-memory result. `skills-service` runs in the `orbit-system` namespace; it is the only service permitted to read or write these Redis keys, and network policy in the cluster enforces that.

### Skill Data Model

The complete skill record stored in Redis — the same shape is returned by the detail endpoint:

```go
type Skill struct {
    Slug        string      `json:"slug,omitempty"`
    Name        string      `json:"name"`
    Description string      `json:"description"`
    Skill       SkillFile   `json:"skill"`
    Documents   []SkillFile `json:"documents"`
}

type SkillFile struct {
    Path    string `json:"path"`
    Name    string `json:"name"`
    Content string `json:"content"`
}

// MarketplaceSource is one entry from marketplace-sources.json.
// Org, Repo, Branch, RepoName, Link, and MarketplaceSlug are backend-internal
// and never appear in API responses.
type MarketplaceSource struct {
    Name            string `json:"name"`
    Org             string `json:"org"`
    Repo            string `json:"repo"`
    RepoName        string `json:"repoName"`   // alias of Repo; set by the registry
    Link            string `json:"link"`        // https://github.com/{org}/{repo}
    MarketplaceSlug string `json:"-"`           // {org}__{repo}; set by the registry
    Branch          string `json:"branch"`
}
```

`Skill.Skill` is the `SKILL.md` file — `path`, `name`, and full `content`. `Documents` is the slice of files found under `skills/{skill-name}/resources/`. If no `resources/` directory exists, `Documents` is an empty slice — never `null`.

`Skill.Slug` carries the internal skill identifier (`{org}:{repo}:{skill-name}`) used solely to look up a skill by directory name from the cached `[]Skill` array. It is stored in Redis under `omitempty` so an empty value is omitted on serialisation. The detail endpoint explicitly clears the field before serialising the response; the list endpoint's `SkillSummary` projection type has no `Slug` field. **The slug is never returned to the frontend.**

`MarketplaceSource.MarketplaceSlug` is the internal Redis key segment (`{org}__{repo}`), generated once by the registry when loading `marketplace-sources.json`, and excluded from JSON serialisation entirely (`json:"-"`).

### Internal Slug Formats

Two separate internal identifiers are used by the backend; neither is exposed in API responses.

**Internal marketplace slug** — used as the repository segment in Redis keys:

```
{org}__{repo}
```

Example: `clear-route__skills-marketplace`

Generated once by the registry when loading `marketplace-sources.json` and stored on `MarketplaceSource.MarketplaceSlug`. The double-underscore separator distinguishes it from the URL-safe marketplace name and from path components. A `marketplaceName` URL parameter is resolved by slugifying the configured source's `Name` field; the resulting `MarketplaceSlug` is the one used for cache operations.

**Internal skill slug** — used to look up a specific skill inside the cached `[]Skill` array:

```
{org}:{repo}:{skill-name}
```

Example: `clear-route:skills-marketplace:react-expert`

`skill-name` is the directory name under `skills/` that contains the `SKILL.md` file. The slug is stable across cache evictions and pod restarts. It is stored in Redis under `omitempty` and is cleared before any detail response is written. It is never returned to the frontend.

### Redis Key Structure

One shared Redis instance across all tenants. Every key is prefixed with the tenant ID and uses the internal marketplace slug:

```
marketplace:{tenantID}:{marketplaceSlug}   →   []Skill   (TTL: 12h)
```

Where `marketplaceSlug = {org}__{repo}`. Example for a tenant with three declared marketplaces:

```text
ONE SHARED REDIS INSTANCE
│
├── marketplace:tenant-a:clear-route__skills-marketplace       →   []Skill   (TTL: 12h)
├── marketplace:tenant-a:clearroute__core-skills               →   []Skill   (TTL: 12h)
├── marketplace:tenant-a:partner__reference-skills             →   []Skill   (TTL: 12h)
├── marketplace:tenant-b:clear-route__internal-skills          →   []Skill   (TTL: 12h)
└── ...
```

Each key holds a JSON-encoded `[]Skill` array. All skills for a repository expire together — there are no per-skill sub-keys. The tenant ID is the isolation boundary; a request from `tenant-a` can never construct a key that reads `tenant-b`'s data because the tenant ID comes from the authenticated identity, not user input, and the marketplace slug is resolved from the registry, not directly from a URL parameter.

### Access Restrictions

- `skills-service` runs in the `orbit-system` namespace.
- The Redis instance is exposed only inside the mesh, with an authorization policy that allows connections from `skills-service` only.
- `skills-service` is the sole owner of these Redis keys — no other service reads or writes them.
- The tenant ID used to construct every Redis key is derived from the authenticated caller's identity, not from any request parameter, so a request cannot cross tenant boundaries even if the URL is manipulated.

### ConfigMap Structure

The `orbit-operator` renders all valid `SkillMarketplaceSource` CRDs into the `orbit-skill-marketplace-sources` ConfigMap as `marketplace-sources.json`. Sources are grouped by tenant ID; the tenant ID is derived by the operator from the namespace in which each `SkillMarketplaceSource` CRD is installed — it is never a field on the CRD spec itself:

```json
{
  "tenants": {
    "tenant-a": {
      "sources": [
        { "name": "Clear Route Skills", "org": "clear-route", "repo": "skills-marketplace", "branch": "main" },
        { "name": "ClearRoute Core",    "org": "clearroute",  "repo": "core-skills",        "branch": "main" }
      ]
    },
    "tenant-b": {
      "sources": [
        { "name": "Partner Reference",  "org": "partner",     "repo": "reference-skills",   "branch": "main" }
      ]
    }
  }
}
```

`name` is the human-readable label — this is the **only** field the FE ever sees. `org`, `repo`, and `branch` are backend-internal: they never appear in any API response, and `skills-service` looks them up from the ConfigMap by `name` when it needs to crawl GitHub or build a Redis key. `name` is unique within a tenant's source list and is used verbatim (URL-safe-slugified) as the marketplace identifier in URLs.

One shared ConfigMap holds all tenants. Individual source entries carry no `tenantId` field — isolation is structural: sources are already nested under their tenant's key. `skills-service` scopes every registry lookup to the tenant ID resolved from the authenticated caller's identity context, so a request can never reach another tenant's sources even if the URL is manipulated.

`skills-service` mounts `orbit-skill-marketplace-sources` as a read-only file at `ORBIT_MARKETPLACE_SOURCES_CONFIG`, reads `marketplace-sources.json` on startup, and hot-reloads it via `fsnotify`. The backend never imports the CRD type and never calls the Kubernetes API.

### GitHub App Authentication and Platform Client

`TokenProvider` (in the feature package) holds the App private key, App ID, and installation ID (from Kubernetes Secret). It mints a short-lived installation token on first use and refreshes it before expiry.

All GitHub calls go through the platform client at:

```
backend/internal/platform/clients/github/client.go
```

This client embeds `httpclient.Base` — the same base every other service-to-service client uses — and therefore gets W3C trace propagation, OTel span lifecycle, and structured logging for free. `skills-service` obtains it through the standard client wiring in `main.go`; it never constructs an `http.Client{}` literal, which is banned by the `noRawHttpClientLiteral` lint rule anyway.

The feature package supplies the token by injecting a `TokenProvider` into the client at construction time; the client itself has no knowledge of GitHub App semantics and only knows how to add an `Authorization: Bearer <token>` header from whatever source it is given. This mirrors how other typed clients handle credential injection.

### Request Path and Tenant Resolution

The BFF does **not** extract or pass `tenant_id`. It just forwards the auth token. The tenant ID is resolved inside `skills-service` from the authenticated caller's context (the identity carried by `AuthMiddleware`):

```
FE → BFF (forwards bearer token) → skills-service
         → AuthMiddleware verifies identity, puts claims on ctx
         → handler reads tenantID from token claims
         → path param marketplaceName → registry lookup → MarketplaceSource { org, repo }
         → marketplaceSlug = {org}__{repo}
         → produce key marketplace:{tenantID}:{marketplaceSlug}
         → Redis GET/SET
```

Why it matters: if the BFF or the URL supplied `tenant_id`, a caller could tamper with it and read another tenant's keys. Deriving it from the verified identity is what makes the prefix a real isolation boundary. Similarly, the `marketplaceSlug` comes from the registry (resolved by the public name), never directly from URL input.

### Request Flow (Cache-Aside)

```text
HTTP request arrives at skills-service
   │  path: /v1/marketplaces/{marketplaceName}/skills[/{skillName}]
   ▼
Resolve tenantID from the authenticated caller (token claims)
   │
   ▼
Slugify marketplaceName and match against tenant's registry
   │   → MarketplaceSource { org, repo, branch }   (404 if not found or ambiguous)
   ▼
marketplaceSlug = {org}__{repo}
Construct key: marketplace:{tenantID}:{marketplaceSlug}
   │
   ▼
Redis GET
   │
   ├── HIT ──► deserialise []Skill ──► apply search / server-cap ──► respond 200
   │
   └── MISS
         │
         ▼
      platform/clients/github (with TokenProvider)
         ├── GET /repos/{org}/{repo}/git/trees/{branch}?recursive=1
         │     → filter paths matching skills/*/SKILL.md
         │     → filter paths matching skills/*/resources/*
         │
         └── for each skill directory (parallel, per-repo cap 20; global cap 100):
               ├── fetch skills/{name}/SKILL.md          → Skill
               └── fetch skills/{name}/resources/* (all) → Documents
         │
         ▼
      Redis SET marketplace:{tenantID}:{marketplaceSlug} (TTL: 12h)
         │
         ▼
      apply search / server-cap ──► respond 200
```

If GitHub is unreachable on a miss and there is no cached value to fall back on, the handler responds with `503 Service Unavailable` — that is a genuine server-side failure, not a cache-miss condition. A miss that GitHub serves successfully is not a failure and is never surfaced to the frontend as one.

### Repository Discovery

The tree crawl filters for two path patterns:

| Pattern | Meaning |
|---|---|
| `skills/*/SKILL.md` | The canonical skill definition file |
| `skills/*/resources/*` | All files in the adjacent resources directory |

A skill is identified by the directory name between `skills/` and `/SKILL.md`. A `resources/` directory is optional — its absence produces `"documents": []`. Any `SKILL.md` missing a required frontmatter field is skipped with a structured warning log; its absence does not fail the crawl for other skills.

### SKILL.md Frontmatter Schema

Every `SKILL.md` must open with a YAML frontmatter block delimited by `---`:

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | **Yes** | Human-readable display name; max 100 characters |
| `description` | string | **Yes** | Short summary shown on skill cards; max 300 characters |

### API Surface

All Marketplace endpoints live under `/v1/marketplaces`, matching the versioned REST layout used elsewhere in Orbit. The tenant is resolved from the authenticated caller and is never a path parameter. The frontend addresses a marketplace by its `name` only; `org`, `repo`, and `branch` are backend-internal and never appear in a URL or response.

| Method | Path | Description |
|---|---|---|
| GET | `/v1/marketplaces` | List marketplaces declared for the caller's tenant (name only) |
| GET | `/v1/marketplaces/{marketplaceName}/skills` | Server-capped skill list — metadata only (`name`, `description`) |
| GET | `/v1/marketplaces/{marketplaceName}/skills/{skillName}` | Full skill detail — `SKILL.md` body + resource files |

`{marketplaceName}` is the URL-safe slug of a `name` from the ConfigMap. `{skillName}` is the directory name under `skills/` and cannot contain a slash.

#### `GET /v1/marketplaces`

Returns the marketplaces declared for the caller's tenant, read from the tenant's `marketplace-sources.json`. No Redis or GitHub call. Only `name` is exposed; `org`, `repo`, and `branch` are stripped.

Response (HTTP 200):

```json
{
  "marketplaces": [
    { "name": "Clear Route Skills" },
    { "name": "ClearRoute Core" },
    { "name": "Partner Reference" }
  ]
}
```

#### `GET /v1/marketplaces/{marketplaceName}/skills`

Query parameters:

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `search` | string | No | Case-insensitive substring match on `name`; omit to return all skills |

The server:

1. Resolves `tenantID` from the authenticated caller.
2. Slugifies `marketplaceName` and matches it against the tenant's registry → `MarketplaceSource { org, repo, branch }`. If no unique match, returns `404`.
3. Constructs `marketplace:{tenantID}:{org}__{repo}` and reads Redis. On a miss, crawls the repository as described in the request flow, writes the result to Redis with a 12h TTL, then serves from the same in-memory result.
4. Applies the `search` filter and caps the result at 200 skills.

**Successful response (HTTP 200):**

```json
{
  "skills": [
    { "name": "React Expert", "description": "Senior React developer" }
  ]
}
```

`Skill.Skill`, `Skill.Documents`, and `Skill.Slug` are always stripped before the response is written; the list item type (`SkillSummary`) exposes only `name` and `description`. An empty result returns `{ "skills": [] }` with HTTP 200.

If `marketplaceName` is not declared for the caller's tenant (or is ambiguous), the handler returns `404 Not Found`.

If Redis is unavailable, or if the cache is cold and GitHub is unreachable, the handler returns `503 Service Unavailable`. These are genuine server failures.

#### `GET /v1/marketplaces/{marketplaceName}/skills/{skillName}`

Example:

```
GET /v1/marketplaces/clear-route-skills/skills/react-expert
```

Response (HTTP 200):

```json
{
  "name": "React Expert",
  "description": "Senior React developer",
  "skill": {
    "path": "skills/react-expert/SKILL.md",
    "name": "SKILL.md",
    "content": "<complete SKILL.md content>"
  },
  "documents": [
    {
      "path": "skills/react-expert/resources/examples.md",
      "name": "examples.md",
      "content": "<complete file content>"
    }
  ]
}
```

`skill` is always present. `documents` is an empty array if the skill has no `resources/` directory. `Skill.Slug` is explicitly cleared before serialisation and is not present in the response.

On a Redis miss, the handler performs the same cache-aside crawl the list endpoint does (after the same `marketplaceName` → `MarketplaceSource` registry lookup), populates Redis, and looks up the requested `skillName` by matching against `Skill.Slug` (which encodes the skill directory name as `{org}:{repo}:{skillName}`). If the marketplace is not declared for the tenant, or if the crawl completes but `skillName` is not present in the repository, the handler returns `404 Not Found`. `503` is reserved for the same genuine server-failure cases as the list endpoint.

### Status-Code Summary

| Situation | Status |
|---|---|
| Cache hit | 200 |
| Cache miss, GitHub fetch succeeds | 200 |
| Marketplace not declared for caller's tenant | 404 |
| Skill directory does not exist in the repo | 404 |
| Redis unavailable | 503 |
| Cache miss + GitHub unreachable / rate-limited with no fallback | 503 |

A cache miss on its own is never surfaced as an error. 5xx codes are reserved for genuine server-side failures.

### TTL

| Key | Default TTL | Configuration |
|---|---|---|
| `marketplace:{tenantID}:{org}__{repo}` | 12 hours | `MARKETPLACE_CACHE_TTL` |
| GitHub App installation token | ~55 minutes | Refreshed 5 min before GitHub's 60-min expiry |

When a key expires, the next request that targets it triggers a fresh crawl and repopulates the key.


### Authorization

All three endpoints require scope `marketplace:read`. Middleware chain: `AuthMiddleware → DelegationMiddleware → ScopeEnforcementMiddleware`. The tenant ID is extracted from the authenticated caller's context and used verbatim as the Redis key prefix; there is no code path where a client-supplied value can influence which tenant's keys are read.

---

## Consequences

### Positive

- One cache-population code path. The request-time miss path is the only miss path, so there is no risk of a startup crawl and a runtime crawl diverging.
- No startup penalty. The pod is ready as soon as it can accept traffic; cold repositories pay a one-time miss cost when first requested.
- Strict tenant isolation with cheap infrastructure. All tenants share one Redis instance; the tenant ID prefix is the boundary, and the fact that `skills-service` is the only reader/writer makes cross-tenant access structurally impossible.
- Per-repo isolation: a failing GitHub fetch for one repository does not affect any other cached repository.
- The detail endpoint returns the complete skill content — `SKILL.md` body and all `resources/` files — in one response.
- A server-side cap (200 skills per marketplace) keeps list response sizes bounded regardless of dataset size.
- Internal identifiers (marketplace slug, skill slug) are generated and consumed entirely within the backend and never appear in API responses.
- URLs follow the platform's `/v1/{resource}` REST conventions and are consistent with the rest of the API.
- GitHub calls go through the standard platform client, so tracing, logging, and metrics work the same way they do for every other service-to-service call.

### Negative

- The first request for a cold repository pays a GitHub round-trip inline. Acceptable because the payload is small and the alternative (startup preload) crawls repositories that may never be requested.
- No cache-stampede protection in V1 — a burst of concurrent cold-cache requests for the same key can cause duplicate GitHub crawls. Acceptable at expected traffic; will be revisited with `singleflight` or a Redis lock if metrics show real duplication.
- Redis is a hard runtime dependency. A Redis outage results in `503` from the list and detail endpoints (the marketplace listing endpoint is unaffected because it reads only from the ConfigMap).
- Storing `Skill` and `Documents` for every skill increases Redis memory consumption compared to metadata-only storage. The full record is needed for detail requests, so caching it avoids a second GitHub round-trip per click.

---

## Implementation

### Step 1 — Admin Declares Marketplace Sources

A platform operator creates one `SkillMarketplaceSource` CRD per GitHub repository via GitOps — specifying the `tenantId`, `name`, `org`, `repo`, and `branch` for each. The `orbit-operator` validates each CRD, marks it `Aggregated` or `Invalid`, and renders the approved set into the `orbit-skill-marketplace-sources` ConfigMap. This is a one-time setup step per repository; the operator keeps the ConfigMap up to date as CRDs are added, changed, or removed.

### Step 2 — Service Starts

When `skills-service` starts:

1. Reads `ORBIT_MARKETPLACE_SOURCES_CONFIG/marketplace-sources.json` to build the tenant-scoped view of declared marketplaces.
2. Builds a `TokenProvider` that mints a GitHub App installation token on demand and refreshes it before expiry.
3. Constructs the platform GitHub client (`internal/platform/clients/github`) with the `TokenProvider` and wires it into the service.
4. Passes the readiness probe. No preload is performed.

### Step 3 — User Opens the Marketplace

1. The frontend calls `GET /v1/marketplaces`.
2. `skills-service` reads the tenant's `marketplace-sources.json` and returns `name` only for each declared marketplace. No Redis or GitHub call.
3. The frontend displays the marketplace selector — the dropdown shows names only.

### Step 4 — User Browses Skills

1. The frontend calls `GET /v1/marketplaces/{marketplaceName}/skills`
2. `skills-service` resolves `tenantID` from the authenticated caller.
3. Slugifies `marketplaceName` and matches it against the tenant's registry → `MarketplaceSource { org, repo, branch }`.
4. Constructs the key `marketplace:{tenantID}:{org}__{repo}` and reads Redis.
5. On a hit: deserialise, apply search + server-cap, strip `Skill`, `Documents`, and `Slug`, respond `200`.
6. On a miss: crawl the repository via the platform GitHub client, write `[]Skill` back to Redis with a 12h TTL, apply search + server-cap, strip, respond `200`.
7. If Redis is unavailable or GitHub is unreachable on a cold-cache miss, respond `503`.

### Step 5 — User Opens a Skill Detail Page

1. The frontend calls `GET /v1/marketplaces/{marketplaceName}/skills/{skillName}`.
2. `skills-service` performs the same `tenantID` resolution and `marketplaceName` → `MarketplaceSource` registry lookup as the list endpoint.
3. Reads Redis; on a miss it runs the same cache-aside crawl the list endpoint does.
4. Looks up `skillName` by matching against `Skill.Slug` (`{org}:{repo}:{skillName}`). If found, responds `200` with the full record (slug cleared); if not found in the crawled repository, responds `404`.

### Service Layout

```
backend/internal/skills/
├── models.go                   — Skill, SkillFile, MarketplaceSource domain types
├── cache.go                    — Redis read/write; tenant-scoped key helpers; TTL constants
├── crawler.go                  — GitHub Trees API walk; SKILL.md + resources fetch
├── registry.go                 — Reads marketplace-sources.json; generates MarketplaceSlug;
│                                  hot-reload via fsnotify; tenant → sources mapping
├── token.go                    — TokenProvider: GitHub App token mint and refresh
├── service.go                  — Cache-aside orchestration; search, server-cap, field projection
├── handlers_skills_entry.go    — HTTP controllers for marketplaces, list, and detail endpoints
├── formatter.go                — Response formatting helpers
└── errors.go                   — Sentinel errors: ErrMarketplaceNotFound, ErrSkillNotFound,
                                   ErrRedisUnavailable

backend/internal/platform/clients/github/
└── client.go        — Typed GitHub client embedding httpclient.Base
                       (Trees API + raw content fetch); accepts a token provider

backend/internal/operator/
└── marketplace_aggregator.go  — MarketplaceSourceAggregator: watches SkillMarketplaceSource CRDs;
                                  validates sources; marks Pending/Aggregated/Invalid;
                                  renders orbit-skill-marketplace-sources ConfigMap
```

The GitHub HTTP client lives under `internal/platform/clients/github/` because the platform-client convention requires it. The feature package supplies GitHub App semantics (`TokenProvider`) but never constructs its own HTTP client.

### API Style

REST is used for V1. The Marketplace has a small, well-defined set of operations and a flat data model — GraphQL provides no meaningful advantage here. GraphQL can be reconsidered if the data model becomes significantly more relational.

### Delivery Phases

**Phase 1 — Configuration and platform client**
Set up `skills-service` skeleton, ConfigMap and Secret mounts, `registry.Registry` loading from `marketplace-sources.json` with tenant scoping, `TokenProvider` with GitHub App token mint and cache, and `internal/platform/clients/github`. Service starts, reads credentials, and can make authenticated calls to the GitHub Trees API and `raw.githubusercontent.com`.

**Phase 2 — Cache-aside read path**
Implement `cache.go`, `crawler.go`, and `service.go`: tenant-scoped Redis key helpers using `{org}__{repo}` marketplace slugs, `SKILL.md` and `resources/` crawl with `{org}:{repo}:{skillName}` internal skill slugs, `[]Skill` write to Redis on miss, and the cache-aside orchestration used by the handlers.

**Phase 3 — API endpoints**
Implement `GET /v1/marketplaces` (returns names only), `GET /v1/marketplaces/{marketplaceName}/skills` (list, search, server-side cap at 200), and `GET /v1/marketplaces/{marketplaceName}/skills/{skillName}` (full detail). Wire the `marketplaceName` → registry → `MarketplaceSource` lookup and the status-code semantics (`200`/`404`/`503`) as defined above. Wire the BFF proxy. Frontend can populate the selector, browse, and open skills end to end.

**Phase 4 (deferred) — Stampede protection and/or startup preload**
Revisit if metrics show duplicate crawls on cold-cache bursts, or if the first-request-after-expiry latency turns out to be a real user problem. Both are additive to the cache-aside design and require no changes to the API contract.

### Migration Path

No migration is required — `skills-service` is a new service with no existing data to migrate. The `SkillMarketplaceSource` CRD and `orbit-skill-marketplace-sources` ConfigMap are new resources with no prior state.

---

**Last Updated:** 2026-10-10

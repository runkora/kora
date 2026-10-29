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

Marketplace sources are declared through GitOps — one `SkillMarketplaceSource` CRD per GitHub repository, scoped to a tenant. The `orbit-operator` watches all `SkillMarketplaceSource` CRDs and aggregates the valid entries into one ConfigMap (`orbit-skill-marketplace-sources`). `skills-service` reads a `skills-mapping.yaml` file from that ConfigMap to know which repos are declared for which tenant.

The service authenticates with GitHub using a **GitHub App**. A `TokenProvider` mints short-lived installation tokens on demand and refreshes them before expiry. Multiple marketplace sources may use different GitHub App credentials via per-source `credentialsRef`; a `TokenProviderPool` lazily constructs a `TokenProvider` per credential directory. All content fetches go through the platform GitHub client at `internal/platform/clients/github/`, which embeds `httpclient.Base` — the same convention every other service-to-service HTTP client follows.

A single Redis instance is shared across all tenants. Isolation is enforced by prefixing marketplace list keys with the tenant ID. `skills-service` is the only service allowed to read or write these keys, and it lives in the `orbit-system` namespace — no other workload has network access to this Redis instance or to the `skills-service` API.

### Problem Statement

- The Marketplace requires server-side search/filtering with bounded list responses.
- A transient failure in one repository's GitHub fetch must not block other repositories from being served.
- Skill detail responses must include any `.md` resource files adjacent to the `SKILL.md`.
- List responses must be bounded; the frontend must not hold or process the full dataset.
- The frontend needs a marketplace selector — a list of available marketplaces to display before any skills are shown.
- Tenants must be strictly isolated from each other; a tenant must not be able to see another tenant's skills.

### Goals

- Serve reads out of Redis when warm; on a miss, fetch from GitHub, populate Redis, and serve the request (cache-aside).
- Store skill entries per repository under separate Redis keys, all inside one shared Redis instance, with the tenant ID as the isolation prefix.
- Use a tenant-scoped internal marketplace slug (`{org}__{repo}`) for deterministic Redis key construction; generate it once in the registry.
- Cache lightweight `SkillEntry` records (no file content) for list operations; cache full `Skill` records separately per skill for detail operations.
- Cap list responses at a server-side maximum (200 skills); the frontend never processes the full dataset.
- Expose a marketplace listing endpoint so the frontend can populate the marketplace selector, returning the marketplace's slug and link alongside its name.
- Use the platform GitHub client (`internal/platform/clients/github/`) that embeds `httpclient.Base` — no raw `http.Client` construction inside the feature package.
- Keep Marketplace content read-only; never execute skill content.

### Non-Goals

- Package installation or execution.
- Agent runtime integration.
- Cache stampede protection — deferred; the first version uses plain cache-aside. Singleflight/lock-based deduplication is a follow-up if metrics show duplicate GitHub crawls under load.
- Startup preload — deferred; kept as a possible optimisation if the first-request latency turns out to be a real user problem.
- Webhook-driven cache invalidation — keys expire after the configured TTL; the next request after expiry triggers a fresh fetch.
- Marketplace source management from the dashboard — sources are declared via `SkillMarketplaceSource` CRDs, not the UI.
- Hot-reload of the skills-mapping.yaml — the registry loads once at startup; a restart picks up changes.

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

**Description:** HTTP handlers read Redis first. On a hit, the response is served from cache. On a miss, `skills-service` crawls the repository via the GitHub Trees API, downloads every `SKILL.md` and records adjacent `.md` resource paths, writes a `[]SkillEntry` array to Redis with the configured TTL, and serves the same result. Full skill content is fetched and cached per-skill on first detail request. Marketplace list keys are prefixed with the tenant ID; one Redis instance is shared across tenants.

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

Every read goes to Redis first. On a hit, the cached `[]SkillEntry` is used directly. On a miss, the handler crawls the repository through the platform GitHub client, writes `[]SkillEntry` back to Redis with the configured TTL, and serves the response from the same in-memory result. Full `Skill` content (file bodies) is cached separately per-skill on first detail request. `skills-service` runs in the `orbit-system` namespace; it is the only service permitted to read or write these Redis keys, and network policy in the cluster enforces that.

### Skill Data Model

The types used across the cache, service, and API layers:

```go
// MarketplaceSource is one marketplace entry derived from skills-mapping.yaml.
// Org, RepoName, and MarketplaceSlug are backend-internal — they never appear
// in API responses.
type MarketplaceSource struct {
    Name            string `json:"name"`
    Org             string `json:"org"`
    RepoName        string `json:"repoName"`
    Link            string `json:"link"`            // https://github.com/{org}/{repo}
    MarketplaceSlug string `json:"marketplaceSlug"` // {org}__{repo}; set by the registry
}

// SkillEntry is the lightweight record stored in the marketplace Redis key.
// Path and DocPaths are backend-only routing hints used when fetching
// full content on a skill-cache miss; the frontend never sees them.
type SkillEntry struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    SkillSlug   string   `json:"skillSlug"`
    Path        string   `json:"path"`
    DocPaths    []string `json:"docPaths,omitempty"`
}

// SkillFile is one file (SKILL.md or a resource document).
type SkillFile struct {
    Path    string `json:"path"`
    Name    string `json:"name"`
    Content string `json:"content"`
}

// Skill is the full skill record returned by the detail endpoint and cached
// per-skill in Redis.
type Skill struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    SkillSlug   string      `json:"skillSlug"`
    File        SkillFile   `json:"skill"`
    Documents   []SkillFile `json:"documents"`
}

// SkillSummary is the list-endpoint projection sent to the frontend.
type SkillSummary struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    SkillSlug   string `json:"skillSlug"`
    SkillName   string `json:"skillName"` // nameSlug alone; addresses the detail endpoint
}
```

`Skill.File` is the `SKILL.md` file — `path`, `name`, and full `content`. `Documents` is the slice of `.md` files found adjacent to `SKILL.md` (in the same directory tree, at any depth). If no such files exist, `Documents` is an empty slice — never `null`.

`SkillEntry.SkillSlug` and `Skill.SkillSlug` carry the public skill identifier (`{marketplaceSlug}__{nameSlug}`) used by the frontend to address skills and by the service to look up entries. The slug is **returned to the frontend** in both list and detail responses.

`MarketplaceSource.MarketplaceSlug` is the Redis key segment (`{org}__{repo}`), generated once by the registry when loading `skills-mapping.yaml`. Unlike the `json:"-"` placeholder in earlier drafts, the field is serialised with `json:"marketplaceSlug"` because it is also returned in the `GET /v1/marketplaces` response — the frontend needs it to address subsequent skill list requests.

### Internal Slug Formats

Two identifiers are used by the backend; the skill slug is also exposed in API responses.

**Marketplace slug** — used as the repository segment in Redis keys and as the URL path parameter:

```
{org}__{repo}
```

Example: `clear-route__skills-marketplace`

Generated once by the registry when loading `skills-mapping.yaml` and stored on `MarketplaceSource.MarketplaceSlug`. The double-underscore separator distinguishes it from URL path components. The `{marketplaceSlug}` URL parameter is matched directly against `MarketplaceSource.MarketplaceSlug` — no extra slugification step.

**Skill slug** — used to identify and address a skill:

```
{marketplaceSlug}__{nameSlug}
```

Example: `clear-route__skills-marketplace__react-expert`

`nameSlug` is the slugified form of the `name` frontmatter field (lowercase, non-alphanumeric runs replaced by `-`). The slug is stable across cache evictions and pod restarts. It is returned to the frontend in both list and detail responses.

**Skill name** — the `nameSlug` alone, e.g. `react-expert`. The list response returns it as `skillName`, and it is the value the frontend puts in its route (`/marketplace/{marketplaceSlug}/{skillName}`) and passes as the `{skillName}` URL parameter of the detail endpoint. The service rebuilds the full slug as `marketplaceSlug + "__" + skillName`, so the frontend never splits or joins on the `__` separator. For callers that still address skills by slug, the endpoint also accepts the full `{marketplaceSlug}__{nameSlug}` and strips the `{marketplaceSlug}__` prefix. Any other value that is not a valid `nameSlug` (`^[a-z0-9]+(-[a-z0-9]+)*$`), including another marketplace's skill slug, is rejected with `400`.

### Redis Key Structure

One shared Redis instance across all tenants. Two key shapes are used:

```
marketplace:{tenantID}:{marketplaceSlug}  →  JSON []SkillEntry  (TTL: configured)
{skillSlug}                               →  JSON Skill          (TTL: configured)
```

Where `marketplaceSlug = {org}__{repo}`. Example for a tenant with two declared marketplaces:

```text
ONE SHARED REDIS INSTANCE
│
├── marketplace:tenant-a:clear-route__skills-marketplace  →  []SkillEntry  (TTL: configured)
├── marketplace:tenant-a:clearroute__core-skills          →  []SkillEntry  (TTL: configured)
├── clear-route__skills-marketplace__react-expert         →  Skill         (TTL: configured)
├── marketplace:tenant-b:clear-route__internal-skills     →  []SkillEntry  (TTL: configured)
└── ...
```

Marketplace list keys store only lightweight `SkillEntry` records (name, description, slug, and path hints) — no file content. Full content is loaded on demand and cached in a separate per-skill key whose `skillSlug` already encodes the marketplace. Tenant access to per-skill keys is enforced at the service layer before Redis is ever read, so the fact that skill keys have no tenant prefix does not weaken isolation.

The tenant ID is the isolation boundary for list keys; a request from `tenant-a` can never construct a key that reads `tenant-b`'s list data because the tenant ID comes from the authenticated identity, not user input, and the marketplace slug is matched from the registry, not directly from a URL parameter.

### Access Restrictions

- `skills-service` runs in the `orbit-system` namespace.
- The Redis instance is exposed only inside the mesh, with an authorization policy that allows connections from `skills-service` only.
- `skills-service` is the sole owner of these Redis keys — no other service reads or writes them.
- The tenant ID used to construct every marketplace list key is derived from the authenticated caller's identity, not from any request parameter, so a request cannot cross tenant boundaries even if the URL is manipulated.

### ConfigMap Structure

The `orbit-operator` renders all valid `SkillMarketplaceSource` CRDs into the `orbit-skill-marketplace-sources` ConfigMap as `skills-mapping.yaml`. Sources are grouped by tenant ID; the tenant ID is derived by the operator from the namespace in which each `SkillMarketplaceSource` CRD is installed — it is never a field on the CRD spec itself:

```yaml
skillsMapping:
  tenants:
    - id: tenant-a
      marketplaces:
        - https://github.com/clear-route/skills-marketplace
        - https://github.com/clearroute/core-skills
    - id: tenant-b
      marketplaces:
        - https://github.com/partner/reference-skills
```

Each entry in `marketplaces` is a GitHub repository URL. The registry parses each URL into a `MarketplaceSource`: `Org` and `RepoName` are extracted from the URL path, `Name` defaults to the lowercased repository name, and `MarketplaceSlug` is set to `{org}__{repo}`. Duplicate URLs within one tenant's list are silently deduplicated; invalid URLs are skipped with a warning.

`Branch` is not configurable — the crawler always uses `HEAD`, which GitHub resolves to the repository's default branch. There is no per-source branch override.

`skills-service` mounts `orbit-skill-marketplace-sources` as a read-only directory at `ORBIT_MARKETPLACE_SOURCES_CONFIG`, reads `skills-mapping.yaml` once at startup, and does not hot-reload. A restart picks up any ConfigMap changes. The backend never imports the CRD type and never calls the Kubernetes API.

### GitHub App Authentication and Platform Client

`TokenProvider` (in the feature package) holds the App private key path, App ID, and installation ID. It mints a short-lived installation token on first use and refreshes it before expiry. The private key is read fresh on every mint — it is never decoded and held resident in memory, so a rotated key takes effect on the next mint without a process restart.

Multiple marketplace sources may use different GitHub App credentials. `TokenProviderPool` lazily constructs and caches one `TokenProvider` per `credentialsRef` directory path. A source with an empty `CredentialsRef` falls back to the default provider configured at startup. Each `credentialsRef` directory must contain three files: `appID`, `installationID`, and `privateKey` (PEM-encoded RSA key), matching the Kubernetes Secret mount shape.

All GitHub calls go through the platform client at:

```
backend/internal/platform/clients/github/client.go
```

This client embeds `httpclient.Base` — the same base every other service-to-service client uses — and therefore gets W3C trace propagation, OTel span lifecycle, and structured logging for free. `skills-service` obtains it through the standard client wiring in `main.go`; it never constructs an `http.Client{}` literal, which is banned by the `noRawHttpClientLiteral` lint rule anyway.

The feature package supplies the token by injecting a `TokenProvider` (or pool) into the crawler at construction time; the GitHub client itself has no knowledge of GitHub App semantics and only knows how to add an `Authorization: Bearer <token>` header from whatever source it is given.

### Request Path and Tenant Resolution

The BFF does **not** extract or pass `tenant_id`. It just forwards the auth token. The tenant ID is resolved inside `skills-service` from the authenticated caller's context (the identity carried by `AuthMiddleware`):

```
FE → BFF (forwards bearer token) → skills-service
         → AuthMiddleware verifies identity, puts claims on ctx
         → handler reads tenantID from token claims
         → path param marketplaceSlug → registry match → MarketplaceSource { org, repoName }
         → produce key marketplace:{tenantID}:{marketplaceSlug}
         → Redis GET/SET
```

Why it matters: if the BFF or the URL supplied `tenant_id`, a caller could tamper with it and read another tenant's keys. Deriving it from the verified identity is what makes the prefix a real isolation boundary. Similarly, the `marketplaceSlug` is validated against the registry (must exist in the tenant's source list), never used raw from URL input.

### Request Flow (Cache-Aside)

```text
HTTP request arrives at skills-service
   │  path: /v1/marketplaces/{marketplaceSlug}[/{skillName}]
   ▼
Resolve tenantID from the authenticated caller (token claims)
   │
   ▼
Match marketplaceSlug against tenant's registry
   │   → MarketplaceSource { org, repoName }   (404 if not found)
   ▼
Construct key: marketplace:{tenantID}:{marketplaceSlug}
   │
   ▼
Redis GET
   │
   ├── HIT ──► deserialise []SkillEntry ──► apply search / server-cap ──► respond 200
   │
   └── MISS
         │
         ▼
      platform/clients/github (with TokenProvider/Pool)
         ├── GetTree(org, repo, "HEAD")
         │     → walk all blobs; identify each SKILL.md by filename
         │     → collect adjacent .md files as docPaths
         │
         └── for each skill (parallel, per-repo cap 20; global cap 100):
               fetch SKILL.md  →  parse frontmatter  →  SkillEntry
               (docPaths recorded; content not fetched here)
         │
         ▼
      Redis SET marketplace:{tenantID}:{marketplaceSlug} TTL=configured
         │
         ▼
      apply search / server-cap ──► respond 200


For GET /v1/marketplaces/{marketplaceSlug}/{skillName}:
   │  skillSlug = marketplaceSlug + "__" + skillName
   │  (after marketplace list resolved as above)
   ▼
Find skillSlug in []SkillEntry   (404 if absent)
   │
   ▼
Redis GET {skillSlug}
   │
   ├── HIT ──► respond 200 with full Skill
   │
   └── MISS
         │  look up SkillEntry in []SkillEntry for entry.Path + entry.DocPaths
         ▼
      platform/clients/github (with TokenProvider/Pool)
         fetch SKILL.md + all docPaths in parallel
         │
         ▼
      Redis SET {skillSlug} TTL=configured  →  respond 200
```

If GitHub is unreachable on a miss and there is no cached value to fall back on, the handler responds with `503 Service Unavailable` — that is a genuine server-side failure, not a cache-miss condition. A miss that GitHub serves successfully is not a failure and is never surfaced to the frontend as one.

### Repository Discovery

The tree crawl walks every blob in the repository at `HEAD`. A `SKILL.md` at the repository root is skipped with a warning — a skill needs its own directory. A `SKILL.md` anywhere below the root defines a skill whose directory is the folder containing it. Resource documents are any files (not `SKILL.md`) that share a directory tree with a skill's folder; binary files (fonts, images, archives — anything that is not UTF-8 text or contains a NUL byte) are skipped after download, since a document's content is text; when directories nest, a file is assigned to the most specific (longest path) skill folder.

### SKILL.md Frontmatter Schema

Every `SKILL.md` must open with a YAML frontmatter block delimited by `---`:

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | **Yes** | Human-readable display name; max 100 characters |
| `description` | string | **Yes** | Short summary shown on skill cards; max 300 characters |

### API Surface

All Marketplace endpoints live under `/v1/marketplaces`, matching the versioned REST layout used elsewhere in Orbit. The tenant is resolved from the authenticated caller and is never a path parameter. The frontend addresses a marketplace by its `marketplaceSlug`; `org` and `repoName` are backend-internal and never appear in a URL or response.

| Method | Path | Description |
|---|---|---|
| GET | `/v1/marketplaces` | List marketplaces declared for the caller's tenant |
| GET | `/v1/marketplaces/{marketplaceSlug}` | The selected marketplace (`name`, `marketplaceSlug`, `link`) with a server-capped skill list — `name`, `description`, `skillSlug`, `skillName`; accepts `?search=` and `?cursor=` |
| GET | `/v1/marketplaces/{marketplaceSlug}/{skillName}` | Full skill detail — `SKILL.md` body + resource files |

`{marketplaceSlug}` is the `{org}__{repo}` value from the registry. `{skillName}` is the `{nameSlug}` value; the service combines the two into the `{marketplaceSlug}__{nameSlug}` skill slug.

#### `GET /v1/marketplaces`

Returns the marketplaces declared for the caller's tenant, read from the tenant's `skills-mapping.yaml` via the in-memory registry. No Redis or GitHub call.

Response (HTTP 200):

```json
{
  "marketplaces": [
    {
      "name": "skills-marketplace",
      "marketplaceSlug": "clear-route__skills-marketplace",
      "link": "https://github.com/clear-route/skills-marketplace"
    },
    {
      "name": "core-skills",
      "marketplaceSlug": "clearroute__core-skills",
      "link": "https://github.com/clearroute/core-skills"
    }
  ]
}
```

`org` and `repoName` are stripped. `marketplaceSlug` and `link` are returned — the frontend needs `marketplaceSlug` to address subsequent skill list requests and `link` to offer a direct GitHub link.

#### `GET /v1/marketplaces/{marketplaceSlug}`

Query parameters:

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `search` | string | No | Case-insensitive substring match on `name`; omit to return all skills |
| `cursor` | string | No | The previous response's `nextCursor`; omit for the first page |

The server:

1. Resolves `tenantID` from the authenticated caller.
2. Matches `marketplaceSlug` directly against the tenant's registry → `MarketplaceSource { org, repoName }`. If not found, returns `404`.
3. Constructs `marketplace:{tenantID}:{marketplaceSlug}` and reads Redis. On a miss, crawls the repository as described in the request flow, writes `[]SkillEntry` to Redis with the configured TTL, then serves from the same in-memory result.
4. Applies the `search` filter and caps the result at 200 skills.

**Successful response (HTTP 200):**

```json
{
  "name": "skills-marketplace",
  "marketplaceSlug": "clear-route__skills-marketplace",
  "link": "https://github.com/clear-route/skills-marketplace",
  "skills": [
    {
      "name": "React Expert",
      "description": "Senior React developer",
      "skillSlug": "clear-route__skills-marketplace__react-expert",
      "skillName": "react-expert"
    }
  ]
}
```

`Path` and `DocPaths` are always stripped before the response is written; the list item type (`SkillSummary`) exposes `name`, `description`, `skillSlug`, and `skillName`. An empty result returns `{ "skills": [] }` with HTTP 200.

If `marketplaceSlug` is not declared for the caller's tenant, the handler returns `404 Not Found`.

If Redis is unavailable, or if the cache is cold and GitHub is unreachable, the handler returns `503 Service Unavailable`. These are genuine server failures.

#### `GET /v1/marketplaces/{marketplaceSlug}/{skillName}`

Example:

```
GET /v1/marketplaces/clear-route__skills-marketplace/react-expert
```

Response (HTTP 200):

```json
{
  "name": "React Expert",
  "description": "Senior React developer",
  "skillSlug": "clear-route__skills-marketplace__react-expert",
  "skill": {
    "path": "react-expert/SKILL.md",
    "name": "SKILL.md",
    "content": "<complete SKILL.md content>"
  },
  "documents": [
    {
      "path": "react-expert/examples.md",
      "name": "examples.md",
      "content": "<complete file content>"
    }
  ]
}
```

`skill` is always present. `documents` is an empty array if the skill has no adjacent `.md` files. `skillSlug` is returned in the response.

The handler first resolves the tenant's marketplace list from `marketplace:{tenantID}:{marketplaceSlug}` (running the same cache-aside crawl as the list endpoint if needed) and checks that `{marketplaceSlug}__{skillName}` is in it. Only then does it read `{skillSlug}`; on a skill-cache miss it fetches the full file content and caches it under `{skillSlug}`. Because the per-skill key has no tenant prefix, the membership check is what keeps a tenant from reading skills outside its own marketplaces. If the marketplace is not declared for the tenant, or the skill is not in its list, the handler returns `404 Not Found`. `503` is reserved for the same genuine server-failure cases as the list endpoint.

### Status-Code Summary

| Situation | Status |
|---|---|
| Cache hit | 200 |
| Cache miss, GitHub fetch succeeds | 200 |
| Marketplace not declared for caller's tenant | 404 |
| Skill slug not found in the crawled repository | 404 |
| Redis unavailable | 503 |
| Cache miss + GitHub unreachable / rate-limited with no fallback | 503 |

A cache miss on its own is never surfaced as an error. 5xx codes are reserved for genuine server-side failures.

### TTL

| Key | Default TTL | Configuration |
|---|---|---|
| `marketplace:{tenantID}:{marketplaceSlug}` | 12 hours | `MARKETPLACE_CACHE_TTL` |
| `{skillSlug}` | 12 hours | `MARKETPLACE_CACHE_TTL` (same TTL) |
| GitHub App installation token | ~55 minutes | Refreshed 5 min before GitHub's 60-min expiry |

When a key expires, the next request that targets it triggers a fresh crawl and repopulates the key.


### Authorization

All three endpoints require scope `marketplace:read`. Middleware chain: `AuthMiddleware → DelegationMiddleware → ScopeEnforcementMiddleware`. In local development mode (`LOCAL_TENANT_ID` set), the middleware chain is bypassed and the configured tenant ID is used directly. The tenant ID is extracted from the authenticated caller's context and used verbatim as the Redis marketplace key prefix; there is no code path where a client-supplied value can influence which tenant's keys are read.

---

## Consequences

### Positive

- One cache-population code path. The request-time miss path is the only miss path, so there is no risk of a startup crawl and a runtime crawl diverging.
- No startup penalty. The pod is ready as soon as it can accept traffic; cold repositories pay a one-time miss cost when first requested.
- Strict tenant isolation with cheap infrastructure. All tenants share one Redis instance; the tenant ID prefix on marketplace list keys is the boundary, and the fact that `skills-service` is the only reader/writer makes cross-tenant access structurally impossible.
- Per-repo isolation: a failing GitHub fetch for one repository does not affect any other cached repository.
- Two-level cache: list operations read lightweight `SkillEntry` records (no file content); full content is fetched and cached only when a user opens a skill detail page, keeping the marketplace list warm and fast.
- The detail endpoint returns the complete skill content — `SKILL.md` body and all adjacent `.md` resource files — in one response.
- A server-side cap (200 skills per marketplace) keeps list response sizes bounded regardless of dataset size.
- `skillSlug` is stable and URL-safe — the frontend can use it directly as a path parameter without additional encoding.
- GitHub calls go through the standard platform client, so tracing, logging, and metrics work the same way they do for every other service-to-service call.
- Per-source `credentialsRef` allows each marketplace repository to authenticate with its own GitHub App installation, enabling cross-organization marketplaces without sharing credentials.

### Negative

- The first request for a cold repository pays a GitHub round-trip inline. Acceptable because the payload is small and the alternative (startup preload) crawls repositories that may never be requested.
- No cache-stampede protection in V1 — a burst of concurrent cold-cache requests for the same key can cause duplicate GitHub crawls. Acceptable at expected traffic; will be revisited with `singleflight` or a Redis lock if metrics show real duplication.
- Redis is a hard runtime dependency. A Redis outage results in `503` from the list and detail endpoints (the marketplace listing endpoint is unaffected because it reads only from the in-memory registry).
- Two separate Redis TTLs (marketplace list + per-skill) means a key rotation (e.g. forced eviction) requires evicting both shapes. Mitigated by using the same `MARKETPLACE_CACHE_TTL` for both.
- `MarketplaceSource.Name` is currently derived from the repo name (lowercased), not from a human-readable label in the ConfigMap. Richer display names require a future extension to `skills-mapping.yaml`.

---

## Implementation

### Step 1 — Admin Declares Marketplace Sources

A platform operator creates one `SkillMarketplaceSource` CRD per GitHub repository via GitOps. The `orbit-operator` validates each CRD, marks it `Aggregated` or `Invalid`, and renders the approved set into the `orbit-skill-marketplace-sources` ConfigMap as `skills-mapping.yaml`. This is a one-time setup step per repository; the operator keeps the ConfigMap up to date as CRDs are added, changed, or removed.

### Step 2 — Service Starts

When `skills-service` starts:

1. Reads `ORBIT_MARKETPLACE_SOURCES_CONFIG/skills-mapping.yaml` to build the tenant-scoped in-memory registry.
2. Builds a default `TokenProvider` from `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, and `GITHUB_APP_PRIVATE_KEY_PATH`. Also constructs a `TokenProviderPool` backed by this default, for sources that supply their own `credentialsRef`.
3. Constructs the platform GitHub client (`internal/platform/clients/github`) and wires it into the `Crawler` with the pool.
4. Passes the readiness probe. No preload is performed.

### Step 3 — User Opens the Marketplace

1. The frontend calls `GET /v1/marketplaces`.
2. `skills-service` reads the tenant's sources from the in-memory registry and returns `name`, `marketplaceSlug`, and `link` for each. No Redis or GitHub call.
3. The frontend displays the marketplace selector — the dropdown shows names; `marketplaceSlug` is stored for subsequent API calls.

### Step 4 — User Browses Skills

1. The frontend calls `GET /v1/marketplaces/{marketplaceSlug}`.
2. `skills-service` resolves `tenantID` from the authenticated caller.
3. Matches `marketplaceSlug` against the tenant's registry → `MarketplaceSource { org, repoName }`.
4. Constructs the key `marketplace:{tenantID}:{marketplaceSlug}` and reads Redis.
5. On a hit: deserialise `[]SkillEntry`, apply search + server-cap, project to `SkillSummary`, respond `200`.
6. On a miss: crawl the repository via the platform GitHub client, write `[]SkillEntry` back to Redis with the configured TTL, apply search + server-cap, respond `200`.
7. If Redis is unavailable or GitHub is unreachable on a cold-cache miss, respond `503`.

### Step 5 — User Opens a Skill Detail Page

1. The user clicks a skill card; the frontend navigates to `/marketplace/{marketplaceSlug}/{skillName}` and calls `GET /v1/marketplaces/{marketplaceSlug}/{skillName}`.
2. `skills-service` performs the same `tenantID` resolution and `marketplaceSlug` registry validation, and builds `skillSlug = {marketplaceSlug}__{skillName}`.
3. Resolves the marketplace list (cache-aside for `[]SkillEntry` if needed) and finds the `SkillEntry` with that `SkillSlug`; if there is none, responds `404`.
4. Reads Redis for `{skillSlug}`; on a hit, responds `200`.
5. On a miss: fetches `SKILL.md` and all `DocPaths`, caches the `Skill`, responds `200`.

### Service Layout

```
backend/internal/skills/
├── models.go                   — SkillEntry, Skill, SkillFile, SkillSummary, MarketplaceSource types
├── cache.go                    — Redis read/write; two-level key helpers; TTL configuration
├── crawler.go                  — GitHub Trees API walk; SKILL.md + adjacent .md fetch
├── registry.go                 — Reads skills-mapping.yaml; parses GitHub URLs; tenant → sources mapping
├── token.go                    — TokenProvider (single App) + TokenProviderPool (per-source credentialsRef)
├── service.go                  — Cache-aside orchestration; search, server-cap, slug validation
├── handlers_skills_entry.go    — HTTP controllers for marketplaces, list, and detail endpoints
├── formatter.go                — SKILL.md frontmatter parser (ParseSkillFrontmatter)
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

The GitHub HTTP client lives under `internal/platform/clients/github/` because the platform-client convention requires it. The feature package supplies GitHub App semantics (`TokenProvider`, `TokenProviderPool`) but never constructs its own HTTP client.

### API Style

REST is used for V1. The Marketplace has a small, well-defined set of operations and a flat data model — GraphQL provides no meaningful advantage here. GraphQL can be reconsidered if the data model becomes significantly more relational.

### Delivery Phases

**Phase 1 — Configuration and platform client**
Set up `skills-service` skeleton, ConfigMap and Secret mounts, `registry.Registry` loading from `skills-mapping.yaml` with tenant scoping, `TokenProvider` / `TokenProviderPool` with GitHub App token mint and cache, and `internal/platform/clients/github`. Service starts, reads credentials, and can make authenticated calls to the GitHub Trees API and `raw.githubusercontent.com`.

**Phase 2 — Cache-aside read path**
Implement `cache.go`, `crawler.go`, and `service.go`: tenant-scoped Redis key helpers using `{org}__{repo}` marketplace slugs, `SKILL.md` and adjacent `.md` crawl with `{marketplaceSlug}__{nameSlug}` skill slugs, two-level write to Redis on miss, and the cache-aside orchestration used by the handlers.

**Phase 3 — API endpoints**
Implement `GET /v1/marketplaces` (returns name, marketplaceSlug, link), `GET /v1/marketplaces/{marketplaceSlug}` (marketplace with its skill list, search, server-side cap at 200), and `GET /v1/marketplaces/{marketplaceSlug}/{skillName}` (full detail). Wire the `marketplaceSlug` → registry → `MarketplaceSource` lookup and the status-code semantics (`200`/`404`/`503`) as defined above. Wire the BFF proxy. Frontend can populate the selector, browse, and open skills end to end.

**Phase 4 (deferred) — Stampede protection and/or startup preload**
Revisit if metrics show duplicate crawls on cold-cache bursts, or if the first-request-after-expiry latency turns out to be a real user problem. Both are additive to the cache-aside design and require no changes to the API contract.

### Migration Path

No migration is required — `skills-service` is a new service with no existing data to migrate. The `SkillMarketplaceSource` CRD and `orbit-skill-marketplace-sources` ConfigMap are new resources with no prior state.

---

**Last Updated:** 2026-10-13

# Agent Marketplace — Startup Preload, Per-Repo Redis Keys, and Resource-Aware Skills

**Status:** Proposed
**Date:** 2026-09-09
**Deciders:** Platform Engineering, Product Owner
**Technical Story:** Defines the backend architecture for the read-only Agent Marketplace: startup preload into Redis, per-repository Redis keys, a skill model that includes resources directory files, server-side pagination, and an API contract scoped to an org.

---

## Context

### Background

The Orbit Marketplace is a read-only catalogue where users browse, search, and review skills — pre-packaged sets of Claude instructions hosted in approved GitHub repositories. Skills are never installed or executed from this surface.

A `skills-service` owns this capability. GitHub is the source of truth for all Marketplace content.

Marketplace sources are declared through GitOps — one `SkillMarketplaceSource` CRD per GitHub repository. If an org has three skills repos, it declares three CRDs. The `orbit-operator` watches all `SkillMarketplaceSource` CRDs and aggregates all valid entries into one ConfigMap (`orbit-skill-marketplace-sources`). `skills-service` reads that single ConfigMap to know which repos to crawl.

The service authenticates with GitHub using a **GitHub App**. A `TokenProvider` mints short-lived installation tokens on demand and refreshes them before expiry. All content fetches use `githubrepo.Client`, a typed HTTP client against the GitHub Trees API and `raw.githubusercontent.com`, with no Egress Gateway dependency.

All repos for an org write into one shared Redis instance, each under its own key (`marketplace:{org}:{repo}:skills`). Orgs are fully isolated from each other — separate `SkillMarketplaceSource` CRDs and separate Redis instances.

### Problem Statement

- The Marketplace requires server-side search/filtering and pagination.
- A transient failure in one repository's GitHub fetch must not block other repositories from being served.
- Skill detail responses must include the `resources/` directory files that accompany `SKILL.md`.
- List responses must be bounded; the frontend must not hold or process the full dataset.
- The frontend needs a marketplace selector — a list of available marketplaces to display before any skills are shown.
- A missing Redis key (crawl failure) must return a clear error to the frontend, not a silent empty list.

### Goals

- Populate Redis at service startup so the first user request always hits a warm cache.
- Check Redis first at startup; skip the GitHub fetch for any repository already cached.
- Store skills per repository under separate Redis keys within one shared Redis instance per org.
- Include `resources/` directory files in the cached skill record and expose them in the detail API.
- Serve paginated skill lists server-side; the frontend passes `offset` and `limit` and never processes the full dataset.
- Keep both list and detail endpoints reading from the same Redis structure — no separate metadata-only key.
- Expose a marketplace listing endpoint so the frontend can populate the marketplace selector.
- Return HTTP 503 when a Redis key is absent so the frontend can show a clear error instead of a blank list.
- Keep Marketplace content read-only; never execute skill content.

### Non-Goals

- Package installation or execution.
- Agent runtime integration.
- Write-side cache operations — GitHub is the source of truth.
- Webhook-driven cache invalidation — keys expire after 12 hours; re-crawl happens at next pod start or cache miss.
- Marketplace source management from the dashboard — sources are declared via `SkillMarketplaceSource` CRDs, not the UI.

---

## Decision Drivers

* **Time to first skill** — a cold start must not produce a slow first-user experience.
* **Repository fault isolation** — a failing repository must not block the others.
* **Complete skill content** — the detail endpoint must return everything a consumer needs: the `SKILL.md` body and any accompanying resource files.
* **Server-side pagination** — list responses must be bounded; the frontend must not hold or process the full dataset.
* **Per-repository Redis keys** — cache granularity must match the unit of crawl: one repo, one key.
* **GitHub rate limits** — GitHub must not be contacted on every user interaction.
* **No Egress Gateway dependency** — `githubrepo.Client` calls GitHub APIs and `raw.githubusercontent.com` directly.
* **Transparent failure** — a marketplace that failed to load must return a clear error, not a silent empty list.

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

**Description:** `skills-service` fetches all skill metadata at startup and holds it in a process-level in-memory Store. All API requests read from that Store. Full `SKILL.md` content is fetched from GitHub only on skill detail open.

**Pros:**
- No external storage or cache invalidation logic.
- Fast in-memory reads.

**Cons:**
- Store is lost on pod restart — reloaded from GitHub on next boot.
- Each Kubernetes replica holds its own independent Store — no cross-replica consistency.
- Skill detail requests hit GitHub on every click; rate limit risk at scale.
- No isolation between repositories — a failing source blocks or corrupts the entire Store.

**Rejected** — cross-replica inconsistency, per-click GitHub calls on detail, and no per-repo fault isolation make this unsuitable beyond a prototype.

---

### Option C: Background Refresh Worker

**Description:** A separate goroutine wakes on a configurable interval, crawls all configured repositories, and writes to Redis. HTTP handlers always read from Redis.

**Pros:**
- Decouples fetching from serving; workers can retry failed repositories independently.
- Cache is always pre-warmed regardless of request traffic.

**Cons:**
- The interval introduces a stale window that is hard to tune.
- A worker goroutine that panics silently leaves Redis stale with no user-visible error.
- Adds coordination state (last-refresh timestamp, per-repo status).
- A persistent goroutine running indefinitely is harder to reason about than a bounded startup pass.

**Rejected** — a dedicated always-on refresh worker adds coordination state and a hard-to-tune stale window; startup preload achieves the same warm-cache goal with a bounded burst that ends when the pod is ready.

---

### Option D: Startup Preload with Per-Repo Redis Keys

**Description:** At service startup, `skills-service` checks Redis for each configured repository — skipping any that are already warm, crawling GitHub for the rest. All crawls run in parallel goroutines. HTTP handlers always read from Redis; they never contact GitHub at request time.

**Pros:**
- First user request always hits a warm cache.
- Redis check at startup is cheap; already-warm repos add no latency to the startup path.
- Per-repo keys isolate failures — a slow or failing repository does not block others.
- TTL eviction remains the refresh mechanism; no dedicated refresh worker needed.
- Singleflight prevents duplicate crawls within a pod when a fsnotify hot-reload triggers a second preload while the first is still running.
- All replicas share one Redis instance — cross-replica consistency at no extra cost.

**Cons:**
- Service startup is slower on first deploy or after full cache eviction — repositories must be crawled before the pod is ready.

**Chosen.**

---

## Decision

**Chosen Option: D — Startup Preload with Per-Repo Redis Keys**

At startup, `skills-service` reads every source from the ConfigMap and fires one goroutine per repository. Each goroutine checks Redis first — if the key is warm, it skips; if absent or expired, it crawls the repository via the GitHub Trees API, downloads `SKILL.md` and all `resources/` files in parallel, and writes the full `[]Skill` array to Redis. HTTP handlers read from Redis only; there is no runtime fallback to GitHub.

### Skill Data Model

The complete skill record stored in Redis — the same shape is returned by the detail endpoint:

```go
type Skill struct {
    Slug        string      `json:"slug"`
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
```

`Skill` is the `SKILL.md` file — `path`, `name`, and full `content`. `Documents` is the slice of files found under `skills/{skill-name}/resources/`. If no `resources/` directory exists, `Documents` is an empty slice — never `null`. The stored Redis value and the detail API response are the same shape; the list endpoint strips `Skill` and `Documents` at read time.

### Slug Format

```
slug = "{org}/{repo}/{skill-name}"
```

Example: `platform-engineering/skills/react-expert`

`skill-name` is the directory name under `skills/` that contains the `SKILL.md` file. The slug is stable across cache evictions and pod restarts and is the canonical identifier in both APIs.

### Redis Key Structure

One key per repository, all inside one shared Redis instance per org:

```
marketplace:{org}:{repo}:skills   →   []Skill   (TTL: 12h)
```

Example for an org with three repositories:

```text
ONE REDIS INSTANCE (org-a)
│
├── marketplace:platform-engineering:skills        →   []Skill   (TTL: 12h)
├── marketplace:clearroute:core-skills:skills      →   []Skill   (TTL: 12h)
└── marketplace:partner:reference-skills:skills    →   []Skill   (TTL: 12h)
```

Each key holds a JSON-encoded `[]Skill` array. All skills for a repository expire together — there are no per-skill sub-keys. Orgs are fully isolated from each other; each org has its own Redis instance.

### ConfigMap Structure

The `orbit-operator` renders all valid `SkillMarketplaceSource` CRDs into the `orbit-skill-marketplace-sources` ConfigMap as `marketplace-sources.json`:

```json
{
  "sources": [
    { "name": "Platform Engineering", "org": "platform-engineering", "repo": "skills",           "branch": "main" },
    { "name": "ClearRoute Core",      "org": "clearroute",           "repo": "core-skills",      "branch": "main" },
    { "name": "Partner Reference",    "org": "partner",              "repo": "reference-skills", "branch": "main" }
  ]
}
```

`name` is the human-readable label shown in the frontend marketplace selector. `org` and `repo` together identify the GitHub repository and form the Redis key.

`skills-service` mounts `orbit-skill-marketplace-sources` as a read-only file at `ORBIT_MARKETPLACE_SOURCES_CONFIG`, reads `marketplace-sources.json` on startup, and hot-reloads it via `fsnotify`. The backend never imports the CRD type and never calls the Kubernetes API.

### GitHub App Authentication

`TokenProvider` holds the App private key, App ID, and installation ID (from Kubernetes Secret). It mints a short-lived installation token on first use and refreshes it before expiry. `githubrepo.Client` injects the cached token as `Authorization: Bearer` on every call to the GitHub Trees API and `raw.githubusercontent.com`.

### Startup Preload Flow

```text
skills-service starts
   │
   └── for each source in ConfigMap (parallel goroutines):
         │
         ├── Redis GET marketplace:{org}:{repo}:skills
         │     │
         │     ├── HIT (non-expired) → skip; log "cache warm for {org}/{repo}"
         │     │
         │     └── MISS
         │           │
         │           ▼
         │        githubrepo.Client
         │           ├── GET /repos/{org}/{repo}/git/trees/HEAD?recursive=1
         │           │     → filter paths matching skills/*/SKILL.md
         │           │     → filter paths matching skills/*/resources/*
         │           │
         │           └── for each skill directory (parallel, concurrency limit 20 per repo;
         │                 global cap 100 across all repos):
         │                 ├── fetch skills/{name}/SKILL.md          → Skill
         │                 └── fetch skills/{name}/resources/* (all) → Documents
         │           │
         │           ▼
         │        Redis SET marketplace:{org}:{repo}:skills (TTL: 12h)
         │
all goroutines settle
   │
readiness probe satisfied → HTTP server begins accepting traffic
```

If a repository crawl fails (GitHub unreachable, 429, 5xx), the error is logged and that key is left absent. The startup sequence does not fail — other repositories are still preloaded. The pod is marked not-ready only if every repository fails; a partial failure allows the pod to serve the repositories that succeeded.

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

| Method | Path | Description |
|---|---|---|
| GET | `/api/marketplaces` | List all available marketplaces — populates the frontend selector |
| GET | `/api/{org}/skills` | Paginated skill list — metadata only (`slug`, `name`, `description`) |
| GET | `/api/{encodedSkillSlug}` | Full skill detail — `SKILL.md` body + resource files |

#### `GET /api/marketplaces`

Returns the list of available marketplaces, read directly from `marketplace-sources.json`. No Redis or GitHub call.

Response (HTTP 200):

```json
{
  "marketplaces": [
    { "name": "Platform Engineering", "org": "platform-engineering", "repo": "skills" },
    { "name": "ClearRoute Core",      "org": "clearroute",           "repo": "core-skills" },
    { "name": "Partner Reference",    "org": "partner",              "repo": "reference-skills" }
  ]
}
```

Always returns all declared marketplaces regardless of Redis state. This gives the frontend a stable, always-available list for the selector. Whether a marketplace's data is ready is determined at the next step when the frontend requests its skills.

#### `GET /api/{org}/skills`

Query parameters:

| Parameter | Type | Required | Notes |
|---|---|---|---|
| `offset` | integer | No | Zero-based index of the first item; default `0` |
| `limit` | integer | No | Maximum items to return; default `15`, max `100` |
| `search` | string | No | Substring match on `name`; omit to return all skills |
| `repo` | string | **Yes** | Repository name; required to construct the Redis key |

The frontend controls `offset` and `limit`. The server applies the search filter first, then slices `skills[offset : offset+limit]`.

**Redis key missing (crawl failed or TTL expired):**

```json
HTTP 503
{
  "error": "marketplace_unavailable",
  "message": "This marketplace is currently unavailable. Try again later."
}
```

The backend checks `errors.Is(err, redis.Nil)` after the Redis GET. A nil result means the crawl failed or the key expired — the frontend receives a 503 with a structured error body, not a silent empty list. The frontend can display a clear error state (e.g. "This marketplace could not be loaded") instead of an empty skills grid.

**Successful response (HTTP 200):**

```json
{
  "skills": [
    { "slug": "platform-engineering/skills/react-expert", "name": "React Expert", "description": "Senior React developer" }
  ],
  "pagination": {
    "offset": 0,
    "limit": 15,
    "totalItems": 100
  }
}
```

`Skill` and `Documents` are always stripped before the response is written. An empty result returns `{ "skills": [], "pagination": { "offset": 0, "limit": 15, "totalItems": 0 } }` with HTTP 200.

#### `GET /api/{encodedSkillSlug}`

The slug `{org}/{repo}/{skill-name}` is passed as a single percent-encoded path segment — forward slashes encoded as `%2F`:

```
GET /api/platform-engineering%2Fskills%2Freact-expert
```

Response (HTTP 200):

```json
{
  "slug": "platform-engineering/skills/react-expert",
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

`skill` is always present. `documents` is an empty array if the skill has no `resources/` directory. Returns HTTP 503 if the Redis key is absent (same condition as the list endpoint).

### Cache Miss at Request Time

When a user selects a marketplace and the backend reads `marketplace:{org}:{repo}:skills` from Redis, the key may be absent because:

- The startup crawl for that repository failed (GitHub unreachable, 429, 5xx, or a parse error).
- The key expired and the pod has not yet been restarted to re-crawl.

Redis returns `nil` for a missing key — it does not return a 503. The backend translates that nil into a `503 Service Unavailable` response to the frontend:

```json
{
  "code": "marketplace_unavailable",
  "message": "skills for this marketplace are not yet available, try again shortly"
}
```

**Why 503 and not 200 with an empty list:** an empty 200 is indistinguishable from a legitimately zero-skill repository. A 503 signals a recoverable infrastructure state and lets the frontend show a meaningful error (e.g. "This marketplace is temporarily unavailable") rather than an empty skill list with no explanation.

This applies to both `GET /api/{org}/skills` and `GET /api/{encodedSkillSlug}` when the backing repo key is absent. The `GET /api/marketplaces` endpoint reads only from `marketplace-sources.json` and is unaffected.

---

### Singleflight

`golang.org/x/sync/singleflight` is process-local — it deduplicates crawls within a single pod only. If a `fsnotify` hot-reload fires while a preload for a repository is already in progress in the same pod, the second call coalesces into the first rather than starting a duplicate crawl. It does not coordinate across pod replicas; two pods starting simultaneously will each run their own crawl independently. Because all pods write to the same Redis key, the last writer wins, which is safe for an immutable crawl result.

### TTL

| Key | Default TTL | Configuration |
|---|---|---|
| `marketplace:{org}:{repo}:skills` | 12 hours | `MARKETPLACE_CACHE_TTL` |
| GitHub App installation token | 55 minutes | Refreshed 5 min before GitHub's 60-min expiry |

When a key expires, the next startup (pod restart or new deployment) re-crawls the repository and repopulates the key.

### Authorization

All three endpoints require scope `marketplace:read`. Middleware chain: `AuthMiddleware → DelegationMiddleware → ScopeEnforcementMiddleware`.

---

## Consequences

### Positive

- The warm-cache guarantee: every user request after the first pod start hits Redis, not GitHub.
- Per-repo isolation: a failing GitHub fetch for one repository does not affect others.
- The detail endpoint returns the complete skill content — `SKILL.md` body and all `resources/` files — in one response.
- Server-side pagination keeps response sizes bounded regardless of dataset size.
- Startup cost is paid once per repository per TTL window; subsequent pod restarts within the TTL cost only a Redis check.
- All replicas share one Redis instance per org — cross-replica consistency at no extra cost.
- The marketplace listing endpoint reads from the ConfigMap and is always available — it never depends on Redis being warm.
- A missing Redis key returns HTTP 503, not a silent empty list — the frontend can show a meaningful error state.

### Negative

- Service startup is slower on first deploy or after full cache eviction — all repositories must be crawled before the pod is ready.
- An HTTP request that arrives during the startup window receives an empty list. **Mitigation:** the readiness probe gates traffic until startup crawls settle.
- Redis is a hard runtime dependency — a Redis outage makes the Marketplace unavailable.
- Storing `Skill` and `Documents` for every skill increases Redis memory consumption compared to metadata-only storage. **Mitigation:** the full record is needed for detail requests; caching it avoids a second GitHub round-trip per click.
- `GET /api/marketplaces` always shows all declared marketplaces even if some failed to crawl. **Mitigation:** the skills list endpoint returns HTTP 503 for unavailable marketplaces, giving the frontend a clear signal to display an error state.

---

## Implementation

### Step 1 — Admin Declares Marketplace Sources

Before the service can receive any traffic, a platform operator creates one `SkillMarketplaceSource` CRD per GitHub repository via GitOps — specifying the `name`, `org`, `repo`, and `branch` for each. The `orbit-operator` validates each CRD, marks it `Aggregated` or `Invalid`, and renders the approved set into the `orbit-skill-marketplace-sources` ConfigMap. This is a one-time setup step per repository; the operator keeps the ConfigMap up to date as CRDs are added, changed, or removed.

### Step 2 — Service Starts and Preloads Redis

When `skills-service` starts, it does the following in order:

1. Reads `ORBIT_MARKETPLACE_SOURCES_CONFIG/marketplace-sources.json` to get the list of approved repositories.
2. Builds a `TokenProvider` that mints a GitHub App installation token on demand and refreshes it before expiry.
3. Fires one goroutine per repository. Each goroutine checks Redis first — if `marketplace:{org}:{repo}:skills` is already warm, it skips and logs "cache warm for {org}/{repo}". If the key is absent or expired, it proceeds to crawl.
4. For each cold repository: calls the GitHub Trees API recursively, filters for `skills/*/SKILL.md` and `skills/*/resources/*` paths, fetches all matching files in parallel (up to 20 concurrent fetches per repository; a global semaphore caps total concurrent GitHub file fetches across all repositories at 100), and writes the complete `[]Skill` array to Redis with a 12-hour TTL.
5. Once all goroutines have settled, the readiness probe passes and HTTP traffic begins.

If any individual repository crawl fails after retries, it is logged and that repository's key remains absent. The startup sequence does not fail — the pod becomes ready on all repositories that succeeded.

### Step 3 — User Opens the Marketplace

When a user navigates to the Skills page:

1. The frontend calls `GET /api/marketplaces`.
2. `skills-service` reads `marketplace-sources.json` and returns the list of all declared marketplaces — `name`, `org`, `repo` for each. No Redis call.
3. The frontend displays the marketplace selector dropdown. The user picks one.

### Step 4 — User Browses Skills

After selecting a marketplace:

1. The frontend calls `GET /api/{org}/skills?repo={repo}&offset=0&limit=15` with the `org` and `repo` from the marketplaces response, plus optional `search`.
2. `skills-service` reads `marketplace:{org}:{repo}:skills` from Redis.
3. If the key is absent (`redis.Nil`), returns HTTP 503 with `{ "error": "marketplace_unavailable" }`.
4. If present: applies case-insensitive substring search on `name` if `search` is provided, slices the result by `offset` and `limit`, strips `Skill` and `Documents`, and returns the page.

No GitHub call is made during this flow.

### Step 5 — User Opens a Skill Detail Page

When a user clicks a skill to read its full description:

1. The frontend calls `GET /api/{encodedSkillSlug}`, where the slug is URL-encoded as a single path segment.
2. `skills-service` URL-decodes the slug, reads `marketplace:{org}:{repo}:skills` from Redis, and finds the matching element.
3. If the key is absent, returns HTTP 503.
4. Returns the full record — `skill` (`SKILL.md` as `path`, `name`, `content`) and `documents` (`[]SkillFile`, or `[]` if no `resources/` directory).

No GitHub call is made during this flow.

### Service Layout

Follows `backend/internal/{feature}` package structure:

```
backend/internal/skills/
├── models.go        — Skill, SkillFile domain types
├── cache.go         — Redis read/write; key helpers; TTL constants
├── crawler.go       — GitHub Trees API walk; SKILL.md + resources fetch; singleflight guard
├── registry.go      — Reads marketplace-sources.json; hot-reload via fsnotify;
│                       org → repo mapping; TokenProvider
├── token.go         — TokenProvider: GitHub App token mint and refresh
├── github.go        — githubrepo.Client: Trees API + raw content fetch
├── preload.go       — Startup preload: per-repo goroutines; readiness gate
├── service.go       — Pagination, search, field projection
└── handlers.go      — HTTP controllers for marketplaces, list, and detail endpoints

backend/internal/operator/
└── marketplace_aggregator.go  — MarketplaceSourceAggregator: watches SkillMarketplaceSource CRDs;
                                  validates sources; marks Pending/Aggregated/Invalid;
                                  renders orbit-skill-marketplace-sources ConfigMap
```

### API Style

REST is used for V1. The Marketplace has a small, well-defined set of operations and a flat data model — GraphQL provides no meaningful advantage here. GraphQL can be reconsidered if the data model becomes significantly more relational.

### Delivery Phases

**Phase 1 — Configuration and startup**
Set up `skills-service` skeleton, ConfigMap and Secret mounts, `registry.Registry` loading from `marketplace-sources.json`, `TokenProvider` with GitHub App token mint and cache, and `githubrepo.Client`. Service starts, reads credentials, and can make authenticated calls to the GitHub Trees API and `raw.githubusercontent.com`.

**Phase 2 — Startup preload**
Implement `preload.go`: per-repo goroutine fan-out, Redis HIT skip, `SKILL.md` and `resources/` crawl, `[]Skill` write to Redis, `singleflight` guard, and readiness gate. Service fully preloads Redis on startup.

**Phase 3 — API endpoints**
Implement `GET /api/marketplaces`, `GET /api/{org}/skills` (list, search, offset/limit pagination, HTTP 503 on missing key), and `GET /api/{encodedSkillSlug}` (full detail, HTTP 503 on missing key). Wire the BFF proxy. Frontend can populate the selector, browse, and open skills end to end.

### Migration Path

No migration is required — `skills-service` is a new service with no existing data to migrate. The `SkillMarketplaceSource` CRD and `orbit-skill-marketplace-sources` ConfigMap are new resources with no prior state.

---

**Last Updated:** 2026-09-09

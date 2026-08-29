# Agent Marketplace — GitHub App Direct Fetch with Singleton In-Memory Store

**Status:** Proposed
**Date:** 2026-08-27
**Deciders:** Platform Engineering, Product Owner
**Technical Story:** Users need a read-only Marketplace to browse, search, filter, and review skills from approved GitHub repositories. This ADR establishes the V1 backend architecture: GitHub App authentication, a singleton in-memory Store, and server-side search/filter/sort/pagination with no PostgreSQL or Redis dependency.

---

## Context

### Background

The Orbit Marketplace is a browseable catalogue inside the platform where users can discover, search, and review skills — pre-packaged sets of Claude instructions hosted in an approved GitHub repository. A user opens the Marketplace, types a search term or picks a category, browses paginated results, and clicks into a skill to read its full description before deciding to use it. The content is read-only; skills are never installed or executed from this surface.

A new `skills-service` will own this capability. GitHub is the source of truth for all Marketplace content; the service must not introduce unnecessary infrastructure in V1.

The service authenticates with GitHub using a **GitHub App** — the App private key, App ID, and installation ID are injected via Kubernetes Secrets. A `TokenProvider` mints short-lived installation tokens on demand and refreshes them before expiry. All content fetches use `githubrepo.Client`, a typed HTTP client against `raw.githubusercontent.com`, with no Egress Gateway dependency.

### Problem Statement

The Marketplace requires server-side search, filtering, sorting, and pagination. Fetching `marketplace.json` on every request would generate excessive GitHub API traffic. A background sync worker or database would add operational complexity not justified by a read-only V1 workload.

### Goals

- Authenticate with GitHub using a GitHub App — credentials supplied via Kubernetes Secrets
- Load `/.claude-plugin/marketplace.json` once at startup and hold metadata in a singleton in-memory Store
- Serve all search, filter, sort, and pagination requests from the Store with no GitHub call
- Fetch full `SKILL.md` content from GitHub only on skill detail open
- Return only the requested page to the frontend — the frontend never holds the full dataset
- Keep Marketplace content read-only; never execute package content

### Non-Goals

- Automatic background refresh or GitHub webhook integration
- PostgreSQL storage or Redis caching
- Package installation or execution
- Agent runtime integration
- Marketplace source management from the dashboard (sources are declared in `sources.json`, not the UI)

---

## Decision Drivers

* Simplicity — V1 must not introduce PostgreSQL, Redis, or a background sync worker
* Performance — search, filter, sort, and pagination must be fast in-memory operations
* GitHub rate limits — GitHub must not be contacted on every user interaction
* Read-heavy workload — Marketplace metadata changes infrequently; an in-memory representation is appropriate
* Frontend simplicity — the frontend sends queries and displays pages; it must not hold or process the full dataset
* No Egress Gateway dependency — `githubrepo.Client` calls `raw.githubusercontent.com` directly

---

## Considered Options

### Option A: PostgreSQL + Redis + Background Sync

**Description:** A worker periodically fetches `marketplace.json`, stores metadata in PostgreSQL, and caches reads in Redis.

**Pros:**
- Persistent data; survives service restarts
- Indexed searches
- Redis reduces database read pressure

**Cons:**
- Adds PostgreSQL, Redis, and a synchronisation worker
- Requires cache invalidation logic and synchronisation state
- Significant operational overhead for a read-only V1 feature

### Option B: PostgreSQL Only

**Description:** A synchronisation worker fetches GitHub data periodically and stores it in PostgreSQL.

**Pros:**
- Persistent storage with structured queries and indexes

**Cons:**
- Requires a synchronisation worker and schema migrations
- Data becomes stale between syncs
- More infrastructure than V1 requires

### Option C: Fetch GitHub on Every Request

**Description:** Every search, filter, sort, and pagination request fetches `marketplace.json` directly from GitHub.

**Pros:**
- Always returns current data; no local storage

**Cons:**
- Every user interaction generates GitHub API traffic
- High latency; unnecessary load on GitHub rate limits

### Option D: Singleton In-Memory Store

**Description:** `skills-service` fetches `marketplace.json` at startup and stores parsed metadata in a single `StoreHolder`. All API requests read from that Store. The full `SKILL.md` content is fetched from GitHub only when a user opens a skill detail page.

**Pros:**
- No external storage, sync worker, or cache invalidation logic
- Very fast in-memory reads; GitHub not contacted for search/filter/paginate
- `StoreHolder` with `sync.RWMutex` ensures safe concurrent reads at startup and during normal operation

**Cons:**
- Store is lost on pod restart — reloaded from GitHub on next boot
- Each Kubernetes replica holds its own independent Store
- Metadata does not update while the process is running; a pod restart is required to pick up new content

---

## Decision

**Chosen Option: D — Singleton In-Memory Store**

`skills-service` maintains exactly one `Store` instance per process, loaded at startup from each configured `marketplace.json`. `GetStore()` — reads are fully concurrent and no request ever sees an empty or partially built Store.

### Store Model

```
StoreHolder (package-level singleton)
└── Store
     └── Skills []Skill
          ├── slug
          ├── name
          ├── description
          ├── category
          └── source
               └── path   (skill detail fetch only)  
```

### GitHub App Authentication

`TokenProvider` holds the App private key, App ID, and installation ID (from Kubernetes Secret). It mints a short-lived installation token on first use and refreshes it 60 minutes before expiry. `githubrepo.Client` injects the cached token as `Authorization: Bearer` on every call to `raw.githubusercontent.com`.

### Startup Flow

1. Read `SOURCES_FILE` (ConfigMap) → `registry.Registry` of source repos
2. Initialise `TokenProvider` from Secret values
3. For each source: fetch `/.claude-plugin/marketplace.json` via `githubrepo.Client`
4. For each skill path: fetch `SKILL.md`, parse YAML frontmatter (`name`, `description`, `category`)
5. Build `Store` → call `SwapStore`

### API Surface

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/skills` | List, search (`?search=`), filter (`?category=`), sort, paginate (`?page=`) — all from Store |
| GET | `/api/v1/skills/{slug}` | Metadata from Store; full `SKILL.md` fetched from GitHub on demand |

Page size is fixed at 10 server-side. Next is disabled when the returned page contains fewer than 10 skills.

### Request Flows

**List / Search / Filter / Paginate — no GitHub call:**
```
Frontend → BFF → skills-service → GetStore() → search → filter → sort → paginate → 10 results
```

**Skill detail — GitHub call:**
```
Frontend → BFF → skills-service → GetStore() (source.path) → githubrepo.Client → SKILL.md → Frontend
```

Option D delivers all required Marketplace capabilities with no additional infrastructure. The dataset is small, read-heavy, and changes infrequently — an in-memory representation is appropriate.

---

## Consequences

### Positive

- No PostgreSQL, Redis, or background sync worker — minimal operational footprint
- Search, filter, sort, and pagination are fast in-memory operations
- GitHub is contacted only at startup and on skill detail opens
- Token minted on demand and cached — no static credentials that expire silently
- No Egress Gateway dependency

### Negative

- Store is lost on pod restart and must be reloaded from GitHub
- Each Kubernetes replica holds an independent Store — no cross-replica consistency
- Marketplace metadata does not update while the process is running; a pod restart is required to pick up new content
- Initial load depends on GitHub availability

### Neutral

- Horizontal scaling means each pod has its own Store — acceptable for a read-only V1 workload

---

## Implementation

### Step 1 — Admin Configures the Service

Before the service can run, an administrator provides two pieces of configuration:

- A **sources file** that lists which GitHub repositories contain Marketplace content. This file is mounted into the service via a Kubernetes ConfigMap.
- **GitHub App credentials** (App ID, Installation ID, and a private key) that allow the service to authenticate with GitHub. These are stored securely in a Kubernetes Secret and injected into the service at startup.

The administrator does this once. The service reads both at boot time.

### Step 2 — Service Starts and Loads the Marketplace

When the `skills-service` process starts, it does the following in order:

1. Reads the sources file to get the list of approved GitHub repositories.
2. Uses the GitHub App credentials to request a short-lived access token from GitHub. This token is cached so the service does not need to request a new one for every call.
3. For each repository in the sources file, fetches the `marketplace.json` file hosted at a fixed path inside that repository. This file lists all the available skills and where their content lives.
4. For each skill listed in `marketplace.json`, fetches that skill's metadata file (`SKILL.md`) and reads the name, description, and category from the top of the file.
5. Stores all of this metadata in a single shared in-memory structure called the **Store**. The Store lives for as long as the service process is running.

By the time the service is ready to accept requests, the Store is already populated. No GitHub call is needed for any search or browse action.

### Step 3 — User Browses the Marketplace

When a user opens the Marketplace in the frontend and types a search term or selects a category filter:

1. The frontend sends a request to the backend with the search term, filter, and page number.
2. The backend reads from the Store entirely in memory — no database, no GitHub call.
3. It applies the search (matching skill names and descriptions), the category filter, and sorts the results.
4. It returns exactly 10 skills for the requested page. The frontend never receives the full list.
5. The frontend displays the 10 skill cards and shows Previous / Next buttons. Previous is hidden on page 1; Next is hidden when fewer than 10 results come back (meaning the last page has been reached).

A 300 ms delay is applied on the frontend before sending a search request while the user is still typing, so the backend only receives one request per completed search rather than one per keystroke.

### Step 4 — User Opens a Skill Detail Page

When a user clicks on a skill to read its full description:

1. The backend looks up that skill in the Store to get the file path for its full content on GitHub.
2. It uses the cached GitHub access token to fetch the complete `SKILL.md` file directly from GitHub.
3. The full content is returned to the frontend and displayed to the user.

This is the only moment a GitHub call happens during normal user interaction. The token refresh is handled transparently — if the cached token is close to expiry, a new one is requested before the fetch.

### Service Layout

Follows `backend/internal/{feature}` package structure:

```
backend/internal/skills/
├── models.go       — Skill, Store domain types
├── store.go        — StoreHolder, GetStore
├── loader.go       — marketplace.json fetch, SKILL.md frontmatter parsing
├── registry.go     — sources.json → registry.Registry
├── token.go        — TokenProvider (GitHub App token mint and cache)
├── github.go       — githubrepo.Client
├── service.go      — search, filter, sort, paginate logic
└── handlers.go     — HTTP controllers
```

Follows all existing platform conventions: controller-based HTTP routing, YAML config via `config.Load[T]()`, `*logging.Logger` injection, OTel traces and structured logs on all operations.

### API Style

REST is used for V1. The Marketplace has a small, well-defined set of operations and a flat data model — GraphQL's flexible field selection and nested query capabilities provide no meaningful advantage here. GraphQL can be reconsidered if the data model becomes significantly more relational.

### Delivery Phases

**Phase 1 — Configuration and startup**
Set up the `skills-service` skeleton, ConfigMap and Secret mounts, `registry.Registry` loading from `sources.json`, `TokenProvider` with GitHub App token mint and cache, and `githubrepo.Client`. Service starts, reads credentials, and can make authenticated calls to `raw.githubusercontent.com`.

**Phase 2 — Store population**
Implement `marketplace.json` fetch, per-skill `SKILL.md` frontmatter parsing, `Store` and `StoreHolder` with `GetStore` / `SwapStore`. Service loads the full Marketplace metadata into memory on startup.

**Phase 3 — API endpoints**
Implement the list/search/filter/sort/paginate endpoint (`GET /api/v1/skills`) and the skill detail endpoint (`GET /api/v1/skills/{slug}`). Wire the BFF proxy. Frontend can browse and open skills end to end.

**Phase 4 — Observability and hardening**
Add OTel traces, structured logs, and all metrics listed in the Monitoring section. Run the race detector and load tests against the success metric targets. Fix any issues before shipping.

### Migration Path

No migration is required — `skills-service` is a new service with no existing data to migrate.

---

## Validation

### Success Metrics

- Startup load of 10,000 skills completes in under 2 seconds
- p50 response time for search/filter/paginate under 20 ms; p95 under 100 ms; p99 under 200 ms
- Error rate under 0.1% at steady-state load
- Zero data races detected under 1,000 concurrent requests (`go test -race`)
- Memory usage benchmarked at 1K, 5K, 10K, and 25K skills before shipping

### Monitoring

| Signal | Why it matters |
|---|---|
| Request count and latency (p50/p95/p99) by endpoint and status code | Baseline traffic visibility and latency regression detection |
| Store item count (alert if zero after startup) | Catches a failed or empty load before users see an empty Marketplace |
| Startup duration | Flags slow GitHub fetches or large `marketplace.json` files at boot time |
| GitHub fetch success/failure rate | Detects GitHub connectivity issues or token problems during startup and detail fetches |
| Token mint success/failure rate | Detects GitHub App credential misconfiguration (expired key, wrong App ID) |
| Active concurrent requests | Baseline for capacity planning |

---

## References

- `/.claude-plugin/marketplace.json` — the GitHub-hosted index file consumed by `skills-service`; declares all skill paths within a repository
- `SKILL.md` — per-skill documentation file; YAML frontmatter provides `name`, `description`, and `category`; full body fetched only on skill detail open
- `githubrepo.Client` — typed HTTP client for `raw.githubusercontent.com`; the only GitHub transport used by this service
- `TokenProvider` — mints and caches GitHub App installation tokens; the authentication layer for all GitHub calls
- `sources.json` — operator-managed registry of approved GitHub repositories; mounted via ConfigMap at `SOURCES_FILE`
- `backend/internal/skills/` — Go package that will own all Marketplace logic (Store, loader, handlers, token, registry)
- [GitHub Apps documentation](https://docs.github.com/en/apps) — GitHub App installation token flow used by `TokenProvider`

---

## Notes

The singleton Store does not mean one Marketplace — it means one shared Store instance per service process. The Store holds metadata from all configured sources but is not the source of truth; GitHub is. It does not persist across pod restarts, synchronise across replicas, or replace the need for GitHub availability at startup.

The Store is intentionally scoped to V1. If the Marketplace grows significantly in size or data freshness requirements tighten, the Store can be replaced with a PostgreSQL-backed model without changing the API surface or the frontend.


---

Last Updated : 2026-08-28


# GitHub Connection — Category-Scoped Actions, Repository Qualifiers, and Down-Scoped Token Minting

<!-- Filename: 2026-07-22-github-connection-category-scoped-actions.md -->

**Status:** Accepted
**Date:** 2026-07-22
**Deciders:** Platform Engineering
**Technical Story:** Extends the `github_app` adapter from the [JIT Credential Brokering ADR](2026-06-06-jit-credential-brokering-egress-gateway.md) beyond its coarse `repo:read`/`repo:write` pair, following the resource-category precedent set by the [Kubernetes adapter's Amendment 1](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-1--2026-07-06-resource-category-verb-taxonomy).

---

## Context

### Background

The `github_app` adapter (`internal/credentials/adapters/githubapp/`) was the credential broker's first connection type and shipped with the simplest possible action taxonomy: every git smart-HTTP or REST call classifies into exactly `repo:read` or `repo:write`, derived from the git service name or the HTTP verb alone (`ClassifyAction` → `classifyGitSmartHTTP` / `classifyREST`). The request **path** is never inspected. A `connection:<name>:repo:write` grant therefore authorizes *every* mutating call GitHub accepts with the connection's installation token — pushing commits, commenting on issues, deleting webhooks, editing branch protection, and writing Actions secrets are all the same action to kora's policy layer.

The Kubernetes adapter started from the same coarse pair (`cluster:read`/`cluster:write`) and corrected it in [Amendment 1](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md#amendment-1--2026-07-06-resource-category-verb-taxonomy): `ClassifyAction` now parses the API request path into a **resource category** combined with a read/write verb class, with Secrets deliberately split from ConfigMaps so an admin can grant the routine without the sensitive. GitHub has exactly the same shape of problem — "can open pull requests" and "can rewrite branch protection" are different trust levels — and now has a proven in-house pattern to follow.

GitHub also offers something Kubernetes does not: the credential itself is scoped **at mint time**. `Resolve` already exchanges the App JWT for an installation token via `POST /app/installations/{id}/access_tokens`, whose body accepts a `permissions` map (`InstallationTokenRequest.Permissions`, `internal/platform/clients/github/`). Today that map is populated only from the connection's static config; the classified action is available on `CallerIdentity.Action` but unused during minting. This ADR uses it.

**Which of the three questions this touches:** *What can it access?* — the grantable vocabulary and the minted credential's blast radius both narrow. *Who is this agent?* and *How does it connect?* are unchanged: identity flow, the broker's authorization sequence, and the credential-never-enters-the-sandbox guarantee are untouched.

### Problem Statement

A kora admin cannot express any GitHub authorization narrower than "all reads" / "all writes". Concretely:

1. A triage agent that should only read and comment on issues must be granted `repo:write`, which also lets it push code and delete webhooks.
2. A CI-status agent that should only read check runs must be granted `repo:read`, which also lets it read the entire repository's contents.
3. The minted installation token always carries the connection's full configured permission set regardless of what the request needs, so the token is over-privileged even when the policy decision was narrow.
4. Repository reach is a connection property (`config.repositories`), not a grant property — every agent attached to the connection sees the same repositories. An admin cannot give one agent the whole installation and another agent two repositories; narrowing one agent means editing shared connection state that affects all of them.

### Goals

- Enumerate a category-scoped action vocabulary for `github_app` that covers the surfaces agents actually use (code, issues, pull requests, Actions, checks, repo administration), at a "decent level of control" — deliberately **not** a 1:1 mirror of GitHub's ~40 fine-grained App permissions.
- Move repository scoping from connection config to **grant time**: attaching the connector to an agent selects which repositories that agent may touch, with "all repositories" expressible as a single scope via the platform's `covers` hierarchy — never a wildcard, never an enumeration of every repo.
- Enforce categories at both layers the architecture already has: the policy check in `Broker.Authorize` (edge, fail-closed) **and** the permission set on the minted installation token (upstream authority, defense in depth).
- Preserve every existing grant's behavior across the upgrade: a principal holding `connection:<name>:repo:read` / `repo:write` today must lose nothing.
- Zero changes to the broker, the CRD schema, the vocabulary publisher's mechanics, or the frontend — the registry-metadata seam (`internal/platform/connections`) must carry the whole change, as it did for Kubernetes' 14 actions.

### Non-Goals

- Organization-level administration (`/orgs/*` membership, org webhooks, org secrets). The `Connection` model is repository-centric; org surfaces stay in the catch-all until a real need is scoped.
- GraphQL query parsing. `/graphql` remains conservatively classified as a write (unchanged from today).
- Defining the attachment UI — not because it is deferred, but because it is owned by the [Connection Scope Qualifiers ADR](2026-07-22-connection-scope-qualifiers.md)'s Surfaces section (the `ConnectionCards` resource-scope extension), which this ADR's repository instance renders through unchanged.
- Every remaining REST family (deployments, pages, packages, discussions, search, code scanning, …). Each is a one-line future addition to the category table, same as adding a resource to the Kubernetes map.
- Changing what the MCP tool gateway or any GitHub *tool* exposes — this ADR is about the egress credential scope vocabulary only.

---

## Decision Drivers

- **Proven precedent over novelty.** Kubernetes Amendment 1 already established category:verb actions, path-parsing classification, a sensitive-category split, and a catch-all bucket. Diverge only where GitHub is genuinely different, and say why.
- **Least privilege at the credential, not only at the gate.** GitHub is the rare upstream that lets the broker mint a credential matching the authorized action. A policy deny at the gateway is necessary; a token that *couldn't* have done more is strictly better.
- **No breaking migration.** Connections and grants are GitOps/admin-managed state in the field; an upgrade must not strand a single working agent.
- **Registration, not code.** The vocabulary, the connector picker, auto-egress derivation, and grant UI must all pick the new actions up from the one metadata table, exactly as they did for Kubernetes.
- **Hierarchy is `covers`, never `*`.** kora scopes parse as `<resource_type>:<path…>:<action>`, and breadth is expressed by the `scope_covers` prefix rule (`authz.rego` / `authz_gateway.rego`, mirrored in `policyservice/api/service.go`): a shorter path covers every longer path beneath it, for the same resource type and action. There is no wildcard matching in the policy layer at all — the `connection:<name>:*` entry the egress gateway publishes today matches nothing under `scope_covers` (its action segment `*` never equals `read`/`write`) and is dead vocabulary. Scope shapes in this ADR must therefore put dimensions in the path so that omitting a segment *is* the broad grant.

---

## Considered Options

### Decision A — Action taxonomy shape

#### Option 1: Category:verb actions over GitHub's route families (Chosen)

**Description:** Mirror the Kubernetes correction: `ClassifyAction` parses the request path (and host family) into a category — `contents`, `issues`, `pulls`, `actions`, `checks`, `admin`, `metadata` — combined with `:read`/`:write` from the verb. Roughly one category per GitHub App permission *group agents actually exercise*, not per permission.

**Pros:**
- Same mental model, docs shape, and code shape as the Kubernetes adapter — a reviewer who understands `k8sbearer.categorizeRequest` understands this.
- Seven categories cover the observed agent workload (clone/push, PR lifecycle, issue triage, CI interaction, admin) while staying small enough for a human to reason about in the grants UI.
- Each category maps cleanly onto GitHub App installation-token permissions, which Decision D exploits.

**Cons:**
- An incomplete enumeration by construction — GitHub's REST surface is far larger than the table. Mitigated by Decision C's catch-all rather than Kubernetes' deny (see there for why the treatments differ).

#### Option 2: Full mirror of GitHub's fine-grained App permissions

**Description:** One kora action per GitHub App permission (`administration`, `checks`, `contents`, `deployments`, `issues`, `metadata`, `packages`, `pages`, `pull_requests`, `repository_hooks`, `secrets`, `statuses`, `workflows`, …), each with GitHub's own read/write/admin levels.

**Why not chosen:** ~40 grantable scopes per connection swamps the picker and the grants UI for no demonstrated need; GitHub's own permission taxonomy shifts (new permissions appear regularly), turning the mirror into a treadmill; and several permissions have no classifiable REST route family of their own (e.g. `single_file`). The user-facing requirement is explicitly "a decent level of control", not parity.

#### Option 3: Keep the coarse pair, split out only `admin`

**Description:** Three-way: `repo:read`, `repo:write`, `admin:write` — the minimal "sensitive split", analogous to the rejected `cluster:delete` third tier from the Kubernetes ADR's original Decision B.

**Why not chosen:** For the same reason the Kubernetes ADR's Amendment 1 rejected verb-tier splitting: it doesn't express the real grants admins want ("issues but not code", "checks but not pushes"). Real agent role shapes split by *surface*, not by a single sensitivity bit.

### Decision B — Backwards compatibility of `repo:read` / `repo:write`

#### Option 1: Legacy bridge via vocabulary `requires`; go-forward umbrella via `covers` (Chosen)

**Description:** Under the scope grammar, `connection:<n>:repo:read` parses as path `[<n>, repo]` — a *sibling* of the new category scopes (`[<n>, issues]`, …), not their parent, so `covers` alone cannot make an existing grant reach the categorized surface. The bridge: `repo:read`/`repo:write` stay in the vocabulary as the catch-all category, and their published `ScopeDeclaration`s carry `Requires` edges to every same-connection category action of the matching verb class. The bundle compiler already expands `requires` transitively into the effective scope set for grants **and** roles (`expandRequires`, `internal/policyservice/bundle/compiler.go`), and the vocabulary upsert already preserves non-empty `requires` on re-registration. Going forward, the idiomatic "everything on this connection" grant is not `repo:*` at all — it is the covering parents `connection:<n>:read` / `connection:<n>:write` (path `[<n>]`), which mechanically cover every category and every repository qualifier beneath them. Both are published as declaration-only vocabulary entries; the dead `connection:<n>:*` wildcard entry is dropped.

**Pros:**
- Zero stored-grant migration. Every existing `repo:read`/`repo:write` grant expands to the full category set at the next bundle compile — behaviorally identical to today.
- The go-forward umbrella needs no expansion machinery at all: `connection:<n>:read`/`write` cover the whole subtree by the prefix rule, including scopes that don't exist yet (future categories, any repository qualifier).
- Reuses the exact mechanism the MCP tool gateway already ships for scope dependency (`orbit/requires`) for the legacy bridge, and the platform's one hierarchy mechanism (`covers`) for breadth — no wildcard semantics invented anywhere.

**Cons:**
- Two umbrella spellings coexist during the deprecation window: legacy `repo:read`/`repo:write` (requires-expanded) and the covering parents. The vocabulary description strings must state that `repo:*` is the *catch-all category plus legacy bridge*, and new grants should prefer the parents.

#### Option 2: Hard migration — rewrite stored grants to the category set and delete the pair

**Why not chosen:** Requires a data migration across every tenant's grants plus coordinated GitOps changes for any declaratively-managed grants, to reach the same effective state Option 1 reaches with none. Also removes the legitimately useful "everything" grant.

#### Option 3: Keep `repo:*` matching everything inside the classifier (categories as aliases only)

**Description:** Classifier still returns `repo:read`/`repo:write`; the policy layer treats a category grant as satisfying the coarse action.

**Why not chosen:** Inverts the enforcement: the audit log would record every call as `repo:*`, losing exactly the per-surface attribution this ADR exists to create, and the minted token (Decision D) could not be narrowed because the broker never learns the category.

### Decision C — Requests outside the category table

#### Option 1: Verb-classified catch-all into `repo:read` / `repo:write` (Chosen)

**Description:** An API path not matched by the category table (deployments, packages, search, GraphQL, …) classifies exactly as today: verb → `repo:read` or `repo:write`. Only a principal holding the catch-all — directly, or via a covering parent (`connection:<name>:read`/`write`) — is authorized for it.

**Pros:**
- Upgrade-safe: agents doing uncategorized-but-legitimate calls today under `repo:*` grants keep working untouched.
- Still least-privilege for category-granted agents: a principal holding only `issues:write` is **denied** on an uncategorized write, because the catch-all action is not in its effective set.
- Mirrors the spirit of Kubernetes' `cluster` catch-all while acknowledging the different starting point (see below).

**Cons:**
- The umbrella grant's blast radius includes surfaces this ADR never reviewed — but that is precisely today's shipped semantics, now opt-in rather than the only option.

#### Option 2: Deny outright, Kubernetes-style (`ErrUnsupportedAction`)

**Why not chosen — and why the divergence from the k8s precedent is justified:** Kubernetes could deny unknown resources because the adapter was days old with no installed base, and its resource universe is small and stable enough to enumerate. `github_app` has been the production adapter since 2026-06; existing agents exercise uncategorized routes under `repo:*` grants **today**, and GitHub's REST surface is orders of magnitude larger and faster-moving than the Kubernetes resource set. Denying unknowns would convert this upgrade into a breaking change gated on a complete enumeration this ADR explicitly declines to attempt. The fail-closed property Kubernetes bought with denial is bought here at the grant layer instead: unknown surface requires the umbrella, which an admin grants knowingly.

### Decision D — Enforcement depth

#### Option 1: Policy check plus permission-down-scoped token minting (Chosen)

**Description:** Two layers. (1) `Broker.Authorize` evaluates `connection:<name>:<category>:<verb>` exactly as it evaluates every connection action today — no broker changes. (2) `githubapp.Resolve` derives the installation-token `permissions` map from `CallerIdentity.Action` via a static category→permission table, intersected with the connection's configured `permissions` ceiling when one is set, and mints (and caches) **per-category tokens**.

**Pros:**
- Defense in depth with the upstream as authority: a token minted for `issues:write` cannot read code or touch webhooks *even if presented outside the gateway*, because GitHub itself rejects it. This is the "defer to the upstream system as the authority" principle applied to the one adapter whose upstream supports it.
- Nearly free: `Resolve` already receives the action, already sends a `permissions` map, and the token cache was explicitly designed for this ("scoped … to the exact repo/permission set requested, so a narrower request never reuses a broader token" — `cacheKey`'s own doc).

**Cons:**
- More distinct cached tokens per connection (bounded by category count × connections; the existing sweep threshold already covers it).
- One extra mint round-trip the first time each category is exercised per connection (amortized by the existing cache).

#### Option 2: Policy check only; keep minting the connection's full permission set

**Why not chosen:** Leaves the minted credential over-privileged relative to the decision that authorized it, for no saving — the delta is a small static table in the adapter. It would also make the category vocabulary purely advisory at the credential layer, which is the weaker half of "what can it access".

### Decision E — Repository scoping via the connection scope qualifier pattern

Repository scoping moves from connection config to a **grant-time qualifier**, per the companion [Connection Scope Qualifiers ADR](2026-07-22-connection-scope-qualifiers.md), which owns the generic decisions: the qualifier path segment (vs. grant conditions or vocabulary enumeration), the unbounded-dimension-deepest ordering, the broker-enforced config ceiling, and the adapter seams for declaration/extraction/enumeration. This ADR is that pattern's first instance; GitHub-specific choices:

- **The qualifier is the bare repository name, not `owner/repo`.** A GitHub App installation is bound to exactly one account, so the owner is a property of the connection — and the bare name is precisely what `InstallationTokenRequest.Repositories` takes at mint time. Names are lowercased (GitHub treats them case-insensitively). Two accounts are two installations and therefore two connections, so the connection segment — not the qualifier — is the owner disambiguator (`connection:github-orbital:issues:orbit:read` vs `connection:github-clearroute:issues:orbit:read`).
- **The owner is verified, never dropped.** The connection declares its account (`owner` in config, cross-checked against the installation), and the classifier denies any path whose owner segment does not match (`ErrUnsupportedAction`) instead of extracting the bare name. Without this, a request for a *same-named repository under a foreign owner* would match the qualifier and pass the gate — and while GitHub's token rejects foreign writes and private reads, it happily serves foreign **public** repos, so name collision would quietly widen a repo-scoped read grant. Fail closed, same treatment as `kubectl exec` in the Kubernetes adapter.
- **Attachment selections compile to qualified category scopes**; "all repositories" compiles to the unqualified category scope, which covers every qualified form by prefix — including repositories created after the grant.
- **Mint-time narrowing is real for GitHub** (unlike the Kubernetes instance): the token carries the request's repository, so the credential bounds the resource even outside kora's gate.
- **Repo-less endpoints** (`/graphql`, `/search/*`, `/user`, `/rate_limit`) follow the pattern's rule — they classify unqualified and are covered only by unqualified grants; handling per endpoint family is in the Decision section below.
- The connection's existing `repositories` config is demoted from the primary scoping knob to the pattern's optional **ceiling** (broker-enforced, reflected in the picker).

---

## Decision

**Chosen:** Category:verb taxonomy (A-1), legacy bridge via `requires` with covers-parent umbrellas (B-1), verb-classified catch-all for uncategorized routes (C-1), permission-down-scoped per-category token minting (D-1), and grant-time repository qualifiers (E-1).

### Scope grammar

```
connection:<name>:<category>[:<repository>]:<read|write>
```

Path segments `[name, category, repository?]`; the repository qualifier is optional grant data, never vocabulary — the shape defined by the [Connection Scope Qualifiers ADR](2026-07-22-connection-scope-qualifiers.md). Breadth is entirely `covers` (prefix coverage — shorter path, same action):

| Grant scope | Means |
|---|---|
| `connection:gh:read` | every category, every repository (umbrella) |
| `connection:gh:contents:write` | push/contents-write on all repositories |
| `connection:gh:issues:webapp:write` | issue writes on the `webapp` repository only |
| `connection:gh:repo:read` | legacy/catch-all reads, all repositories (plus, via `requires`, every category read — the compatibility bridge) |

### The category table

| Category | Route families classified into it | Installation-token permissions minted |
|---|---|---|
| `metadata` (read-only) | `GET /repos/{o}/{r}` (bare repo object), `/rate_limit`, `/user`, `/installation/repositories`, API-root discovery | `metadata: read` |
| `contents` | git smart-HTTP (`info/refs`, `git-upload-pack` → read; `git-receive-pack` → write), `/repos/{o}/{r}/`{`contents`, `git/*`, `commits`, `branches`, `tags`, `compare`, `releases`}, the content-host family (`codeload.github.com`, `*.githubusercontent.com` — GET/HEAD only), and `PUT /repos/{o}/{r}/pulls/{n}/merge` (merging mutates contents; GitHub itself demands `contents: write` for it) | `contents: read\|write` |
| `issues` | `/repos/{o}/{r}/`{`issues`, `labels`, `milestones`, `assignees`} and issue comments | `issues: read\|write` |
| `pulls` | `/repos/{o}/{r}/pulls` and subresources (reviews, review comments, requested reviewers) — except `merge`, which is `contents:write` | `pull_requests: read\|write` |
| `actions` | `/repos/{o}/{r}/actions/`{`workflows`, `runs`, `jobs`, `artifacts`, `caches`} — **not** `secrets`/`variables` | `actions: read\|write` |
| `checks` | `/repos/{o}/{r}/`{`check-runs`, `check-suites`}, `/commits/{ref}/`{`check-runs`, `check-suites`, `status`, `statuses`}, `POST /repos/{o}/{r}/statuses/{sha}` | `checks: read\|write`, `statuses: read\|write` |
| `admin` | `PATCH`/`DELETE /repos/{o}/{r}` (settings, deletion), `/hooks`, `/keys`, `/collaborators`, `/branches/{b}/protection`, `/actions/`{`secrets`, `variables`}, `/dependabot/secrets` | `administration: read\|write`, `repository_hooks: read\|write`, `secrets: read\|write`, `dependabot_secrets: read\|write` |
| `repo` (catch-all + umbrella) | Everything not matched above — deployments, packages, pages, discussions, `/search/*`, `/graphql` (POST → write, unchanged), and any future GitHub surface | umbrella mints the connection's configured `permissions` verbatim when set, else the union of every row above |

Actions-secrets and webhook surfaces sit under `admin` deliberately — the direct analog of the Kubernetes taxonomy splitting Secrets from ConfigMaps: an admin can grant the routine CI surface (`actions:*`) without the credential-adjacent one.

### `requires` edges

Published by the egress gateway's `ConnectionScopes` per connection `<n>`:

- `connection:<n>:repo:read` → requires every category `:read` action on `<n>`.
- `connection:<n>:repo:write` → requires every category `:write` action on `<n>`.
- Every category action on `<n>` → requires `connection:<n>:metadata:read`.

The last edge is the lesson from Kubernetes Amendment 1's discovery-endpoint gotcha, solved structurally this time instead of documented: `gh`, Octokit, and most SDKs probe metadata endpoints before real work, so every category grant auto-confers the metadata reads rather than making "also grant `metadata:read`" a support surprise. (GitHub behaves the same way itself: every installation token carries `metadata: read` implicitly.)

`requires` edges live on vocabulary entries, and repository-qualified scopes are grant data that never enter the vocabulary — so the metadata edge cannot fire for an agent granted *only* qualified scopes (`[gh, issues, webapp]` neither covers nor is covered by the vocabulary leaf `[gh, issues]`). The attachment flow closes this the same way it already compiles auto-egress: any connector attachment with repository-qualified selections also emits the unqualified `connection:<n>:metadata:read`.

The `connection:<n>:*` entry currently published by `ConnectionScopes` is removed rather than extended: `scope_covers` has no wildcard rule, its `*` action segment never matches `read`/`write`, and it therefore authorizes nothing today. Its intended meaning is exactly what the covering parents `connection:<n>:read`/`connection:<n>:write` express properly.

### Classifier shape

`ClassifyAction` gains host-family awareness (the `*http.Request` it already receives carries the host; only the doc comment claimed it wouldn't be inspected):

1. git smart-HTTP shapes (existing detection, unchanged) → `contents:read` / `contents:write` — **reclassified** from `repo:*`.
2. API host (`api.github.com`, or the GHE `/api/v3` path prefix on the appliance host; `/api/graphql` on GHE) → route-family table above, keyed on the segments after `/repos/{owner}/{repo}/`, verb → `:read`/`:write` exactly as `classifyREST` does today.
3. Content-host family (`codeload.github.com`, `*.githubusercontent.com`) → GET/HEAD `contents:read`; any other verb denied (`ErrUnsupportedAction` — nothing legitimate writes there).
4. Anything else → verb-classified `repo:read`/`repo:write` catch-all (today's behavior, unchanged).

The classifier also extracts the **repository qualifier** wherever the path carries one: git smart-HTTP (`/{owner}/{repo}.git/…`), `/repos/{owner}/{repo}/…`, and the content-host download shapes all name the repository. The owner segment is verified against the connection's declared account and the request is denied on mismatch (see Decision E — never dropped, or foreign-owner name collisions would qualify); the repository name is lowercased. The broker's `CheckInput` then carries `PathSegments: [name, category, repository]`, and the rego evaluates the fully qualified required scope — an unqualified or parent grant covers it by prefix, a qualified grant must match it.

Requests that carry **no repository** (`/graphql`, `/search/*`, `/user`, `/rate_limit`, API-root discovery) produce an unqualified required scope, satisfiable only by an unqualified grant of the matching category (or a covering parent). A deeper qualified grant deliberately does *not* cover the shallower requirement — fail closed. In practice: metadata-family probes are covered by the unqualified `metadata:read` every attachment emits; `/graphql` and `/search/*` sit in the catch-all, so a strictly repo-qualified agent loses them — the minted token for a catch-all-granted agent still carries the granted repository list, so GitHub bounds what such queries can see.

### Minting

`Resolve` builds `InstallationTokenRequest.Permissions` from the table row for `CallerIdentity.Action`, and `InstallationTokenRequest.Repositories` from the request's extracted repository (`CallerIdentity` grows a `Repository` field alongside `Action`; a repo-less request falls back to the connection's configured list, if any). When the connection's config declares a `permissions` map, it acts as a **ceiling**: the derived set is intersected with it key-by-key (`read` < `write`; a key absent from the ceiling is dropped). The connection's `repositories` config is likewise demoted from the primary scoping knob to an optional ceiling — the qualifier from the authorized request is what mints. The cache key moves from the raw config to the **derived** permission set plus the derived repository set, so per-category, per-repository tokens never cross-contaminate — the shape `cacheKey` was documented to want. Cache growth is repositories × categories, bounded by actual traffic and the existing sweep threshold.

The `workflows: write` App permission (required by GitHub for any push touching `.github/workflows/`) is deliberately **excluded** from the derived `contents:write` set — an agent that can push code should not silently also be able to rewrite CI workflows (the classic secrets-exfiltration escalation). A connection whose agents legitimately edit workflow files declares `workflows: write` in its config `permissions`, and only the `repo:write` umbrella (which mints the configured set verbatim) picks it up.

---

## Consequences

### Positive

- Admins can finally express the real agent role shapes — issue triage, PR review, CI status, code-only — as grants, with the audit log attributing every brokered call to a per-surface action **and repository** instead of a binary read/write over everything.
- Repository reach is per-agent: one connection (the installation) serves an all-repos agent and a two-repos agent simultaneously, each with its own slice, and "all repositories" is a single covering scope that keeps covering repositories created tomorrow.
- The minted credential matches the authorization on both axes: a leaked or replayed token is bounded by GitHub itself to the category's permissions and the request's repository, not just by kora's gate. This is the first adapter to exercise mint-time down-scoping, proving that half of the broker design.
- Zero stored-grant migration and zero CRD change — the delta lives in the metadata table, the adapter, the scope publisher, one `CheckInput`/structural-validation touch in the policy path, and the attachment flow's grant compilation.

### Negative

- The category enumeration is incomplete by design; uncategorized surface requires the umbrella, so a strictly-category-granted agent may hit denials on legitimate long-tail endpoints until the table grows a row. Extending it is a one-line change (plus its minting row), ordinary maintenance per the Kubernetes precedent, not a new ADR.
- More vocabulary entries per GitHub connection (16 + wildcard, up from 3) — the same growth the picker and grants UI already absorbed for Kubernetes' 15.
- `repo:write`'s name undersells what it confers (everything, via `requires`). Mitigated by description strings and by the compiled bundle showing the expansion, but it is a naming debt accepted for compatibility.
- A renamed or transferred repository strands its qualified grants: the gate starts denying (fail closed) with no signal at rename time. The attachment UI should flag selections that no longer resolve against the installation; until it does, this is a support surface.
- A strictly repository-qualified agent loses the repo-less surfaces entirely — `/graphql` and `/search/*` need the (unqualified) catch-all. Accepted: those endpoints cannot be repo-checked at the gate, and granting them un-scoped by default would quietly undo the qualifier.

### Neutral

- GraphQL remains all-or-nothing (`repo:write` via the catch-all), unchanged from today; parsing queries for finer classification stays explicitly deferred.
- Workflow-file pushes fail for category-granted agents unless the connection ceiling includes `workflows: write` and the agent holds the umbrella — a deliberate security default, documented so it isn't a support surprise.

---

## Implementation

1. `internal/platform/connections/adapters.go` — new action constants and `ActionMeta` rows for `github_app`; extend `ActionMeta` with a `RequiresActions []string` field (action fragments, resolved to full `connection:<n>:<fragment>` scopes by the publisher) so `requires` edges live in the same single source of truth as the actions themselves.
2. `internal/egressgw/scopes.go` — `ConnectionScopes` copies each action's resolved `Requires` into the published `ScopeDeclaration` (the field already exists on `routescopes.ScopeDeclaration` and is already persisted and expanded by the policy service); publish the `connection:<n>:read`/`write` covering parents; stop publishing `connection:<n>:*`.
3. `internal/credentials/adapters/githubapp/githubapp.go` — replace `classifyREST`-only logic with the four-step classifier including repository extraction; add the category→permissions table; derive minted permissions and repositories from `CallerIdentity` with the config ceilings intersected; re-key the cache on the derived permission and repository sets.
4. `internal/egressgw/service.go` + `internal/credentials/adapter.go` — `Broker.Authorize` passes the extracted repository through `CheckInput.PathSegments` and `CallerIdentity.Repository`. (The adapter interface's `ClassifyAction` return grows to carry the qualifier; `passthrough`/`k8sbearer` return it empty.)
5. `internal/policyservice/api/service.go` — structural validation for repository-qualified connection scopes at grant time (known connection-scope shape plus one extra path segment: lowercase, no `:` or `/`), mirroring how `egress:<host>:connect` scopes are validated without vocabulary enumeration.
6. `internal/agents/effective_scopes.go` (attachment compilation) — compile repository selections into qualified grant scopes; always include unqualified `connection:<n>:metadata:read`; "all repositories" emits the unqualified category scopes.
7. A brokered repository-listing endpoint feeding the attachment picker (the gateway lists the installation's repositories with a metadata-scoped token, admin-initiated), consumed by the `ConnectionCards` resource-scope extension defined in the [qualifier pattern ADR](2026-07-22-connection-scope-qualifiers.md)'s Surfaces section.
8. Tests — table-driven classifier tests mirroring `k8sbearer`'s (every route family × verb, GHE prefix forms, content-host family, catch-all, repository extraction), minting tests asserting permission/repository derivation, ceiling intersection, and cache-key separation, and rego/compiler tests for qualified-scope coverage and the metadata `requires` edges.

### Migration Path

None required for stored grants. On gateway rollout: the scope publisher re-upserts the vocabulary (new actions, covering parents, and `requires` edges appear; existing entries update in place; the `*` entry is dropped), the next bundle compile expands existing `repo:read`/`repo:write` grants to the category set, and every in-flight grant keeps today's effective access. Any grant holding `connection:<n>:*` was already a no-op under `scope_covers` and loses nothing; the rollout notes should call out replacing those with the covering parents. Connections using `config.repositories` keep it as a mint-time ceiling. Rollback is equally clean: the old classifier's two actions remain valid vocabulary.

---

## Validation

### Success Metrics

Mirroring the Kubernetes ADR's live-verification matrix, against the local cluster's GitHub connection:

- An agent granted only `connection:<n>:issues:write` can comment on an issue, is policy-denied for a push (`git-receive-pack` → `contents:write`), and its minted token — extracted at the gateway for the test, never in the sandbox — is rejected by GitHub itself on a `GET /repos/{o}/{r}/contents/...` (token-layer proof, not just gate-layer).
- An agent granted only `contents:read` can clone but cannot push, and cannot read issues via REST.
- A pre-existing `repo:read` + `repo:write` grant behaves identically before and after rollout across clone, push, issue comment, and an uncategorized endpoint (e.g. a deployments call).
- An agent granted `connection:<n>:issues:webapp:write` can comment on `webapp` issues, is policy-denied on a sibling repository's issues, and its minted token is rejected by GitHub on the sibling repository even when presented directly (both gate and token bound the repo).
- A request for a same-named repository under a **foreign owner** (including a public one) is denied at classification (owner mismatch), never matched against the qualifier.
- An agent granted the unqualified `connection:<n>:issues:write` reaches issues on a repository created *after* the grant, with no grant change — the covers-parent property.
- The audit log shows per-category, per-repository actions for classified calls and `repo:*` only for genuine catch-all traffic.

### Monitoring

The broker's existing structured logging needs no changes: `egress authorized` / `egress denied by policy` already carry the action, which now carries the category. A spike in `repo:*`-classified traffic is the signal that a route family is popular enough to earn its own table row.

---

## Related Decisions

- [connection-scope-qualifiers](2026-07-22-connection-scope-qualifiers.md) — the connector-generic qualifier pattern this ADR instantiates for repositories (grammar, ordering, ceiling, adapter seams live there).
- [jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — the broker architecture and the `github_app` adapter this ADR refines; its classification section is superseded by the category table above.
- [kubernetes-connection-bearer-adapter-and-gateway-ca-trust](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md) — Amendment 1 is the direct precedent for category:verb taxonomy; this ADR documents where and why it diverges (catch-all instead of deny).
- [mcp-tool-gateway](2026-06-29-mcp-tool-gateway.md) — origin of the vocabulary `requires` expansion this ADR reuses for umbrella compatibility.

## References

- `internal/credentials/adapters/githubapp/githubapp.go` — current classifier and minting path
- `internal/credentials/adapters/k8sbearer/k8sbearer.go` — the category-classifier shape mirrored
- `internal/platform/connections/adapters.go` — the metadata table carrying the vocabulary
- `internal/policyservice/bundle/compiler.go` (`expandRequires`) — the umbrella expansion mechanism
- [GitHub REST: create an installation access token](https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app) — the `permissions`/`repositories` down-scoping this ADR exploits

---

## Notes

- Exact GitHub permission-key names in the minting table (`repository_hooks` vs `webhooks`, `dependabot_secrets`, variables) are to be verified against the installation-token API during implementation; the categories they back are the decision, the key spellings are not.
- Open, deliberately deferred: org-level categories and GraphQL classification. Each is additive to this taxonomy, none is blocked by it. (Per-repository scoping, deferred in the first draft, is now Decision E via the [qualifier pattern](2026-07-22-connection-scope-qualifiers.md).)
- **Same-host connections:** two github.com connections in one tenant claim the same egress host family. [Same-Host Connection Selection in the Egress Gateway](2026-10-15-same-host-connection-selection.md) decides how the broker selects among them by the declared `owner` and how the operator rejects overlaps it cannot resolve.

**Last Updated:** 2026-07-22

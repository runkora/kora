# Agent Skills

<!-- Filename: 2026-08-17-agent-skills.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-08-17
**Deciders:** Justin Wilkin
**Technical Story:** [Agent Skills PRD](../prd/agent-skills.md)

---

## Context

The [Agent Skills PRD](../prd/agent-skills.md) requires a Skills page where tenant members can browse, search and read skills from the GitHub repositories their tenant registers. Each repository will be one Marketplace. A skill is a folder that holds a `SKILL.md` with YAML frontmatter (`name`, `description`) and optional supporting files.

### Background

Tenant administrators work in their tenant namespace (`kora-tenant-<name>`) through GitOps. Access to that namespace decides who acts for the tenant. Registry kinds without database rows reach services as operator-rendered `kora-system` ConfigMaps and Secrets that the service mounts. `MCPServer` is the reference: `internal/operator/mcpserver_aggregator.go` renders `kora-mcp-servers` and copies each referenced credential into `kora-mcp-server-secrets`.

A GitHub App installation token can be minted for a subset of the installation's repositories and permissions (`internal/platform/clients/github/`).

### Problem Statement

Tenants need to register skill repositories in their own GitHub organizations, including private ones. Each repository must be read with a credential that reaches only that repository. Browsing must not call GitHub on every interaction, and one large repository must not exhaust GitHub rate limits or service memory.

### Goals

- Serve each tenant only the Marketplaces declared in its namespace.
- Read each repository with its own credential, limited to that repository.
- Serve browse, search and skill pages from a cache and bound the crawl cost of each repository.
- Build the Skills page on the dashboard's server component, BFF and Hades patterns.

### Non-Goals

- Installing skills, adding skills to agents, or running skill files.
- Ranking search results by relevance.
- Marketplaces shared by every tenant, and providers other than GitHub.
- A Marketplace status view in the dashboard.

---

## Decision Drivers

- **Credential reach:** A credential must not read more than the one repository a Marketplace declares.
- **Service access:** skills-service must not gain Kubernetes API access or a database.
- **Key custody:** GitHub App private keys must stay in Kubernetes Secrets.

---

## Considered Options

### Option 1: Tenant Marketplaces with Per-Marketplace GitHub Apps

**Description:** A tenant administrator applies a `SkillMarketplace` in the tenant namespace that declares a repository and the GitHub App that reads it. The operator aggregates every Marketplace into a `kora-system` ConfigMap and copies each private key into a `kora-system` Secret. skills-service mounts both, mints a token per Marketplace and calls GitHub directly.

**Pros:**
- Each token reads one repository.
- The resource follows the `MCPServer` registry pattern.

**Cons:**
- Each tenant must create and install a GitHub App for every organization it reads from.
- The CR status shows only what the operator can check. Whether GitHub accepts the credential shows on the Skills page.

### Option 2: Tenant Marketplaces Projected into skills-service

**Description:** The operator sends each `SkillMarketplace` and its private key to an internal skills-service API. skills-service stores them and returns status to the CR.

**Pros:**
- The CR status reflects whether skills-service read the repository.
- Changes apply on reconcile with no kubelet sync delay.

**Cons:**
- skills-service needs a database and migrations for records that have no API path and no references.
- Private keys are stored at rest in the skills-service database.

### Option 3: Marketplaces Read Through the Credential Broker

**Description:** Each Marketplace references a `github_app` `Connection` in its namespace. skills-service fetches each repository through the credential broker with that Connection.

**Pros:**
- Credentials stay in the broker.

**Cons:**
- The broker serves agent outbound calls. A platform service calling it on behalf of a tenant is a new path.

### Option 4: Platform GitHub App with Approved Repositories

**Description:** One platform GitHub App reads every repository. A platform administrator approves each repository for each tenant.

**Pros:**
- Tenants set up no GitHub App.

**Cons:**
- The platform token reaches every repository on its installation, including other tenants' private repositories.
- Approval duplicates the namespace boundary.

---

## Decision

**Chosen Option:** Option 1 - Tenant Marketplaces with Per-Marketplace GitHub Apps

### Rationale

None at this time.

### The `SkillMarketplace` Resource

`SkillMarketplace` is `runkora.io/v1alpha1`, namespaced, with a status subresource and the short name `kskm`. It ships in `charts/kora-crds` and is the only way to register a Marketplace.

```yaml
apiVersion: runkora.io/v1alpha1
kind: SkillMarketplace
metadata:
  name: team-skills
  namespace: kora-tenant-example
spec:
  github:
    owner: example-org
    repo: skills
    ref: main
    app:
      appID: "4015347"
      installationID: 139299720
      privateKeySecretRef:
        name: skills-github-app
        key: private-key.pem
```

- `metadata.name` is the Marketplace slug in API paths and dashboard URLs. The display name is the repository name.
- `spec.github.ref` is optional. A 40-character hex value pins a commit SHA, any other value tracks a branch, and no value tracks the default branch.
- `privateKeySecretRef` references a Secret in the same namespace.
- A tenant can declare several Marketplaces from different GitHub organizations, each with its own GitHub App.

### Operator Aggregation

`SkillMarketplaceAggregator` runs on every Marketplace reconcile and every tenant reconcile:

- It resolves the owning tenant with `servedTenants`.
- It renders `kora-skill-marketplaces` in `kora-system`. Each tenant entry lists its Marketplaces with owner, repo, ref, App ID, installation ID and the data key of the private key.
- It copies each referenced private key into `kora-skill-marketplace-secrets` in `kora-system`. The data key derives from the namespace and the Marketplace name.
- `internal/platform/marketplace` defines the file shape for the operator and skills-service.

Each Marketplace reports a phase and an `Aggregated` condition:

- `Pending` with `TenantNotActive` when no active tenant owns the namespace.
- `Invalid` with `GlobalSourcesNotSupported` in `kora-system`.
- `Invalid` with `DuplicateRepository` when an older Marketplace in the namespace holds the same repository.
- `Aggregated` with `Aggregated` when the Marketplace is served.
- `Aggregated` with `CredentialUnavailable` when the Secret or key is missing or invalid. The message cites the Secret and the key, and browsing the Marketplace fails closed.

### skills-service

skills-service mounts the Marketplaces ConfigMap and the keys Secret and reloads both on change.

- **Tokens:** skills-service mints each installation token with `repositories: [repo]` and `permissions: {contents: read}`. For a repository outside the App's installation, it mints a token with `permissions: {metadata: read}` only. That token reads public repositories and is refused on private ones.
- **Crawl:** A crawl resolves the ref to one commit, reads the tree at that commit and fetches each `SKILL.md`. A skill page loads its `SKILL.md` and the list of its other files. Each other file is fetched when it is selected and then cached. A `SKILL.md` at the repository root is skipped.
- **Skill identity:** A skill is identified by its folder path. The frontmatter `name` is the display name.
- **Limits:** The chart sets `skillsService.limits` for skills per Marketplace (500), listed files per skill (500), bytes per file (128 KiB) and total `SKILL.md` bytes per Marketplace (8 MiB). An empty value removes that cap. Skills and files over a limit are skipped and logged. Binary files are never fetched.
- **Cache:** Redis holds the skill list per tenant and Marketplace, each skill's files, and failed crawls. Every key carries the tenant ID. Entries expire after `cacheTTL` (12 hours) and failed crawls after a shorter TTL.
- **Search:** skills-service filters the cached list in memory and needs no Redis module. A skill matches when the query appears in its title, name, description or `SKILL.md` body.
- **Errors:** A missing repository, ref or skill returns 404. An unavailable credential or an upstream failure returns 502 with a message that cites no provider. An unavailable Redis returns 503, a server timeout returns 504, and a cancelled request gets no response.

The API is under `/v1` and requires the `skills:read` scope. The tenant comes from the verified platform token.

- `GET /v1/marketplaces` returns the tenant's Marketplaces. Each has its slug, repository, ref, the commit and time of its last crawl, and its skill count. The commit, time and count are empty until the Marketplace has been crawled.
- `GET /v1/marketplaces/skills?marketplace=&keys=&search=&offset=&limit=` returns one page of skills across the tenant's Marketplaces, in title order, with `total`. `marketplace` narrows it to one Marketplace, and `keys` narrows it to a list of `marketplace:path` keys. Each skill carries its Marketplace, path, name, title (the first `SKILL.md` heading), description, and the frontmatter version, tags, allowed tools and manual-only flag.
- `GET /v1/marketplaces/{marketplace}/skills/{path...}` returns one skill's title, `SKILL.md`, file list, the commit it was read at and the same frontmatter fields as the list.
- `GET /v1/marketplaces/{marketplace}/skills/{path...}?file={path}` returns one listed file.

### Dashboard

- **Navigation:** The Skills entry under Control Plane is gated on `skills:read`.
- **Routes:** The overview is `/skills`, and a skill is `/skills/[marketplace]/[...path]`.
- **Loading:** The server component loads the first page from the URL. Later browse changes call the BFF route `/api/skills` and update the URL with `window.history.replaceState` (see `frontend/docs/golden-paths/frontend-state-pattern.md`).
- **State:** The URL holds `marketplace` (or `favourites`), `search` and `page`. Both the card and the list view show results in title order, ten per page.
- **Favourites:** Each user's favourites are a `skills.favouriteSkills` list of `marketplace:path` keys in the preferences document, like the Models page favourites. The BFF sends them as `keys` for the Favourites view.
- **Failure:** A Marketplace that returns 502 shows as unavailable with the API message and a retry action.
- **Components:** The overview follows the MCP servers page. Marketplace and Favourites filter chips with a count sit above both views. The card and list toggle sits in the search row for cards and in `toolbarActions` for the table. Each skill shows the `file` icon. The detail page has a header, the open file and a rail with Files and Source, and "View source" links to the skill folder at the served commit. The file explorer uses the Hades icons `file`, `file-code`, `folder` and `folder-open`.
- **Content:** `SKILL.md` renders as markdown with a Raw view. Other files render as plain text through Hades `CodeBlock`.

### Local Development

- `deploy/local/marketplace/` defines one example Marketplace for `kora-tenant-example` that reads `anthropics/skills`.
- `scripts/cluster/create-secrets.sh` creates its App Secret only when a key file exists. Without the file, the Marketplace reports `CredentialUnavailable` and `make up` succeeds.
- No service has a local authentication bypass or a host-run configuration override.

---

## Consequences

### Positive

- A tenant administrator will add a Marketplace with one CR and one Secret.

### Negative

- Repository changes on a tracked branch will appear only after the cache expires.
- A rotated private key will take effect after one operator pass and the kubelet Secret projection delay.
- Concurrent cache misses for the same Marketplace on different replicas will each crawl GitHub.
- A public repository read outside the App's installation uses that App's rate limit.

### Neutral

- A later agent-to-skill link will belong to the agent's own record. A pinned skill version will be a commit SHA in the agent configuration.

---

## Implementation

### Resource and Operator

- `SkillMarketplace` Go types with kubebuilder markers live in `backend/internal/operator/` and register in `internal/operator/scheme.go`. controller-gen generates their deepcopy functions and `charts/kora-crds/generated/runkora.io_skillmarketplaces.yaml`.
- A `SkillMarketplace` reconciler and `SkillMarketplaceAggregator` in `internal/operator/` follow `mcpserver_controller.go` and `mcpserver_aggregator.go`. The tenant reconciler runs the aggregator on every tenant change.
- Like `MCPServerAggregator`, the aggregator reads referenced Secrets uncached through the operator's API reader. A Marketplace with a credential requeues every five minutes.
- The operator `ClusterRole` in `charts/kora/templates/operator/clusterrole.yaml` gains `get/list/watch` on `skillmarketplaces` and `get/patch` on their status.
- `internal/platform/marketplace` holds the ConfigMap and Secret names and the Go type of the sources file. The operator writes the file and skills-service reads it through the same type.

### skills-service

- `backend/cmd/skills-service/` holds `main.go` and `config/base.yaml`. Config keys cover `cacheTTL`, the failure TTL, the crawl limits and the mounted file paths. Secrets load through `os.Getenv` only.
- `backend/internal/skills/` holds the feature:
    - The registry reads the sources file and the keys directory and reloads them on change.
    - The token provider mints and caches one installation token per Marketplace.
    - The crawler resolves the ref, reads the tree and fetches files.
    - The cache wraps the platform Redis client.
    - The service runs search and paging over the cached list.
    - The handlers implement `api.Controller` and use `api.JSON` and `api.ErrorResponse`.
- GitHub calls go through the `httpclient.Base` client in `internal/platform/clients/github/`. The client gains calls to resolve a ref to a commit and to read a tree and a file at that commit.
- skills-service registers the `skills:read` scope and its route scopes with policy-service through `internal/platform/clients/routescopes`.

### Chart

- `charts/kora/templates/skills-service/` renders the Deployment, Service, ServiceAccount, config ConfigMap and client-mTLS DestinationRule.
- The Deployment mounts `kora-skill-marketplaces` and `kora-skill-marketplace-secrets` read-only.
- `charts/kora/templates/api/virtualservice.yaml` routes `/v1/marketplaces` to skills-service. `charts/kora/templates/platform/netpols.yaml` opens the skills-service port to mesh callers.
- The frontend deployment receives `SKILLS_SERVICE_URL`.
- The chart renders a ServiceEntry for `api.github.com:443` so skills-service works under `REGISTRY_ONLY` outbound traffic.

### Dashboard

- The pages live under `frontend/src/app/(dashboard)/skills/` with feature-scoped (Tier 3) components.
- `frontend/src/server/` holds the only upstream calls to skills-service. `frontend/src/app/api/skills/route.ts` is the BFF route, registered in `frontend/src/lib/routing/api-routes.ts`.
- `frontend/src/shared/api.ts` holds the response types that mirror the skills-service JSON.
- The Control Plane navigation entry reads the user's scopes and shows Skills only with `skills:read`.
- `frontend/src/app/(dashboard)/_preferences/preferences.schema.ts` gains `skills.favouriteSkills`.

### Local Development and Docs

- `scripts/cluster/redeploy.sh` and `REDEPLOY_PLATFORM_SERVICES` in the `Makefile` list `skills-service`.
- `make cluster-deploy` applies `deploy/local/marketplace/`.
- `docs/guides/skill-marketplace-sources.md` explains how a tenant administrator creates the GitHub App, the Secret and the `SkillMarketplace`. `docs/code-organization.md` lists the `skills-service` binary and the `skills` package.

### Migration Path

None at this time.

### Timeline

None at this time.

---

## Validation

### Success Metrics

- Tenant members with `skills:read` can find a given skill and identify its source repository without leaving Kora.
- A token minted for one Marketplace cannot read another repository on the same installation.
- A user without `skills:read` sees no Skills entry, and the API refuses the call.

### Monitoring

- `kubectl get kskm` lists every Marketplace with its phase and reason.
- skills-service logs each skipped skill or file with the Marketplace, the path and the limit it exceeded.
- skills-service logs each failed crawl with the Marketplace and the error, and the GitHub client records a span per call.

---

## Related Decisions

- [mcp-server-static-credentials](2026-10-22-mcp-server-static-credentials.md) - Credential aggregation pattern for registry kinds
- [github-connection-category-scoped-actions](2026-07-22-github-connection-category-scoped-actions.md) - Down-scoped GitHub installation tokens

---

## References

- [Agent Skills PRD](../prd/agent-skills.md)
- [Skill marketplace sources guide](../guides/skill-marketplace-sources.md)

---

## Notes

None at this time.

**Last Updated:** 2026-11-04

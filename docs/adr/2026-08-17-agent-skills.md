# Agent Skills

<!-- Filename: 2026-08-17-agent-skills.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-08-17
**Deciders:** Platform Engineering, Product Owner
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

- **Tokens:** skills-service mints each installation token with `repositories: [repo]` and `permissions: {contents: read}`.
- **Crawl:** A crawl resolves the ref to one commit, reads the tree at that commit and fetches each `SKILL.md`. Supporting files load when a skill page opens, and a skill cache miss fetches only that skill. A `SKILL.md` at the repository root is skipped.
- **Skill identity:** A skill is identified by its folder path. The frontmatter `name` is the display name.
- **Limits:** `backend/cmd/skills-service/config/base.yaml` caps skills per Marketplace, files per skill and bytes per file. Skills and files over a limit are skipped and logged. Binary files are never fetched.
- **Cache:** Redis holds the skill list per tenant and Marketplace, each skill's files, and failed crawls. Every key carries the tenant ID. Entries expire after `cacheTTL` (12 hours) and failed crawls after a shorter TTL.
- **Search:** skills-service filters the cached list in memory and needs no Redis module. A skill matches when the query appears in its name, description or `SKILL.md` body.
- **Errors:** A missing repository, ref or skill returns 404. An unavailable credential or a GitHub failure returns 502.

The API is under `/v1` and requires the `skills:read` scope. The tenant comes from the verified platform token.

- `GET /v1/marketplaces` returns the Marketplaces of the caller's tenant.
- `GET /v1/marketplaces/{marketplace}?search=&offset=&limit=` returns one page of skills in name order with `total`.
- `GET /v1/marketplaces/{marketplace}/skills/{path...}` returns one skill and its text files.

### Dashboard

- **Navigation:** The Skills entry under Control Plane is gated on `skills:read`.
- **Routes:** The overview is `/skills/[marketplace]` and a skill is `/skills/[marketplace]/[...path]`. `/skills` opens the first Marketplace.
- **Loading:** The server component loads the first page from the URL. Later browse changes call the BFF route `/api/skills` and update the URL with `window.history.replaceState` (see `frontend/docs/golden-paths/frontend-state-pattern.md`).
- **State:** The URL holds the Marketplace, `search` and `page`. Results use the `tables.defaultPageSize` preference.
- **Components:** The overview uses Hades `DataTable` with the list and card toggle in `toolbarActions`. The file explorer uses the Hades icons `file`, `file-code`, `folder` and `folder-open`.
- **Content:** File bodies render as plain text through Hades `CodeBlock`.

### Local Development

- `deploy/local/marketplace/` defines one example Marketplace for `kora-tenant-example`.
- `scripts/cluster/create-secrets.sh` creates its App Secret only when a key file exists. Without the file, the Marketplace reports `CredentialUnavailable` and `make up` succeeds.
- No service has a local authentication bypass or a host-run configuration override.

---

## Consequences

### Positive

- A tenant administrator will add a Marketplace with one CR and one Secret.

### Negative

- Repository changes on a tracked branch will appear only after the cache expires.
- A rotated private key will take effect after one operator pass and the kubelet Secret projection delay.
- Two concurrent cache misses for the same Marketplace will both crawl GitHub.

### Neutral

- A later agent-to-skill link will belong to the agent's own record. A pinned skill version will be a commit SHA in the agent configuration.

---

## Implementation

None at this time.

### Migration Path

None at this time.

### Timeline

None at this time.

---

## Validation

### Success Metrics

None at this time.

### Monitoring

None at this time.

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

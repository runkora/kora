# Skill marketplace sources

A skill marketplace is a GitHub repository of skills that a tenant's users browse in the dashboard. A tenant registers one with a `SkillMarketplaceSource` in its own namespace. kora serves it only after a platform administrator approves the repository for that tenant.

The following guide assumes your tenant exists. See [Tenants](tenants.md) to set one up.

## Registering a marketplace

A marketplace is defined using the `SkillMarketplaceSource` CRD, short name `osms`. Apply one in your tenant's namespace:

```yaml
apiVersion: orbit.io/v1alpha1
kind: SkillMarketplaceSource
metadata:
  name: acme-internal
  namespace: orbit-tenant-acme
spec:
  displayName: Acme Internal Skills
  github:
    owner: acme-corp
    repo: internal-skills
    branch: main
```

- `displayName`: the name shown in the dashboard, up to 100 characters. It also names the marketplace in URLs: kora lowercases it and collapses every run of other characters into a hyphen, so `Acme Internal Skills` becomes `acme-internal-skills`. It must contain an ASCII letter or digit, and its URL form must be unique within your namespace.
- `github.owner` and `github.repo`: the repository holding your skills. Both are validated when you apply the source. Every folder that contains a `SKILL.md`, at any depth, is a skill. A `SKILL.md` at the repository root is skipped, because a skill needs its own folder.
- `github.branch`: the branch to read. It defaults to `main`, so a source that omits it reads `main` even when the repository's default branch has another name. If yours does, set `branch` to that name, or to `HEAD` to follow the repository's default branch.

The tenant is the namespace the source is applied in. The spec has no tenant field, so a source cannot name another tenant.

A `SkillMarketplaceSource` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start.

## Permissions

To manage sources, your Kubernetes role needs access to `skillmarketplacesources` in the `orbit.io` API group, in your tenant's namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: skill-marketplace-source-admin
  namespace: orbit-tenant-acme
rules:
  - apiGroups: [orbit.io]
    resources: [skillmarketplacesources]
    verbs: [get, list, watch, create, update, patch, delete]
```

`kubectl apply` and `kubectl edit` send changes to an existing source as patches, so the role includes `patch` alongside `update`. Bind the role to your tenant's administrators with a `RoleBinding` in the same namespace.

The role must not give write access to `orbittenants`. That resource is cluster-scoped, and the list of approved repositories lives on it, so only platform administrators edit it.

## Requesting approval

Check what kora made of your source:

```bash
kubectl get skillmarketplacesources -n orbit-tenant-acme
kubectl describe skillmarketplacesource acme-internal -n orbit-tenant-acme
```

The `Phase` and `Reason` columns show the verdict, and `describe` shows its full message on the `Aggregated` condition. Unless another check in [Status](#status) applies first, a new source is `Pending` with reason `NotAllowlisted`.

Registering a source notifies nobody. Ask your platform administrator, through your usual support channel, to approve `acme-corp/internal-skills` for your tenant. Every repository needs approval, public ones included: kora reads all marketplaces with one platform GitHub App and makes no GitHub call when it judges a source, so it cannot tell whether a repository is public.

Once your platform administrator approves the repository, the source moves to `Aggregated` and the skills service reloads its configuration without a restart. Kubernetes propagates a ConfigMap change to a mounted volume on its own schedule, so allow for a delay before the marketplace appears in the dashboard.

The platform GitHub App must be able to read the repository. A private repository is readable only if the App's installation covers it, so ask your platform administrator.

## Status

The `Phase` and `Reason` columns report the operator's verdict. kora applies the checks in the order below, and a source gets the first row that matches.

| Phase | Reason | Meaning |
| --- | --- | --- |
| `Invalid` | `GlobalSourcesNotSupported` | The source was applied in `orbit-system`. Apply it in your tenant's namespace. |
| `Pending` | `TenantNotActive` | No active tenant owns the namespace: the tenant is suspended, or none owns it. |
| `Invalid` | `DuplicateName` | An older source in your namespace has a `displayName` with the same URL form. Rename one of them, and read [Caching](#caching) before renaming a source that is already served. |
| `Invalid` | `DuplicateRepository` | An older source in your namespace already registers this repository, on any branch. Use one source per repository. |
| `Pending` | `NotAllowlisted` | Waiting for a platform administrator to approve the repository. |
| `Aggregated` | `Aggregated` | Served to your tenant. |

"Older" means created earlier. Sources created in the same second are ordered by `metadata.name`, and the name that sorts first counts as older. A source that is waiting for approval still holds its name and repository, so a newer source in the namespace cannot use them in the meantime.

`Aggregated` means kora handed the repository to the skills service. It does not mean GitHub can serve the repository, because kora checks nothing with GitHub when it sets the status. If the platform App cannot read the repository, or the branch does not exist, loading the marketplace's skills fails with `failed to read marketplace` (HTTP 500). A GitHub outage or rate limit gives `marketplace is temporarily unavailable` (HTTP 503) instead.

## Caching

kora caches each marketplace's skill list and each skill's content in Redis. Entries expire after 12 hours by default, and the platform's configuration can change that. A change in the repository can take that long to appear, because a request is served from the cache until the entry expires and only then reads the repository again.

The cache keys are:

```text
marketplace:{tenant}:{owner/repo@ref}
skill:{tenant}:{owner/repo@ref}:{skillSlug}
```

`{tenant}` is the `OrbitTenant` name, `owner/repo` is lowercase, and `ref` is the branch. Two tenants that register the same repository keep separate entries.

Changing `branch` does not wait for the cache. The key carries the branch, so once the skills service has the new configuration, the next request reads the new branch. The old branch's entries expire on their own.

Renaming `displayName` is different. It changes the marketplace's URL segment, and every skill identifier starts with that segment, as `<segment>__<skill>`. The cache is keyed by repository and branch, not by name, so until the existing entries expire (up to 12 hours by default) the marketplace still lists skills under the old segment, and opening one of them returns 404. Rename a source that users already rely on only when that gap is acceptable.

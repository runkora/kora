# Register a skill marketplace

In runkora, a tenant's users browse and read agent skills on the Skills page. The skills come from GitHub repositories that platform teams register as skill marketplaces. A marketplace can be the organisation's own repository of skills, or a third-party one such as `anthropics/skills`.

Each marketplace reads its repository with its own GitHub App. This allows a tenant to register repositories from several GitHub organisations.

The following guide assumes kora is already installed and the tenant exists. See [Tenants](tenants.md) to create one.

## Creating the GitHub App

Create a GitHub App in the organisation that owns the repository, and give it the **Contents: Read-only** repository permission. Install it on the repository, then generate a private key. You will need the App ID from the App's settings page, the installation ID from the installation URL, and the private key.

kora mints every token for the marketplace's repository alone, even when the App is installed across the whole organisation. Public repositories can also be read with an App that is not installed on them.

## Registering a skill marketplace

A skill marketplace is registered with the `SkillMarketplace` CR in the tenant's namespace. Store the App's private key in a Secret in the same namespace:

```bash
kubectl create secret generic skills-github-app \
  --namespace kora-tenant-example \
  --from-file=private-key.pem=./app-private-key.pem
```

Then register the marketplace:

```yaml
apiVersion: runkora.io/v1alpha1
kind: SkillMarketplace
metadata:
  name: team-skills
  namespace: kora-tenant-example
spec:
  origin: internal
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

- `origin`: whether the skills are your organisation's own (`internal`) or a third party's (`external`). The Skills page shows this as a badge on every skill.
- `github.owner` and `github.repo`: the repository holding the skills. A namespace can register a repository once.
- `github.ref`: the branch to track, or a commit SHA to pin. kora tracks the default branch when this is left out.
- `github.app`: the App ID, the installation ID, and the Secret key holding the App's private key. The Secret must be in the same namespace as the marketplace.

Every folder with a `SKILL.md` is listed as a skill when its frontmatter has a `name` and a `description`.

A `SkillMarketplace` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To list what is registered:

```bash
kubectl get skillmarketplaces -A
```

The `Phase` column reports `Pending`, `Aggregated`, or `Invalid`. When the key is missing or cannot be used, the marketplace still reports `Aggregated` with the reason `CredentialUnavailable`. Its skills will not load until the key is fixed.

Users need the `skills:read` scope to open the Skills page. See [managing roles](policy-and-roles.md#managing-roles) to add it to a role.

## Limits

Platform teams can cap how much of each repository kora reads with `skillsService.limits` in the chart. Skills and files over a limit are skipped and logged. Setting a limit to `0` removes it.

| Limit | Default | Caps |
| --- | --- | --- |
| `skillsPerMarketplace` | 500 | Skills listed per marketplace |
| `filesPerSkill` | 500 | Files listed per skill |
| `bytesPerFile` | 128 KiB | The size of one file |
| `skillMdBytesPerMarketplace` | 8 MiB | Every `SKILL.md` in a marketplace together |
| `fileCacheBytesPerMarketplace` | 32 MiB | Files cached per marketplace. Files past it are still served, without caching |

kora caches each marketplace for 12 hours, then checks whether its branch has a new commit. It only reads the repository again when it does.

## Reference

See [Agent Skills](../adr/2026-08-17-agent-skills.md) for how kora reads and caches marketplaces.

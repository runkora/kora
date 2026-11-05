# Skill Marketplaces

A skill Marketplace is a GitHub repository of skills that a tenant's members browse, search and read on the Skills page. A tenant administrator registers one with a `SkillMarketplace` in the tenant namespace, together with the GitHub App that reads the repository. A tenant can register several Marketplaces from different GitHub organizations, each with its own App.

The steps below assume a tenant created as described in [Tenants](tenants.md).

## How to Create the GitHub App

1.  Create a GitHub App in the organization that owns the repository. Give it the **Contents: Read-only** repository permission and no other permission.
2.  Install the App on the organization and select the skill repository.
3.  Copy the App ID from the App settings page and the installation ID from the installation URL (`https://github.com/organizations/<org>/settings/installations/<id>`).
4.  Generate a private key and download the PEM file.

skills-service mints each installation token with `contents: read` for the Marketplace repository alone.

A public repository works through any App, including one that is not installed on the repository. skills-service then reads it with a `metadata: read` token and uses that App's rate limit.

## How to Register a Marketplace

1.  **Store the Private Key:** Create a Secret in the tenant namespace:

    ```bash
    kubectl create secret generic skills-github-app \
      --namespace kora-tenant-example \
      --from-file=private-key.pem=./app-private-key.pem
    ```

2.  **Declare the Marketplace:** Apply a `SkillMarketplace` (short name `kskm`) in the same namespace:

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

3.  Commit both resources to your GitOps repository, or apply them with `kubectl apply -f`.

The fields are:

*   **`metadata.name`:** The Marketplace slug in API paths and dashboard URLs, at most 63 characters. The Skills page shows the repository name.
*   **`spec.origin`:** Required. `internal` for the organization's own skills and `external` for third-party skills, whatever the repository's visibility. The Skills page badges each Marketplace with it.
*   **`spec.github.owner` and `spec.github.repo`:** The repository that holds the skills.
*   **`spec.github.ref`:** Optional. A 40-character hex value pins a commit SHA, any other value tracks a branch, and no value tracks the default branch.
*   **`spec.github.app.appID` and `spec.github.app.installationID`:** The App and its installation on the repository owner.
*   **`spec.github.app.privateKeySecretRef`:** The Secret and data key that hold the PEM private key. The Secret must be in the Marketplace's namespace.

The tenant is the namespace the Marketplace is applied in. Every folder that holds a `SKILL.md` with `name` and `description` frontmatter is a skill, and the folder path identifies it. A `SKILL.md` at the repository root is skipped.

## Permissions

A tenant administrator needs write access to `skillmarketplaces` and `secrets` in the tenant namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: skill-marketplace-admin
  namespace: kora-tenant-example
rules:
  - apiGroups: [runkora.io]
    resources: [skillmarketplaces]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [""]
    resources: [secrets]
    verbs: [get, create, update, patch, delete]
```

Bind the role to the tenant's administrators with a `RoleBinding` in the same namespace.

## How to Check the Status

```bash
kubectl get kskm -n kora-tenant-example
kubectl describe skillmarketplace team-skills -n kora-tenant-example
```

The `Phase` and `Reason` columns show the operator's verdict, and `describe` shows the message of the `Aggregated` condition. The operator applies these checks in order:

1.  **`Invalid`, `GlobalSourcesNotSupported`:** The Marketplace is in `kora-system` and must move to the tenant namespace.
2.  **`Pending`, `TenantNotActive`:** No active tenant owns the namespace.
3.  **`Invalid`, `DuplicateRepository`:** An older Marketplace in the namespace registers the same repository. A Marketplace created earlier is older, and within one second the name that sorts first is older.
4.  **`Aggregated`, `CredentialUnavailable`:** The Marketplace is listed and browsing it fails. The message cites the Secret and key and the cause: the Secret or key is missing, the key is over 16 KiB or is not a PEM RSA private key, or the Secret is one the operator manages.
5.  **`Aggregated`, `Aggregated`:** The Marketplace is served.

The operator rechecks each Secret every five minutes. A fixed or rotated key takes effect after that pass and the kubelet's Secret sync.

The operator makes no GitHub call. The Skills page reports what GitHub returns:

*   A missing repository or ref shows as not found. A private repository also shows as not found when the App installation does not cover it or the App lacks the **Contents: Read-only** permission.
*   A key that GitHub rejects or a GitHub outage shows the Marketplace as unavailable.

## Limits and Caching

skills-service caches each Marketplace's skill list in Redis, separately for each tenant. After 12 hours it checks the commit the ref points at and reads the repository again only when the commit has changed. A change on a tracked branch appears within 12 hours. During a GitHub outage the cached list stays served for up to 24 hours. A failed read of the repository is cached for one minute.

Opening a skill shows its `SKILL.md` and the list of its other files. skills-service fetches each of those files the first time a member selects it and caches it for 12 hours.

The platform sets the crawl limits in the chart's `skillsService.limits` values:

*   **`skillsPerMarketplace`:** 500 skills per Marketplace.
*   **`filesPerSkill`:** 500 files listed per skill.
*   **`bytesPerFile`:** 128 KiB per file.
*   **`skillMdBytesPerMarketplace`:** 8 MiB of `SKILL.md` files per Marketplace.

An empty or zero value removes that limit. Skills and files over a limit are skipped and logged. skills-service reads only files with a text extension (e.g., `.md`, `.py`, `.json`) and never downloads binary files.

Members browse a Marketplace with the `skills:read` scope. See [Granting `skills:read`](tenants.md#granting-skillsread).

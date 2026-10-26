# Tenants

A tenant is kora's unit of separation. It can be used to represent an organisation, a set of teams or a team. Each tenant comes with its own user, agent and resource separation. This allows for isolated deployments that let federated teams manage their own tenancy. Platform administrators can manage across tenant boundaries, whilst tenant admins will only be able to control their own tenancy.

You do not have to run more than one. A single tenant is enough when you do not need separation between groups of users or workloads. Run several when you host distinct organisations, or teams that must not reach each other's agents, credentials and data. 

A tenant is isolated on several axes: 

- **Namespace:** the tenant's agents and resources run in their own `orbit-tenant-<name>` namespace.
- **Identity:** workloads get SPIFFE identities scoped to the tenant, backed by a per-tenant CA. Humans are governed by tenant-scoped `Principal`s.
- **Network:** a default-deny policy and a per-tenant egress policy bind what the tenant's agents and services can reach.
- **Policy:** grants and roles are scoped to the tenant.
- **Configuration:** a tenant's connections, MCP servers, and OAuth clients live in its namespace and apply only to it. 

Users reach their tenant by email domain. When someone signs in, kora routes them to the tenant whose `emailDomains` claim their address, and authenticates them through that tenant's OIDC client.

The following guide assumes kora is already installed. See [Deploying kora](deployment.md) for the install.

## Configuring a tenant

A tenant is defined using the `OrbitTenant` CRD. The kora operator reconciles it, creating the namespace, applying the SPIRE and Istio labels, and publishing the tenant to the identity service.

```yaml
apiVersion: orbit.io/v1alpha1
kind: OrbitTenant
metadata:
  name: example
spec:
  displayName: Example Organisation
  active: true

  oidc:
    issuerURL: https://idp.example.com/oauth2/default
    clientID: orbit-production
    clientSecretRef:
      name: orbit-example-oidc
      namespace: orbit-system
    scopes: [openid, profile, email, groups]
    emailDomains: [example.com]

  deleteNamespaceOnRemoval: false
```

To adopt an existing namespace you can set it explicitly: `spec.namespace`. kora derives `orbit-tenant-<name>` if you omit this field.

A confidential client needs a Secret in the `clientSecretRef` namespace, carrying the client secret under the key `OIDC_CLIENT_SECRET`. Project it through your secret manager, the same as the platform secrets. A public client does not need to set a `clientSecretRef`. For local use you can create a secret directly:

```bash
kubectl create secret generic orbit-example-oidc \
  --namespace orbit-system \
  --from-literal=OIDC_CLIENT_SECRET='<oidc client secret>'
```

An `OrbitTenant` is a declarative resource, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To inspect its status:

```bash
kubectl get orbittenant example -w
kubectl get namespace orbit-tenant-example
```

Set `deleteNamespaceOnRemoval: true` only when deleting the `OrbitTenant` must delete every workload in its namespace.

Once the tenant is ready, configure access for its principals with [policy and roles](policy-and-roles.md).

## Approving skill marketplace repositories

A tenant's [skill marketplace sources](skill-marketplace-sources.md) are served only from repositories you approve on its `OrbitTenant`. Tenant administrators cannot edit `OrbitTenant`, so this list is the platform's control over what each tenant can surface.

```yaml
spec:
  allowedMarketplaceRepos:
    - acme-corp/internal-skills
    - acme-corp/public-docs
```

Each entry is `owner/repo`, matched without regard to case. The skills service always reads a repository's default branch. The API server rejects an entry that is not a valid `owner/repo`. A source on a suspended tenant (`active: false`) stays `Pending` with reason `TenantNotActive`, whatever the list holds.

Before adding an entry:

1. Confirm the request came from the tenant that owns the source's namespace. That is `orbit-tenant-<name>`, or the tenant's `spec.namespace` when it sets one.
2. Confirm the repository is one this tenant may expose. kora reads every marketplace with the platform GitHub App, so an entry lets the tenant's users read the repository's `SKILL.md` files and the markdown documents beside them, private repositories included. The list is the only gate.
3. Review the change like any other access grant, in your GitOps repository.

Apply it with `kubectl edit orbittenant <name>` or through GitOps. The operator re-renders the marketplace list when it sees the change to the `OrbitTenant`, and the source moves from `Pending` to `Aggregated`. Removing an entry takes the source out of the marketplace the same way. kora does not track or notify pending requests, so agree an approval turnaround with your tenants.

Periodically compare each tenant's list with the sources registered against it, and remove entries no source uses. An unused entry lets the tenant register that repository again without asking you:

```bash
kubectl get orbittenant <name> -o jsonpath='{.spec.allowedMarketplaceRepos}'
kubectl get skillmarketplacesources -n orbit-tenant-<name>
```

### Granting `marketplace:read`

Users can browse marketplaces only with the `marketplace:read` scope. `platform-admin` holds every registered scope, so it needs no grant. For everyone else, add the scope to the role your tenant's members carry (see [Managing Roles](policy-and-roles.md#managing-roles)).

`PUT /api/v1/policy/roles` replaces a role's scopes as a whole, so read the role first and send back its current scopes plus the new one. A request that lists only `marketplace:read` removes the role's other scopes.

```
GET /api/v1/policy/roles/engineer
```

SYSTEM principals must include `?tenant_id=acme`. Then, with the scopes the response returned:

```
PUT /api/v1/policy/roles
Authorization: Bearer <platform-token>

{
  "role":   "engineer",
  "scopes": ["inference:invoke", "mcp:gmail:read", "marketplace:read"]
}
```

SYSTEM principals must include `tenant_id` in the body. The request is rejected if `marketplace:read` is not in the scope vocabulary, which you can check with `GET /api/v1/policy/vocabulary/marketplace:read`. The skills service registers the scope when it registers its routes. The change reaches enforcement within one bundle polling cycle, see [Propagation Timing](policy-and-roles.md#propagation-timing).

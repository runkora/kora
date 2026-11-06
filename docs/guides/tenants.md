# Tenants

A tenant is runkora's unit of separation. It can be used to represent an organisation, a set of teams or a team. Each tenant comes with its own user, agent and resource separation. This allows for isolated deployments that let federated teams manage their own tenancy. Platform administrators can manage across tenant boundaries, whilst tenant admins will only be able to control their own tenancy.

You do not have to run more than one. A single tenant is enough when you do not need separation between groups of users or workloads. Run several when you host distinct organisations, or teams that must not reach each other's agents, credentials and data. 

A tenant is isolated on several axes: 

- **Namespace:** the tenant's agents and resources run in their own `kora-tenant-<name>` namespace.
- **Identity:** workloads get SPIFFE identities scoped to the tenant, backed by a per-tenant CA. Humans are governed by tenant-scoped `Principal`s.
- **Network:** a default-deny policy and a per-tenant egress policy bind what the tenant's agents and services can reach.
- **Policy:** grants and roles are scoped to the tenant.
- **Configuration:** a tenant's connections, MCP servers, skill marketplaces, and OAuth clients live in its namespace and apply only to it. 

Users reach their tenant by email domain. When someone signs in, kora routes them to the tenant whose `emailDomains` claim their address, and authenticates them through that tenant's OIDC client.

The following guide assumes kora is already installed. See [Deploying runkora](deployment.md) for the install.

## Configuring a tenant

A tenant is defined using the `KoraTenant` CRD. The kora operator reconciles it, creating the namespace, applying the SPIRE and Istio labels, and publishing the tenant to the identity service.

```yaml
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: example
spec:
  displayName: Example Organisation
  active: true

  oidc:
    issuerURL: https://idp.example.com/oauth2/default
    clientID: kora-production
    clientSecretRef:
      name: kora-example-oidc
      namespace: kora-system
    scopes: [openid, profile, email, groups]
    emailDomains: [example.com]

  deleteNamespaceOnRemoval: false
```

To adopt an existing namespace you can set it explicitly: `spec.namespace`. kora derives `kora-tenant-<name>` if you omit this field. The namespace cannot change once the tenant exists. The operator refuses the system namespace and a namespace another tenant already uses, and holds such a tenant at `Degraded`.

Register `https://<frontendHost>/auth/callback` as a redirect URI on the OIDC client, and allow kora's services to reach the issuer from `kora-system`.

A confidential client needs a Secret in the `clientSecretRef` namespace, carrying the client secret under the key `OIDC_CLIENT_SECRET`. Project it through your secret manager, the same as the platform secrets. A public client does not need to set a `clientSecretRef`. For local use you can create a secret directly:

```bash
kubectl create secret generic kora-example-oidc \
  --namespace kora-system \
  --from-literal=OIDC_CLIENT_SECRET='<oidc client secret>'
```

An `KoraTenant` is a declarative resource, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To inspect its status:

```bash
kubectl get koratenant example -w
kubectl get namespace kora-tenant-example
```

Set `deleteNamespaceOnRemoval: true` only when deleting the `KoraTenant` must delete every workload in its namespace.

Set `spec.events.retentionDays` to keep the tenant's events for that many days. A tenant that does not set it uses the chart value `koraEvents.retention.defaultDays` (30 by default).

To define the tenant's agents, triggers and roles as resources, or to lock the tenant to them with `apiAuthoring`, see [Declarative resources](declarative-resources.md).

Once the tenant is ready, configure access for its principals with [policy and roles](policy-and-roles.md).

## Sharing an email domain

Each email domain reaches one tenant by default. The operator marks a tenant `Invalid` when it claims a domain that an older tenant already holds.

Several tenants can share a domain when they use the same `issuerURL` and `clientID` and each one sets a `claimBinding`. kora signs a person in to the tenant whose binding matches their ID token.

```yaml
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: engineering
spec:
  displayName: Engineering
  oidc:
    issuerURL: https://idp.example.com/oauth2/default
    clientID: kora-production
    scopes: [openid, profile, email, groups]
    emailDomains: [example.com]
    claimBinding:
      claim: groups
      values: [engineering]
---
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: sre
spec:
  displayName: SRE
  oidc:
    issuerURL: https://idp.example.com/oauth2/default
    clientID: kora-production
    scopes: [openid, profile, email, groups]
    emailDomains: [example.com]
    claimBinding:
      claim: groups
      values: [sre]
```

kora chooses the tenant in three steps:

1.  It reads the domain from the ID token's `email` claim and keeps the tenants on the same issuer and client that list the domain in `emailDomains`. Without an `email` claim, it keeps only the tenant the person signed in to.
2.  It checks each tenant's binding against the token. A string claim must equal one of `values`, and a list claim such as `groups` must contain one of them.
3.  It signs the person in to the tenant that matches. When several match, it uses the tenant the sign-in asks for if that tenant is among them.

A person who matches no tenant gets a 403. A person who matches several tenants picks one on the sign-in page and switches between them from the dashboard. A switch signs in again with the identity provider and succeeds only when the person still matches the target tenant. It ends the session it replaces.

Choose the tenant for a CLI sign-in with `--tenant-id`. When the account matches several tenants and `--tenant-id` names none of them, the script prints the matches and asks for a re-run with one of them. The API returns this case as a 409 whose `details.tenants` lists each match's `id` and `display_name`.

```bash
./scripts/auth-device.sh --tenant-id sre
```

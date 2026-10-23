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

An `KoraTenant` is a declarative resource, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To inspect its status:

```bash
kubectl get koratenant example -w
kubectl get namespace orbit-tenant-example
```

Set `deleteNamespaceOnRemoval: true` only when deleting the `KoraTenant` must delete every workload in its namespace.

Once the tenant is ready, configure access for its principals with [policy and roles](policy-and-roles.md).

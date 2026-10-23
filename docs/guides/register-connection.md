# Register a connection

For agents to be effective, they often need to reach external services. kora grants this access through a `Connection`, which brokers the agent's request. The egress gateway intercepts the agent's outbound call and injects the credential in flight, provisioning for the upstream service just-in-time. The agent never holds the secret and cannot exfiltrate it.

Connections are namespaced. Apply one in a tenant namespace for that tenant to use.

The following guide assumes kora is already installed. See [Deploying kora](deployment.md) for the install.

## Configuring a connection

A connection is defined using the `Connection` CRD. The operator folds it into the egress-gateway config. Agents can then be granted access to the connection within the tenant.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Connection
metadata:
  name: github
  namespace: orbit-tenant-example
spec:
  adapter: github_app
  baseHost: github.com
  secretRef: github-app-key
  secretKey: key.pem
  config:
    app_id: "4015347"
    installation_id: 139299720
    owner: clear-route
```

- `adapter`: selects the credential mechanism. See [Adapters](#adapters) for the available adapters and their config.
- `baseHost`: the upstream host the gateway terminates and re-originates, such as `github.com`.
- `secretRef`: the name of a Secret in the same namespace holding the credential material. `secretKey` is used to target a key within the Secret when there is more than one key.
- `config`: adapter-specific settings that are passed to the gateway verbatim in snake_case. Each adapter's keys are listed under [Adapters](#adapters).

A `Connection` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To list what a tenant has:

```bash
kubectl get connections -n orbit-tenant-example
```

## Adapters

The `adapter` selects how the gateway obtains the upstream credential. kora provides a default `passthrough` for all whitelisted egress that does not require token injection. The available adapters:

### github_app

Authenticates an agent to GitHub as a GitHub App installation. `secretRef` should hold the App's private key PEM.

```yaml
config:
  app_id: (string) the GitHub App's ID.
  installation_id: (number) the installation on your organisation or repositories.
  owner: (string) the org or user the App is installed on. Orbit rejects requests addressed to any other owner.
  permissions: (map) optional ceiling, intersected with the permissions each action derives.
```

The connection does not name repositories. An agent is narrowed to specific repositories when a connector is attached to it, which compiles to a qualified grant; the App installation bounds everything beyond that.

A tenant can hold one `github_app` connection per GitHub organization, each with its own `owner`. A request whose path names an owner, such as a clone or `/repos/{owner}/...`, uses the connection with that `owner`. A GraphQL request uses the connection of the owners its document names, or of the objects its node ids identify. A search request uses the connection of its `repo:`, `org:` or `user:` qualifiers. A request tied to no owner, such as a `viewer` query, uses the one connection the agent is granted. An agent granted several of them is denied that request with the reason `connection_unresolved`, and the denial states which part could not be tied to an owner. The operator rejects a second `github_app` connection on the same host that has no `owner` or repeats one, and a connection of another adapter on a host an existing connection already serves.

### kubernetes

Authenticates an agent to a Kubernetes API server as a ServiceAccount. `secretRef` should hold the bearer token under `secretKey: token`. The adapter reads no `config`.

Set `caCert` to the cluster's CA certificate in PEM. It is a connection field rather than adapter config, because the gateway makes the outbound TLS connection and no adapter dials the upstream.

Extract it from your kubeconfig:

```bash
kubectl config view --raw --minify \
  -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d
```

`baseHost` must be a DNS hostname, not a bare IP. TLS to an IP literal carries no SNI, and the sandbox's egress interception cannot authorize it.

See [connectivity](../architecture/connectivity.md) and [egress gateway](../architecture/egress-gateway.md) for how credentials are brokered.

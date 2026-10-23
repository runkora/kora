# OAuth clients

kora exposes an OAuth authorization server. A hosted application can send its users to kora to sign in and receive a token to act on their behalf against kora's APIs. An `OAuthClient` pre-registers that application: its client ID and the exact redirect URIs kora will return users to.

Every client is public and uses PKCE. A platform owner registers an `OAuthClient` through a CR rather than the app self-registering at runtime. Unlike dynamically-registered clients, it may declare non-loopback HTTPS redirect URIs.

The following guide assumes kora is already installed. See [Deploying kora](deployment.md) for the install.

## Configuring an OAuth client

An OAuth client is defined with the `OAuthClient` CRD. The operator aggregates it into the identity service config, registering the client with the authorization server.

```yaml
apiVersion: runkora.io/v1alpha1
kind: OAuthClient
metadata:
  name: example-app
spec:
  clientId: example-app
  clientName: Example App
  redirectUris:
    - https://app.example.com/auth/callback
    # Loopback redirect for local development (RFC 8252 exact-match exception).
    - http://localhost:3000/auth/callback
```

- `clientId`: the identifier the app presents at the authorize and token endpoints. It must be unique across the whole authorization server, since the identity service resolves clients by client ID alone, not per tenant.
- `clientName`: the display name shown on the consent screen. Optional.
- `redirectUris`: the exact permitted redirect URIs. The authorize endpoint requires an exact string match, with no prefix or wildcard, which is what makes non-loopback URIs safe. At least one is required.

An `OAuthClient` is declarative, applied the way you manage the rest of your cluster. Commit it to your GitOps repository, or `kubectl apply` it for a quick start. To list what is registered:

```bash
kubectl get oauthclients -A
```

See [identity](../architecture/identity.md) for kora's identity and token model.

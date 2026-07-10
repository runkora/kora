# Local Development Guide

Orbit runs in Kubernetes locally, the same way it runs anywhere else. There is one environment. There is no host-process mode ([kubernetes-only-local-development](../adr/2026-07-10-kubernetes-only-local-development.md)).

This is the deep reference. For the shortest path from clone to a running change, read [day-one.md](day-one.md) first.

---

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | control plane |
| Docker Desktop or Colima | 4 CPUs, 8 GiB allocated to the VM | runs the cluster and the backing services |
| Node | 24 (`>=24 <25`) | frontend host checks; `nvm install 24 && nvm use` |
| pnpm | 10 (`>=10 <11`) | `corepack enable` |
| Make | system | pre-installed on macOS and Linux |

`make setup` installs the rest: `goose`, `golangci-lint`, `gitleaks`, `buf`, `k3d`, `kubectl`, `helm`.

---

## Setup

Run once per machine. Safe to re-run.

```bash
make setup
```

`make setup` composes:

| Step | Does |
|---|---|
| `env` | Generates `.env` with machine-local secrets. No-op if `.env` exists. |
| `install-tools` | Installs `goose`, `golangci-lint`, `gitleaks`, `buf`, protoc plugins. |
| `install-tools-cluster` | Installs `k3d`, `kubectl`, `helm`. |
| `tidy` | `go mod tidy`. |
| `frontend-install` | Enforces Node 24 and pnpm 10, runs `pnpm install`. |
| `hooks` | Installs the git pre-commit hooks. |
| `setup-dns` | Configures `*.orbit.local.clearroute.io` resolution on macOS via dnsmasq. No-op on Linux. |
| `setup-tls` | Generates mkcert TLS certs for the local domain. |

---

## Bring the environment up

```bash
make up
```

Idempotent. Run it again whenever the cluster drifts. It composes, in order:

| Step | Does |
|---|---|
| `dev-up` | Starts Postgres, Redis, glauth, and Dex in Docker Compose. On first run, generates `secrets/signing.key` and `config/dex/.env` if absent. |
| `dev-cluster` | Creates the k3d cluster `orbit-local` with a registry at `orbit-registry:5001`, and patches CoreDNS so pods resolve `dex.orbit.local.clearroute.io`. |
| `migrate-up` | Applies every service's Goose migrations. |
| `cluster-operator` | Builds and installs the `orbit-operator`, which reconciles the `OrbitPlatform` CR: SPIRE, Istio, the gateway, `orbit-platform`, and the Orbit services. |
| `cluster-deploy` | Runs `create-secrets.sh`, applies the `OrbitTenant` CRs, and deploys the workload fixtures. |

Each composed step is a standalone target. Run any one on its own to drive a single stage.

Endpoints once `make up` finishes:

- Frontend: `https://orbit.local.clearroute.io:8443`
- Inference gateway: `https://api.orbit.local.clearroute.io:8443/v1`

Tear it all down:

```bash
make down          # cluster + backing services
make dev-down-clean # also drops the Postgres and Redis volumes
```

---

## Authenticate

```bash
make auth
```

Runs the Device Authorization Grant against the cluster. Open the printed URL, enter the code, and log in as `dev@orbit.local.clearroute.io` with password `password`. The token pair is written to `/tmp/orbit_credentials.json`.

### Call an authenticated API

`$ORBIT_TOKEN` is not exported into subprocess shells. Read the token inline. The local certs are from a private CA, so `curl` needs `-k`:

```bash
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://api.orbit.local.clearroute.io:8443/v1/models | jq .
```

Access tokens last 4h locally, set by `accessTokenTtlMinutes` in `deploy/local/orbit-values.yaml`. When one expires:

```bash
make auth-refresh   # rotates the token pair in place
```

If the refresh token is dead too, run `make auth` again.

---

## The inner loop: redeploy

There is no hot-reload. A change ships as a docker build, a push to the local registry, and a rollout.

```bash
make redeploy SERVICE=identity   # rebuild and roll one service
make redeploy                    # rebuild and roll every service
```

| Command | Does |
|---|---|
| `make redeploy SERVICE=<name>` | Build image, push to `orbit-registry:5001`, roll the workload. |
| `make redeploy` | The same, for every service. |
| `make redeploy SERVICE=<name> NO_BUILD=1` | Roll the current image only, skip the build. |
| `make redeploy SERVICE=session-manager TENANT=example` | Roll a tenant-scoped workload. |
| `make redeploy-list` | List every service `make redeploy` knows. |
| `make redeploy-status` | Show drift between cluster-deployed images and the local registry. |
| `make redeploy-warm` | Prebuild every image so the BuildKit caches are hot. Run once at the start of a session. |
| `make cluster-update` | Rebuild every image and roll every workload. |

The full per-service loop, including the shortcut targets, lives in [deploy/local/README.md](../../deploy/local/README.md).

Frontend changes ship the same way:

```bash
make redeploy SERVICE=frontend
```

See [frontend/README.md](../../frontend/README.md) for the frontend workflow and host-side lint, type-check, and test loops.

---

## Database migrations

Orbit uses [Goose](https://github.com/pressly/goose). Each service owns its migrations under `backend/cmd/{service}/migrations/`.

**Never write migration files by hand.** Generate them:

```bash
make migrate-create service=orbit-agents name=add_agent_sessions
```

Both `service=` and `name=` are required. This writes a correctly timestamped file with the `-- +goose Up` and `-- +goose Down` annotations pre-populated.

| Command | Does |
|---|---|
| `make migrate-up` | Apply all pending migrations across every service. |
| `make migrate-down` | Roll back the last applied migration. |
| `make migrate-status` | Show applied and pending migrations. |
| `make migrate-create service=X name=Y` | Generate a new migration file for a service. |

`make up` runs `migrate-up`. Re-run it after pulling changes that add migration files.

---

## Environment configuration

`.env` is generated by `make setup` and is machine-local and gitignored. It holds only the values that cannot have a shared default:

| Variable | Source | Purpose |
|---|---|---|
| `DB_PASSWORD` | Fixed default `orbit` | Postgres password. Baked into the volume at first initialisation, so changing it needs `make dev-down-clean` first. |
| `REDIS_PASSWORD` | Fixed default `orbit` | Redis password. |
| `BFF_SESSION_SECRET` | Generated with `openssl rand -base64 32` | Signs the frontend iron-session cookie. Written into `orbit-frontend-secrets`. |
| `ORBIT_TENANT_EXAMPLE_OIDC_CLIENT_SECRET` | Generated with `openssl rand -hex 32` | Confidential-client secret for the example tenant. Unused in the default public-client setup. |

`create-secrets.sh` reads `DB_PASSWORD`, `REDIS_PASSWORD`, and `BFF_SESSION_SECRET` from `.env` and, together with `secrets/signing.key`, writes them into `orbit-secrets` and `orbit-frontend-secrets` in the cluster.

Everything else has a default and does not appear in `.env`:

- Compose settings (`DB_PORT`, `DB_USER`, `DB_NAME`, `REDIS_PORT`) default inside `docker-compose.yml`.
- In-cluster service settings, including the token TTL, come from `deploy/local/orbit-values.yaml`.

`.env.example` is not the source of `.env`. It documents the optional overrides you can copy into `.env` when you need them:

| Variable | Purpose |
|---|---|
| `BEDROCK_ENABLED`, `AWS_PROFILE`, `BEDROCK_REGION` | Enable the Bedrock provider. `make cluster-bedrock-creds` reads these to refresh the cluster's AWS credentials from your SSO session. |
| `K3D_FIX_DNS` | Docker Desktop only: patch node DNS on cluster creation when `host.docker.internal` does not resolve. |
| `DB_PORT`, `DB_USER`, `DB_NAME`, `DB_PASSWORD`, `REDIS_PORT`, `REDIS_PASSWORD` | Override compose defaults on port collisions. |
| `ORBIT_TENANT_ID` | Tenant used by `make auth`. Defaults to `example`. |

The signing key is a file, not an env var: `secrets/signing.key` holds a 4096-bit RSA private key, generated on first `make up`. Regenerate it with `make gen-signing-key`.

---

## Running tests

```bash
make test              # unit tests, no cluster required
make test-integration  # live-cluster authz tests
```

`make test` is what `make check` and CI run. It needs no cluster: the identity service's SVID validator is stubbed and OIDC flows are not exercised. It is the fastest feedback available.

`make test-integration` (`//go:build integration`) is **not** run by CI or `make check`. It drives the full SPIFFE SVID exchange against the running cluster and refreshes the k3d token first. Bring the environment up before running it:

```bash
make up
make test-integration
```

### Postgres-backed repository tests

Repository tests that hit a real database are gated on `ORBIT_TEST_DB_URL` and skip when it is unset, so they stay out of `make test`. Point it at the local Postgres after `make up`:

```bash
ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
  go test ./internal/triggers/ -run Postgres
```

---

## Using a real OIDC provider

OIDC configuration is per-tenant, defined in the tenant registry, not via `ORBIT_OIDC_*` variables ([tenant-registration-and-oidc-provider-binding](../adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md)).

Local tenants are `OrbitTenant` CRs under `deploy/local/tenants/`, reconciled into the `orbit-tenants` ConfigMap by the operator. To bind a tenant to Microsoft Entra or any OIDC-compliant provider, edit the tenant's `oidc` block and re-apply:

```yaml
spec:
  oidc:
    issuerURL: "https://login.microsoftonline.com/{tenant-id}/v2.0"
    clientID: "{your-application-client-id}"
    emailDomains: ["yourcompany.com"]
    scopes: [openid, profile, email, groups]
```

```bash
kubectl apply -f deploy/local/tenants/example.yaml
```

The backend discovers the provider's endpoints from its `/.well-known/openid-configuration` document, so no code change is needed. The local Dex client is public and needs no secret. Confidential clients are handled by the operator ([oidc-client-secret-projection-for-confidential-tenants](../adr/2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md)).

> **Entra application requirements**: the registered application must have the `urn:ietf:params:oauth:grant-type:device_code` grant type enabled under Authentication, Advanced settings, Allow public client flows.

---

## Driving the Orbit MCP locally

The cluster runs `orbit-mcp` behind the governed tool gateway. The project-scoped `.mcp.json` points Claude Code at it over the gateway's MCP OAuth endpoint:

```json
{ "mcpServers": { "orbit": { "url": "https://api.orbit.local.clearroute.io/v1/mcp/orbit", "type": "streamable-http" } } }
```

In a Claude Code session, run `/mcp` and authenticate the `orbit` server. Claude Code runs the MCP OAuth flow (dynamic client registration and PKCE) and opens the consent screen in your browser. Because you are already signed into the dashboard it is a silent SSO, so you approve the tools being granted and it connects. The token refreshes silently, and you can revoke it any time from the dashboard's Connections. Requires the cluster up and a dashboard browser session.

---

## Troubleshooting

### `orbit-dex` container is unhealthy

`config/dex/.env` is missing or holds an invalid bcrypt hash. `make up` generates it on first run, but it can become stale.

```bash
make dex-hash password=password
make dev-down
make up
```

### `https://orbit.local.clearroute.io:8443` is unreachable

The wildcard DNS record `*.orbit.local.clearroute.io -> 127.0.0.1` is missing.

```bash
dig +short orbit.local.clearroute.io   # should return 127.0.0.1
make setup-dns                         # reconfigures the record and prints fixes
```

### Pod cannot reach Postgres: `connection refused`

A stale k3d cluster whose CoreDNS lost the Compose-service route. Rebuild rather than debug Istio:

```bash
make down
make up
```

### Backend pod fails to start: `failed to parse RSA private key`

`secrets/signing.key` is missing or corrupt.

```bash
make gen-signing-key
make redeploy SERVICE=identity
```

### Migration fails: `relation does not exist`

```bash
make migrate-status   # check what is applied
make migrate-up       # apply pending migrations
```

### `make auth` times out waiting for authorization

The device code expired before you completed the browser step. Run `make auth` again and enter the code promptly.

---

## Related documentation

- [day-one.md](day-one.md): the shortest path from clone to a running change.
- [kubernetes-only-local-development](../adr/2026-07-10-kubernetes-only-local-development.md): why local development is Kubernetes-only.
- [deploy/local/README.md](../../deploy/local/README.md): the cluster deploy sequence and the per-service redeploy loop.
- [code-organization.md](../code-organization.md): the binary map and feature layout.
- [orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md): the operator design.
- [Configuration reference](../configuration/config.md): the YAML config system.
- [Secrets reference](../configuration/secrets.md): how secrets are managed.
</content>
</invoke>

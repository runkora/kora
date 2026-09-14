# Local development

Local development runs Orbit in a k3d cluster on your machine ([kubernetes-only-local-development](../adr/2026-07-10-kubernetes-only-local-development.md)).

## Prerequisites

- Go 1.26 or later
- Docker Desktop or Colima, with a minimum of 4 CPUs and 8 GiB allocated to the VM
- Make
- nvm
- Homebrew, on macOS

`make setup` installs Node (`>=24 <25`) and pnpm (`>=10 <11`) through nvm and Corepack, plus `golangci-lint`, `gitleaks`, `buf`, `mkcert`, `k3d`, `kubectl`, and `helm`.

It also runs `mkcert -install`, which adds a local CA to your system trust store and prompts for your password once. The CA makes `https://orbit.local.clearroute.io` trusted in your browser. On Linux without `mkcert`, certificates are skipped and `make up` generates a self-signed fallback.

## Set up

```bash
make setup
```

- `env`: Generates `.env` with machine-local secrets. No-op if `.env` exists.
- `install-tools`: Installs `golangci-lint`, `gitleaks`, `buf`, protoc plugins.
- `install-tools-cluster`: Installs `k3d`, `kubectl`, `helm`, `helmfile` and the `helm-diff` plugin.
- `tidy`: `go mod tidy`.
- `frontend-install`: Enforces Node 24 and pnpm 10, runs `pnpm install`.
- `hooks`: Installs the git pre-commit hooks.
- `setup-dns`: Verifies `*.orbit.local.clearroute.io` resolves. It is a public wildcard record. Nothing to configure.
- `setup-tls`: Generates mkcert TLS certificates for the local domain.

## Start Orbit

```bash
make up
```

`make up` is idempotent. Run it again whenever the cluster drifts. It runs the following steps:

- `dev-up`: Starts Postgres, Redis, glauth, and Dex in Docker Compose. Generates `secrets/signing.key` on first run.
- `dev-cluster`: Creates the k3d cluster `orbit-local` with a registry at `orbit-registry:5001`, and patches CoreDNS so pods resolve `dex.orbit.local.clearroute.io`.
- `cluster-platform`: Builds any missing service images, creates the `orbit-system` namespace, secrets and TLS, writes the Docker network values to `deploy/reference/environments/local.generated.yaml`, then runs `helmfile -e local apply` from `deploy/reference/`. That installs SPIRE, Istio and the two Orbit charts in dependency order, the same sequence on any cluster. The `orbit` chart's pre-install hook applies the database schema before any service starts. Helmfile diffs before applying, so an unchanged environment is a no-op.
- `cluster-deploy`: Runs `create-secrets.sh`, applies the `OrbitTenant` resources, and deploys the workload fixtures.

Endpoints once it finishes:

- Dashboard: `https://orbit.local.clearroute.io`
- Backend API: `https://api.orbit.local.clearroute.io`

An HAProxy container maps host 443 and 80 to the k3d load balancer. If something else on your machine holds port 443 it will not start. The cluster is then reachable on `:8443` directly.

Tearing down:

```bash
make down            # cluster and backing services preserving data
make reset           # deletes the cluster and builds it again preserving data
make dev-down-clean  # DESTRUCTIVE: stops Postgres and Redis and deletes their volumes
```

## Sign in

```bash
make auth
```

Runs the Device Authorization Grant. Open the printed URL, enter the code, and log in. The token pair is written to `/tmp/orbit_credentials.json`.

Local accounts are backed by `glauth` and surfaced to Dex over LDAP. Their default password is `password`.

| Account | Roles | Use it for |
|---|---|---|
| `dev@orbit.local.clearroute.io` | `platform-admin`, `tenant-admin`, `engineer` | The admin. Policy administration and everything else. |
| `engineer@orbit.local.clearroute.io` | `engineer` | Checking what a regular engineer can and cannot do. |

Access tokens last four hours locally, set by `identity.accessTokenTtlMinutes` in `deploy/reference/environments/local.yaml`. `make auth-refresh` rotates the pair when one expires. If the refresh token has expired too, run `make auth` again.

## Using your token

`/tmp/orbit_credentials.json` holds the access and refresh token pair. Anything that calls the platform outside the browser reads the access token from there.

`$ORBIT_TOKEN` is not exported into subprocess shells. Read it from the file.

The local certificates come from a private CA. `curl` needs `-k`, and any other client needs the mkcert root or TLS verification off.

A `403` on a route you expected to reach means your principal lacks its scope. [Policy and roles](policy-and-roles.md) covers grants, roles, and the `tenant-admin` bootstrap.

## Deploying a change

Changes are applied using the redeploy recipe. The cluster does not hot-reload your local changes. `redeploy` applies any pending migrations and then deploys the service(s).

```bash
make redeploy SERVICE=identity   # rebuild and roll one service
make redeploy                    # rebuilds and rolls all services
make test                        # unit tests, no cluster needed
make check                       # run the full verify gate
```

## Redeploy reference

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

## Changing platform values

Edit `deploy/reference/environments/local.yaml`, then re-apply from `deploy/reference/`:

```bash
helmfile -e local apply --selector name=orbit
```

`helmfile -e example apply` renders the same charts against a starting point for a real cluster (`deploy/reference/environments/example.yaml`), not the local one.

### Parallel builds on high-spec machines

Full rebuilds build each image serially by default. If your Docker VM has headroom, opt in through `.env`:

```bash
# .env
ORBIT_PARALLEL_BUILDS=1
```

With the flag set, `make redeploy-platform` runs one `docker buildx bake` graph over every platform image. BuildKit builds them concurrently and deduplicates the shared Go module and source layers. Single-service redeploys are unaffected.

A healthy VM size for Orbit is 12 CPUs and 24 GiB. To set this in Colima:

```bash
colima stop && colima start --cpu 12 --memory 24   # persists in ~/.colima
```

On smaller VMs, leave the flag unset. Concurrent build pressure can wedge the VM and cascade into mesh and Postgres failures.

## Database migrations

Each table-owning service keeps its [Goose](https://github.com/pressly/goose) migrations under `backend/cmd/{service}/migrations/`. `backend/cmd/migrate` embeds all of them and applies them as one set. In a cluster it runs as the `orbit` chart's pre-install and pre-upgrade hook Job; the same binary runs locally through `go run`. See the [database migrations ADR](../adr/2026-09-10-database-migrations.md).

**Never write migration files by hand.** Generate them:

```bash
make migrate-create service=orbit-agents name=add_agent_sessions
```

Both `service=` and `name=` are required. It writes a timestamped file with the `-- +goose Up` and `-- +goose Down` annotations pre-populated.

| Command | Does |
|---|---|
| `make migrate-create service=X name=Y` | Generate a new migration file for a service. |
| `make migrate-lint` | Check every migration against the content rule. Part of `make check`. |
| `make migrate-status` | Show applied and pending migrations against the compose Postgres. |
| `make redeploy SERVICE=migrate` | Rebuild the migrate image and re-apply the `orbit` release so the hook runs. |

`make up` applies the schema through the hook, including after `make db-reset`: the reset stamps a marker annotation on the Job so the next `make up` re-runs it against the empty database. `make redeploy` of any service first checks whether the migration files changed since the migrate image was last pushed (a content hash recorded in `local.generated.yaml`) and re-runs the hook when they did. A branch that adds a migration never boots a service against a stale schema. `make redeploy-migrate` forces the hook.

Migrations are DDL only: no `INSERT` or `DELETE`, an `UPDATE` only alongside an `ALTER TABLE` on the same table in the same file, nothing in `Down` that changes rows, and no statement that touches a table another service creates. `make migrate-lint` rejects violations. Seeds and reference data are registered at boot by the service that owns them.

`Down` sections exist for local development only. To roll back one migration against the compose Postgres:

```bash
cd backend && ORBIT_MIGRATE_ALLOW_DOWN=1 APP_ENV=local DB_HOST=localhost DB_NAME=orbit DB_USER=orbit DB_PASSWORD=orbit go run ./cmd/migrate down
```

Production never runs `Down`; rollback is `helm rollback` with the schema left in place.

## Environment configuration

`.env` is generated by `make setup`, machine-local, and gitignored. It holds the values that cannot have a shared default:

| Variable | Source | Purpose |
|---|---|---|
| `DB_PASSWORD` | Fixed default `orbit` | Postgres password. Baked into the volume at first initialisation. Changing it requires `make dev-down-clean` first. |
| `REDIS_PASSWORD` | Fixed default `orbit` | Redis password. |
| `BFF_SESSION_SECRET` | Generated with `openssl rand -base64 32` | Signs the frontend iron-session cookie. Written into `orbit-frontend-secrets`. |
| `ORBIT_TRIGGERS_ENCRYPTION_KEY` | Generated with `openssl rand -base64 32` | Encrypts stored trigger credentials. `orbit-triggers` fails to start without it. |

`create-secrets.sh` reads these from `.env` and, together with `secrets/signing.key`, writes them into `orbit-secrets` and `orbit-frontend-secrets` in the cluster.

`.env.example` lists the optional overrides. Copy a line into `.env` only when you need to change it.

The signing key is a file rather than an env var. `secrets/signing.key` holds a 4096-bit RSA private key, generated on first `make up`. Regenerate it with `make gen-signing-key`.

## Running tests

```bash
make test              # unit tests, no cluster required
make test-integration  # live-cluster authz tests
```

`make test-integration` (`//go:build integration`) is not run by CI or `make check`. It drives the full SPIFFE SVID exchange against the running cluster and refreshes the k3d token first. Bring the environment up before running it:

```bash
make up
make auth # test-integration refreshes an existing token
make test-integration
```

### Postgres-backed repository tests

Repository tests that hit a real database are gated on `ORBIT_TEST_DB_URL` and skip when it is unset. `make test-postgres` runs them against the local Postgres.

```bash
make test-postgres
```

## Using a real OIDC provider

OIDC configuration is per-tenant, defined in the tenant registry.

Local tenants are `OrbitTenant` resources under `deploy/local/tenants/`, reconciled into the `orbit-tenants` ConfigMap by the operator. To bind a tenant to Microsoft Entra or any OIDC-compliant provider, edit the tenant's `oidc` block and re-apply:

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

The backend discovers the provider's endpoints from its `/.well-known/openid-configuration` document.

> **Entra application requirements**: the registered dev application must have the `urn:ietf:params:oauth:grant-type:device_code` grant type enabled under Authentication, Advanced settings, Allow public client flows for local development. Only `make auth` requires this.

## Driving the Orbit MCP locally

The cluster runs `orbit-mcp` behind the tool gateway. With Orbit running, point an MCP client at the gateway's OAuth endpoint:

```json
{
  "mcpServers": {
    "orbit": {
      "url": "https://api.orbit.local.clearroute.io/v1/mcp/orbit",
      "type": "streamable-http"
    }
  }
}
```

The client runs the MCP OAuth flow and opens the consent screen in your browser. Approve the tools and it connects. Revoke the token from the dashboard's Connections page.

## Registering a Kubernetes connection

`deploy/local/connections/kubernetes.yaml` is a template Connection CR for the `kubernetes` adapter. Copy it, fill in the `${placeholder}` fields, and work through these steps before applying.

1. **Name it.** `metadata.name` identifies the target cluster (not the adapter); a tenant can register more than one `adapter: kubernetes` connection.
2. **Provision a bearer token** against the target cluster, with least-privilege RBAC:
   ```bash
   kubectl create serviceaccount orbit-agent -n default --context <target-cluster-context>
   kubectl create clusterrolebinding orbit-agent-view \
     --clusterrole=view --serviceaccount=default:orbit-agent --context <target-cluster-context>
   kubectl create token orbit-agent --duration=8760h --context <target-cluster-context>
   ```
   Store it in your local k3d cluster, where Orbit runs, under the name `spec.secretRef` points at:
   ```bash
   kubectl create secret generic ${cluster}-token -n orbit-tenant-example \
     --from-literal=token=<ServiceAccount token> --dry-run=client -o yaml | kubectl apply -f -
   ```
   `make cluster-deploy` applies the shipped CR only once `orbit-local-token` exists; a copied CR under another name is applied by hand.
3. **Get the target cluster's CA cert** and put it inline in `spec.caCert` (it's not a secret):
   ```bash
   kubectl config view --raw \
     -o jsonpath='{.clusters[?(@.name=="<target-cluster-context>")].cluster.certificate-authority-data}' \
     | base64 -d
   ```
   Testing against the local k3d cluster itself instead of a real target: `docker exec k3d-orbit-local-server-0 cat /var/lib/rancher/k3s/server/tls/server-ca.crt`, and `baseHost: k3d-orbit-local-server-0:6443` (resolvable inside the cluster once `make cluster-platform` has patched CoreDNS).

## Troubleshooting

See [troubleshooting](troubleshooting.md).

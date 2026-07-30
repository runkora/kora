# Local Development Guide

Orbit runs in Kubernetes locally, the same way it runs anywhere else. There is one environment. There is no host-process mode ([kubernetes-only-local-development](../adr/2026-07-10-kubernetes-only-local-development.md)).

This is the deep reference. For the shortest path from clone to a running change, read [day-one.md](day-one.md) first.

---

## Prerequisites

Install these yourself:

| Tool | Version | Why |
|---|---|---|
| Go | 1.26+ | the control plane, and `make setup` uses `go install` |
| Docker Desktop or Colima | 4 CPUs, 8 GiB allocated to the VM | runs the cluster and the backing services |
| Make | system | pre-installed on macOS and Linux |
| nvm | any | `make setup` installs Node through it, and exits if it is missing |
| Homebrew | any | macOS only; `make setup` installs `mkcert` with it |

`make setup` installs the rest: Node (`>=24 <25`) and pnpm (`>=10 <11`) via nvm and Corepack, plus `goose`, `golangci-lint`, `gitleaks`, `buf`, `mkcert`, `k3d`, `kubectl` and `helm`.

It also runs `mkcert -install`, which adds a local CA to your system trust store and prompts for your password once. That CA is what makes `https://orbit.local.clearroute.io` trusted in your browser.

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
| `setup-dns` | Verifies `*.orbit.local.clearroute.io` resolves. It is a public wildcard record, so there is nothing to configure. |
| `setup-tls` | Generates mkcert TLS certs for the local domain. |

---

## Bring the environment up

```bash
make up
```

Idempotent. Run it again whenever the cluster drifts. It composes, in order:

| Step | Does |
|---|---|
| `dev-up` | Starts Postgres, Redis, glauth, and Dex in Docker Compose. Generates `secrets/signing.key` on first run. |
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

Local dev has two accounts, both with password `password`, backed by `glauth` and surfaced to Dex over LDAP:

| Account | OIDC roles | Use it for |
|---|---|---|
| `dev@orbit.local.clearroute.io` | `platform-admin`, `tenant-admin`, `engineer` | The admin. Policy administration and everything else. |
| `engineer@orbit.local.clearroute.io` | `engineer` | Checking what a regular engineer can and cannot do. |

`tenant-admin` is the bootstrap role: it confers `platform:grants:write` without a prior grant, so `dev` can create the first grants. Role-to-scope mappings live in `backend/internal/policyservice/bundle/compiler.go` for the built-in roles, and in the `policy_role_scopes` table for custom roles such as `engineer`.

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

`redeploy` applies any pending migrations first, so a branch that adds one needs no extra step from you.

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

### Parallel builds on high-spec machines

Full rebuilds (`make redeploy`, `make cluster-update`, first `make up`) build each image serially by default, which is safe on any machine but slow. If your Docker VM has real headroom, opt in to parallel builds in `.env`:

```bash
# .env
ORBIT_PARALLEL_BUILDS=1
```

With the flag set, `make redeploy-platform` runs one `docker buildx bake` graph over every platform image. BuildKit builds them concurrently and deduplicates the shared Go module and source layers, so the shared compile work happens once instead of thirteen times. Single-service redeploys (`make redeploy SERVICE=<name>`) are unaffected.

Size the VM before enabling it: at least 12 CPUs and 24 GiB. With Colima:

```bash
colima stop && colima start --cpu 12 --memory 32   # persists in ~/.colima
```

On smaller VMs (≤8 CPUs / 16 GiB), leave the flag unset: concurrent build pressure has previously wedged the VM and cascaded into mesh and Postgres failures. The decision record is [ci-pipeline-and-local-build-parallelisation](../adr/2026-07-21-ci-pipeline-and-local-build-parallelisation.md).

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

You should not need to run any of these by hand. `make up` and `make redeploy` both apply pending migrations first, so pulling a branch that adds one and redeploying just works. The manual targets are there for when you want to inspect or roll back.

---

## Environment configuration

`.env` is generated by `make setup` and is machine-local and gitignored. It holds only the values that cannot have a shared default:

| Variable | Source | Purpose |
|---|---|---|
| `DB_PASSWORD` | Fixed default `orbit` | Postgres password. Baked into the volume at first initialisation, so changing it needs `make dev-down-clean` first. |
| `REDIS_PASSWORD` | Fixed default `orbit` | Redis password. |
| `BFF_SESSION_SECRET` | Generated with `openssl rand -base64 32` | Signs the frontend iron-session cookie. Written into `orbit-frontend-secrets`. |

`create-secrets.sh` reads `DB_PASSWORD`, `REDIS_PASSWORD`, and `BFF_SESSION_SECRET` from `.env` and, together with `secrets/signing.key`, writes them into `orbit-secrets` and `orbit-frontend-secrets` in the cluster.

Everything else has a default and does not appear in `.env`:

- Compose settings (`DB_PORT`, `DB_USER`, `DB_NAME`, `REDIS_PORT`) default inside `docker-compose.yml`.
- In-cluster service settings, including the token TTL, come from `deploy/local/orbit-values.yaml`.

`.env.example` is not the source of `.env`. It documents the optional overrides you can copy into `.env` when you need them:

| Variable | Purpose |
|---|---|
| `BEDROCK_ACCOUNTS`, `BEDROCK_<ALIAS>_REGION`, `AWS_PROFILE` | Enable Bedrock providers. Set `BEDROCK_ACCOUNTS=us,aus` and one `BEDROCK_<ALIAS>_REGION` per alias. `make up` writes these into the cluster; `make cluster-bedrock-creds` refreshes credentials from your SSO session. |
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
make auth              # test-integration refreshes an existing token; it cannot create one
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

### Looking at a failing pod

Orbit services run as pods with sidecars, so "the service is broken" usually means one container is broken. Find out which.

```bash
kubectl get pods -n orbit-system            # what is not Running
kubectl get pods -n orbit-tenant-example    # tenant workloads

# Per-container readiness and restart counts. A pod can be 2/3 Ready and useless.
kubectl get pod <pod> -n orbit-system \
  -o jsonpath='{range .status.containerStatuses[*]}{.name}{"\t"}{.ready}{"\t"}{.restartCount}{"\n"}{end}'

kubectl logs <pod> -n orbit-system -c <container>              # current logs
kubectl logs <pod> -n orbit-system -c <container> --previous   # why it died last time
kubectl describe pod <pod> -n orbit-system                     # events: scheduling, image pulls, evictions
```

`--previous` is the one that matters in a crash loop. The running container's logs are from the attempt that has not failed yet.

Every meshed pod carries an `istio-proxy`, and Orbit workloads also carry `identity-proxy`. If the app container is healthy but the pod is not, read the sidecar's logs.

Port-forwarding into a meshed pod is blocked by mTLS. To call a service directly, exec into its Envoy and curl from inside:

```bash
kubectl exec <pod> -n orbit-system -c istio-proxy -- curl -s localhost:8080/health
```

When a service is running the wrong build, `make redeploy-status` shows what is deployed against what is in the registry. When the cluster itself is wedged, `make reset` rebuilds it and keeps your data.

### `make auth` fails with `oidc_not_configured`

`orbit-identity` builds its OIDC providers at startup. `cluster-operator` starts it before `cluster-deploy` registers the tenant, so on a cold cluster it boots with no tenants and logs:

```
no tenants in registry — federated authentication disabled
```

It watches the tenant registry ConfigMap to recover. That watcher fails when the Docker VM is out of inotify instances:

```
failed to create fsnotify watcher — hot-reload disabled  error="too many open files"
```

Docker's Linux VM defaults `fs.inotify.max_user_instances` to 128, which a full Orbit cluster exhausts between the kubelet, the Istio sidecars, and the services that watch their own ConfigMaps. `make up` raises it to 1024 before creating the cluster, and restarts identity if it still has no provider. The sysctl does not survive a VM restart, so `make up` re-applies it every time.

Check the current value, as a k3d node sees it:

```bash
docker exec k3d-orbit-local-server-0 cat /proc/sys/fs/inotify/max_user_instances
```

Recover by hand:

```bash
docker run --rm --privileged alpine:3.20 sysctl -w fs.inotify.max_user_instances=1024
kubectl rollout restart deployment/orbit-identity -n orbit-system
kubectl logs deployment/orbit-identity -n orbit-system -c orbit-identity | grep "OIDC provider initialized"
```

### `orbit-dex` container is unhealthy

Dex authenticates against `glauth` over LDAP. Check both containers are healthy, then restart them.

```bash
docker compose ps glauth dex
docker compose restart glauth dex
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

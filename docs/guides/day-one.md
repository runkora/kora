# Day One

Clone to a running change. Orbit runs in Kubernetes locally, the same way it runs anywhere else. There is no host-process mode ([kubernetes-only-local-development](../adr/2026-07-10-kubernetes-only-local-development.md)).

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | control plane |
| Docker Desktop or Colima | 4 CPUs, 8 GiB allocated to the VM | runs the cluster and the backing services |
| Node | 24 (`>=24 <25`) | frontend; `nvm install 24 && nvm use` |
| pnpm | 10 | `corepack enable` |

## Start

```bash
make setup   # once per machine: tools, git hooks, local DNS, TLS, and a generated .env
make up      # backing services, k3d cluster, migrations, SPIRE, Istio, Orbit, tenants
make auth    # log in as dev@orbit.local.clearroute.io / password
```

`make up` is idempotent. Run it again whenever the cluster drifts.

When the cluster itself is the problem, `make reset` deletes it and brings it back. Postgres and Redis are Docker Compose volumes, so your data survives. k3d owns the image registry, so a reset rebuilds every image. Targets that delete data announce themselves with `DESTRUCTIVE` in `make help`: `dev-down-clean`, `db-reset` and `clean-all`.

- Frontend: `https://orbit.local.clearroute.io:8443`
- Inference gateway: `https://api.orbit.local.clearroute.io:8443/v1`

`make down` tears it all down.

## Call an authenticated API

`make auth` writes the token pair to `/tmp/orbit_credentials.json`. `$ORBIT_TOKEN` is not exported into subprocess shells, so read the token inline:

```bash
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://api.orbit.local.clearroute.io:8443/v1/models | jq .
```

Listing models works out of the box. Scope-gated endpoints, including `POST /v1/chat/completions`, return `403` until your user holds a role or grant that carries the scope. [policy-and-roles.md](policy-and-roles.md) covers roles, grants, and the `tenant-admin` bootstrap.

Access tokens last 4h locally, set by `accessTokenTtlMinutes` in `deploy/local/orbit-values.yaml`. When one expires, `make auth-refresh` rotates the pair. If the refresh token is dead too, run `make auth` again.

## Change something

Rebuild and roll one service:

```bash
make redeploy SERVICE=identity   # make redeploy-list shows every service
make redeploy                    # all of them
make redeploy-status             # what is running vs what is built
```

Each redeploy is a docker build, a push to the local registry, and a rollout. `make redeploy-warm` prebuilds every image so the caches are hot before your first change.

Unit tests need no cluster and are the fastest feedback you have:

```bash
make test
```

## Before you commit

```bash
make check   # tidy, fmt, vet, golangci-lint, gitleaks, docs-check, backend tests, frontend lint/type-check/tests
```

All PRs must pass `make check`. It does not run `make test-integration`, which exercises authz against the live cluster.

## Next

- [backend-golden-path.md](backend-golden-path.md): add a backend feature end to end
- [../../frontend/docs/golden-paths/frontend-state-pattern.md](../../frontend/docs/golden-paths/frontend-state-pattern.md): frontend data fetching, caching, reactive state
- [local-development.md](local-development.md): the full environment reference and troubleshooting
- [../code-organization.md](../code-organization.md): the binary map and feature layout

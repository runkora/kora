# Day One

Clone to a running, authenticated change — backend and frontend. Read this first; [local-development.md](local-development.md) has the depth (env vars, cluster tiers, troubleshooting).

Most work needs only the **Tier 1** stack: the Go binary on your host plus Postgres, Redis, and Dex in Docker. You need the k3d cluster (Tier 2) only for SPIFFE, the mesh, sidecar token injection, and the in-cluster-only services (`orbit-agents`, `session-manager`, `tool-gateway`). Start on Tier 1.

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | control plane |
| Docker Desktop | 4.x+ | runs Postgres/Redis/Dex |
| Node | 24 (`>=24 <25`) | frontend; `nvm install 24 && nvm use` |
| pnpm | 10 | `corepack enable` |

## Backend (Tier 1)

```bash
make setup             # install goose, air, golangci-lint, gitleaks + git hooks
make env               # cp .env.example .env  (defaults work as-is)
make gen-session-secret # prints a secret — paste it into BFF_SESSION_SECRET in .env
make dev-up            # Postgres + Redis + Dex; auto-generates secrets/signing.key
                       #   and config/dex/.env on first run
make migrate-up        # apply all pending migrations
make dev               # start the backend on :8080 with air hot-reload
```

`make dev-up` bootstraps the RSA signing key and the Dex password hash on first run only. Regenerate them any time with `make gen-signing-key` / `make dex-hash password=…`.

### Get a token and call the API

In a second terminal:

```bash
make auth-device       # Device Authorization Grant against local Dex
                       #   browser → dev@orbit.local.clearroute.io / password
                       #   token saved to /tmp/orbit_credentials.json
```

`$ORBIT_TOKEN` is not exported into subprocess shells — always read the token inline:

```bash
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -s \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8081/v1/models | jq .
```

Tokens expire after ~4h in local dev. `make auth-refresh` rotates the pair; if the refresh token is dead, re-run `make auth-device`.

## Frontend

```bash
make frontend-install  # enforces Node 24 + pnpm 10, runs pnpm install
make frontend-dev      # Next.js on :3000 (sources .env for backend URLs + BFF_SESSION_SECRET)
```

The BFF calls `requireEnv(...)` for every backend URL at boot, so the app throws if `.env` is incomplete — run the backend `make env` + `make gen-session-secret` steps first. Pages backed by in-cluster-only services (agents, tool gateway) error against a host-only backend; point their URLs at the cluster to use them.

## When you need the cluster (Tier 2)

For SPIFFE SVID exchange, sidecar token injection, or the in-cluster-only services:

```bash
make dev-cluster       # Docker Compose (Postgres, Redis, Dex) + k3d cluster
make cluster-operator  # deploy orbit-operator → it installs SPIRE, Istio, and all Orbit releases
make cluster-deploy    # create secrets + register tenants + apply workload fixtures
```

Then `make auth-device-k3d` for a cluster-issued token. Iterate on one service with `make redeploy-<svc>`; check drift with `make redeploy-status`. Full details in [local-development.md](local-development.md#full-stack-cluster-tier-2).

## Before you commit

```bash
make check             # go mod tidy + gofmt + vet + golangci-lint + gitleaks + docs-check
                       #   + backend tests + frontend lint/type-check/tests
```

All PRs must pass `make check` with zero issues. It does **not** run `make test-integration` (that needs a full cluster).

## Next

- [backend-golden-path.md](backend-golden-path.md) — add a backend feature end to end
- [../../frontend/docs/golden-paths/frontend-state-pattern.md](../../frontend/docs/golden-paths/frontend-state-pattern.md) — frontend data fetching, caching, reactive state
- [policy-and-roles.md](policy-and-roles.md) — grants, roles, and the scope vocabulary
- [../code-organization.md](../code-organization.md) — the binary map and feature layout

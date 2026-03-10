# Local Development Guide

This guide takes you from a fresh clone to a running Orbit backend with a valid session token. It covers both the fast daily-development tier and the full-stack cluster tier.

---

## Prerequisites

Install these tools before starting. All versions listed are minimums.

| Tool | Version | Install |
|---|---|---|
| Go | 1.26+ | https://go.dev/dl |
| Docker Desktop | 4.x+ | https://www.docker.com/products/docker-desktop |
| Make | system | pre-installed on macOS/Linux |

The following tools are installed automatically by `make setup`:

| Tool | Purpose |
|---|---|
| `goose` | Database migrations |
| `air` | Hot-reload for Go services |
| `golangci-lint` | Code linting |
| `gitleaks` | Secret scanning |

For Tier 2 (full-stack cluster), these are also installed by `make setup`:

| Tool | Purpose |
|---|---|
| `k3d` | Local Kubernetes cluster |
| `kubectl` | Kubernetes CLI |
| `helm` | Kubernetes package manager |

---

## First-Run Bootstrap

Run these steps once after cloning. They are safe to re-run.

### Step 1 — Install tooling

```bash
make setup
```

Installs `goose`, `air`, `golangci-lint`, `gitleaks`, and git hooks. Runs `go mod tidy`.

### Step 2 — Create your `.env` file

```bash
make env
```

Copies `.env.example` → `.env`. Open `.env` and review the values — the defaults work for local development without modification.

### Step 3 — Start backing services

```bash
make dev-up
```

Starts three Docker containers on the `orbit-network` bridge:

| Container | Port | Purpose |
|---|---|---|
| `orbit-postgres` | 5432 | Primary PostgreSQL database |
| `orbit-redis` | 6379 | JTI blocklist and session cache |
| `orbit-dex` | 5556 | Local OIDC identity provider |

**On first run, `make dev-up` automatically bootstraps two prerequisites before starting the containers:**

- **`secrets/signing.key`** — a 4096-bit RSA private key for signing platform JWTs. The identity service reads this via `ORBIT_TOKEN_SIGNING_KEY_FILE=secrets/signing.key` (already set in `.env.example`). Stored as a file rather than an env var because RSA PEM blocks are multi-line and `.env` parsers do not reliably preserve newlines.
- **`config/dex/.env`** — a bcrypt hash of the default dev password (`password`), consumed by the Dex container. Stored in a separate file rather than the root `.env` because Docker Compose v2 interpolates `$` characters in the `environment:` block, destroying the `$2y$10$...` bcrypt prefix.

Both guards are idempotent — they are skipped on subsequent runs if the files already exist.

**To use a custom Dex password** instead of the default `password`, generate `config/dex/.env` before running `dev-up`:

```bash
make dex-hash password=mysecretpassword
make dev-up
```

**To regenerate the signing key** at any time:

```bash
make gen-signing-key
```

Docker Compose health checks will block `make dev-up` until all three containers pass their health checks.

### Step 4 — Run database migrations

```bash
make migrate-up
```

Applies all pending Goose migrations from `backend/internal/platform/storage/migrations/`. Safe to run multiple times — already-applied migrations are skipped.

### Step 5 — Start the backend

```bash
make dev
```

Starts the Orbit backend with `air` for hot-reload. The server listens on `http://localhost:6069` by default.

You should see startup output including:

```
INFO  OIDC provider initialized  issuer=http://localhost:5556/dex
INFO  Server listening           addr=:6069
```

### Step 6 — Authenticate

```bash
make auth-device
```

Runs the Device Authorization Grant flow against local Dex:

1. The script calls `POST /api/v1/identity/device` and receives a `user_code` and `verification_uri`
2. It prints the code and URL — open the URL in your browser and enter the code
3. Log in as `dev@orbit.local` with password `password` (or your custom password from Step 4)
4. The script polls `POST /api/v1/identity/session` until authorization completes
5. On success, the Orbit platform JWT is saved to `/tmp/orbit_credentials.json`

Your token is now ready. Use it in API calls:

```bash
TOKEN=$(jq -r .access_token /tmp/orbit_credentials.json)
curl -H "Authorization: Bearer $TOKEN" http://localhost:6069/api/v1/...
```

---

## Daily Development Workflow

After the first-run bootstrap, your daily workflow is:

```bash
make dev-up      # if containers are not already running
make dev         # start the backend with hot-reload
```

The backend restarts automatically when you save a `.go` file. Migrations only need to be re-run after pulling changes that include new migration files.

To check code quality before committing:

```bash
make check
```

This runs `go mod tidy`, `gofmt`, `go vet`, `golangci-lint`, `gitleaks`, and the test suite.

---

## Database Migrations

Orbit uses [Goose](https://github.com/pressly/goose) for database migrations.

**Never write migration files by hand.** Always generate them:

```bash
make migrate-create name=add_agent_sessions
```

This creates a correctly timestamped file in `backend/internal/platform/storage/migrations/` with the `-- +goose Up` and `-- +goose Down` annotations pre-populated.

| Command | Description |
|---|---|
| `make migrate-up` | Apply all pending migrations |
| `make migrate-down` | Roll back the last applied migration |
| `make migrate-status` | Show applied and pending migrations |
| `make migrate-create name=X` | Generate a new migration file |

---

## Running Tests

```bash
make test          # unit tests — no cluster required
make test-integration  # integration tests — requires make dev-cluster
```

Unit tests run against the Tier 1 stack (Postgres + Redis). They do not require the Dex container or any OIDC provider — the identity service's `SVIDValidator` interface is stubbed, and OIDC flows are not exercised in unit tests.

Integration tests (`//go:build integration`) require the Tier 2 k3d cluster and test the full SPIFFE SVID exchange flow. See [Full-Stack Cluster (Tier 2)](#full-stack-cluster-tier-2) below.

---

## Using a Real OIDC Provider

To authenticate against Microsoft Entra (or any OIDC-compliant provider) instead of local Dex, set environment variables before starting the backend:

```bash
export ORBIT_OIDC_ISSUER_URL=https://login.microsoftonline.com/{tenant-id}/v2.0
export ORBIT_OIDC_CLIENT_ID={your-application-client-id}
export ORBIT_OIDC_CLIENT_SECRET={your-application-client-secret}
make dev
```

No code changes are required. The backend discovers the provider's endpoints from the standard `/.well-known/openid-configuration` document on startup.

The `ORBIT_OIDC_ISSUER_URL` environment variable overrides the `issuerUrl` value in `backend/internal/identity/config/local.yaml`. The YAML file is not modified.

> **Entra application requirements**: The registered application must have the `urn:ietf:params:oauth:grant-type:device_code` grant type enabled under Authentication → Advanced settings → Allow public client flows.

---

## Full-Stack Cluster (Tier 2)

The Tier 2 environment provisions a local Kubernetes cluster with SPIRE, Istio, and Envoy. Use it when you need to test the SPIFFE SVID exchange flow (`POST /api/v1/identity/exchange`) or sidecar token injection.

**Requires Tier 1 Postgres to be running first:**

```bash
make dev-up       # or make db-up if you only need Postgres
make dev-cluster  # provisions k3d cluster + SPIRE + Istio
```

`make dev-cluster` declares `db-up` as a prerequisite — you do not need to remember the ordering.

To tear down the cluster:

```bash
make dev-cluster-down
```

See [ADR-0016](../adr/0016-local-development-environment.md) for the full Tier 2 architecture, including the CoreDNS DNS injection pattern for reaching Docker Compose services from cluster pods.

---

## Environment Variables Reference

All secrets are read from environment variables. The `.env` file (copied from `.env.example` during setup) provides local defaults.

### Application

| Variable | Required | Description |
|---|---|---|
| `APP_ENV` | No | Environment name: `local`, `development`, `production`. Defaults to `local`. |

### Database

| Variable | Required | Description |
|---|---|---|
| `ORBIT_DB_HOST` | Yes | PostgreSQL host. Default: `localhost` |
| `ORBIT_DB_PORT` | Yes | PostgreSQL port. Default: `5432` |
| `ORBIT_DB_USER` | Yes | PostgreSQL username. Default: `orbit` |
| `ORBIT_DB_PASSWORD` | Yes | PostgreSQL password. Default: `orbit` |
| `ORBIT_DB_NAME` | Yes | Database name. Default: `orbit` |
| `ORBIT_DB_SSL_MODE` | No | SSL mode. Default: `disable` (local only) |

### Redis

| Variable | Required | Description |
|---|---|---|
| `ORBIT_REDIS_ADDR` | Yes | Redis address. Default: `localhost:6379` |
| `ORBIT_REDIS_PASSWORD` | No | Redis password. Leave empty for local dev. |
| `ORBIT_REDIS_DB` | No | Redis database index. Default: `0` |

### Identity / Token Signing

| Variable | Required | Description |
|---|---|---|
| `ORBIT_TOKEN_SIGNING_KEY_FILE` | Local | Path to RSA private key PEM file. Default: `secrets/signing.key`. Generate with `make gen-signing-key`. |
| `ORBIT_TOKEN_SIGNING_KEY` | CI/Prod | Inline RSA private key PEM (with real newlines, as injected by secret managers). Takes precedence over `ORBIT_TOKEN_SIGNING_KEY_FILE` if set. |

### OIDC / Federated Identity

| Variable | Required | Description |
|---|---|---|
| `ORBIT_OIDC_CLIENT_ID` | Yes (when OIDC enabled) | OAuth2 client ID. For local Dex: `orbit-local` |
| `ORBIT_OIDC_CLIENT_SECRET` | No | OAuth2 client secret. Leave empty for public clients (Dex local config is a public client). |
| `ORBIT_OIDC_ISSUER_URL` | No | Runtime override for the OIDC issuer URL. Overrides `issuerUrl` in `local.yaml`. Use to point at a real provider. |

---

## Project Structure

```
orbit/
├── backend/
│   ├── cmd/orbit/             # Application entry point and server wiring
│   ├── internal/
│   │   ├── identity/          # Authentication, sessions, token issuance
│   │   ├── platform/
│   │   │   ├── api/           # HTTP mux, controller interface, JSON helpers
│   │   │   ├── config/        # YAML config loader
│   │   │   ├── logging/       # Structured logger
│   │   │   ├── middleware/     # HTTP middleware (auth, tracing, etc.)
│   │   │   ├── observability/ # OpenTelemetry setup
│   │   │   └── storage/       # Postgres + Redis clients and migrations
│   │   └── example/           # Reference implementation of a feature package
├── config/
│   └── dex/
│       ├── config.yaml        # Dex OIDC provider configuration
│       └── .env               # Generated by make dex-env (gitignored)
├── docs/
│   ├── adr/                   # Architecture Decision Records
│   ├── guides/                # This file and other how-to guides
│   └── configuration/         # Configuration and secrets reference
├── scripts/
│   └── auth-device.sh         # Device Authorization Grant CLI script
├── secrets/                   # Gitignored — local signing keys
│   └── signing.key            # Generated by make gen-signing-key
├── .env                       # Local secrets (gitignored, copied from .env.example)
├── .env.example               # Template for .env — committed, no real secrets
├── docker-compose.yml         # Tier 1: Postgres, Redis, Dex
└── Makefile                   # All development targets
```

---

## Troubleshooting

### `orbit-dex` container is unhealthy

**Symptom:** `make dev-up` fails with `container orbit-dex is unhealthy`.

**Cause:** `config/dex/.env` is missing or contains an invalid bcrypt hash. `make dev-up` auto-generates this file on first run, but it may have become stale or corrupt.

**Fix:**
```bash
make dex-hash password=password   # regenerates config/dex/.env with a fresh hash
make dev-down
make dev-up
```

### `OIDC provider initialized` not in startup logs

**Symptom:** The backend starts but OIDC discovery does not appear in logs.

**Cause:** `issuerUrl` is empty in `backend/internal/identity/config/local.yaml` and `ORBIT_OIDC_ISSUER_URL` is not set.

**Fix:** Confirm `local.yaml` has `issuerUrl: "http://localhost:5556/dex"` and that the Dex container is healthy:
```bash
docker compose ps
curl http://localhost:5556/dex/.well-known/openid-configuration
```

### `POST /api/v1/identity/device` returns 400 `oidc_not_configured`

**Cause:** Same as above — the identity service started without a configured OIDC provider.

### Backend fails to start: `failed to parse RSA private key`

**Cause:** `secrets/signing.key` does not exist or is corrupt.

**Fix:**
```bash
make gen-signing-key   # regenerates secrets/signing.key
make dev               # restart the backend
```

### `make auth-device` times out waiting for authorization

**Cause:** The device code expired (default: 5 minutes) before authorization was completed in the browser.

**Fix:** Run `make auth-device` again and complete the browser step promptly.

### Redis fails to start: `FATAL CONFIG FILE ERROR`

**Cause:** An unsupported flag was passed to the Redis server binary.

**Fix:** Check `docker-compose.yml` — the Redis `command:` override (if present) must not include `--no-auth-warning`, which is a client-only flag not accepted by `redis-server`.

### Migration fails: `no such table` or `relation does not exist`

**Cause:** Migrations have not been run, or were run against a different database.

**Fix:**
```bash
make migrate-status   # check what's applied
make migrate-up       # apply pending migrations
```

---

## Related Documentation

- [ADR-0016](../adr/0016-local-development-environment.md) — Local development environment strategy (Tier 1 vs Tier 2)
- [ADR-0017](../adr/0017-oidc-provider-abstraction-and-local-identity-provider.md) — OIDC provider abstraction and Dex decision
- [ADR-0008](../adr/0008-unified-identity-model.md) — Unified identity model
- [Configuration reference](../configuration/config.md) — YAML config system
- [Secrets reference](../configuration/secrets.md) — How secrets are managed

# Infrastructure & Local Dev Setup — Full Audit

> Files reviewed: `Makefile`, `docker-compose.yml`, `.air.toml`, `.env.example`, `backend/.env`,
> `.githooks/pre-commit`, `.gitleaks.toml`, `.github/workflows/ci.yml`, `scripts/cluster/dev-cluster-up.sh`,
> `scripts/cluster/dev-cluster-down.sh`, `scripts/cluster/install-tools-cluster.sh`,
> `infrastructure/overlays/local/k3d-config.yaml`, all SPIRE base manifests,
> `infrastructure/base/spire/controller-manager-config.yaml`, both kustomization overlays,
> `infrastructure/base/network-policy/default-deny.yaml`,
> `backend/cmd/orbit/config/local.yaml`, `backend/cmd/orbit/config/development.yaml`

---

## 🔴 Critical — Breaks Onboarding or Has Security Impact

### 1. `make bootstrap` fails for a brand-new engineer

`bootstrap: setup dev-up migrate-up` — but `dev-up` needs `DB_USER`, `DB_PASSWORD` etc. from `.env`.
There is no `cp .env.example .env` step anywhere in the chain. A fresh clone + `make bootstrap` silently
uses empty vars, connects to `postgres://:@localhost:5432/` and fails with a confusing error.

**Fix:** Add an `env` target and wire it in:

```makefile
.PHONY: env
env: ## Scaffold .env from .env.example (no-op if .env already exists)
	@[ -f .env ] && echo "$(COLOR_YELLOW).env already exists, skipping$(COLOR_RESET)" || \
		(cp .env.example .env && echo "$(COLOR_GREEN)✓ .env created from .env.example$(COLOR_RESET)")

bootstrap: env setup dev-up migrate-up
```

---

### 2. `backend/.env` has no `.env.example` — runtime env is invisible

Root `.env.example` exists and is great. But `make dev` and `make run` both source `backend/.env` for
runtime config, and there is no `backend/.env.example`. An engineer has no idea what runtime vars are
expected. The `make dev` target silently starts with missing env vars rather than failing fast.

**Fix:** Create `backend/.env.example` documenting all runtime vars. Add a scaffold step to `bootstrap`.
The `dev` target should warn clearly if `backend/.env` is missing:

```makefile
dev:
	@[ -f backend/.env ] || (echo "$(COLOR_YELLOW)⚠ backend/.env missing — copy backend/.env.example$(COLOR_RESET)" && exit 1)
	...
```

User Feedback: This is currently not required, as backend doesnt have a .env file yet, since we have only got one example controller, we will add this once we need it. Disregard for now. 

---

### 3. `golangci-lint` version diverges between local and CI

`Makefile` line 74:
```bash
curl -sSfL .../golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
# no version — installs whatever latest is
```

`ci.yml` line 58:
```bash
curl -sSfL .../install.sh | sh -s -- -b $(go env GOPATH)/bin v2.6.2
# pinned to v2.6.2
```

Result: CI runs `v2.6.2`, local installs whatever `master` defaults to today. You will get
passes-locally-fails-CI drift as the linter gains new rules. This is the canonical local/CI divergence
anti-pattern.

**Fix:** Add `GOLANGCI_LINT_VERSION=v2.6.2` at the top of the Makefile alongside `GITLEAKS_VERSION` and
use it in the install command. One source of truth.

```makefile
GOLANGCI_LINT_VERSION=v2.6.2

# in install-tools:
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION)
```

---

### 4. SPIRE dev-unsafe config lives in `base/` — bleeds into every environment

Three settings that are explicitly "local dev only" live in the BASE layer, not in the local overlay:

| File | Setting | Problem |
|------|---------|---------|
| `base/spire/agent.yaml` | `insecure_bootstrap = true` | Dev-only; fatal in prod |
| `base/spire/agent.yaml` | `KeyManager "memory"` | Ephemeral keys, lost on pod restart |
| `base/spire/server-config.yaml` | `database_type = "sqlite3"` | Single-node only |
| `base/spire/controller-manager-config.yaml` | `clusterName: orbit-local` | Wrong in every other env |
| `base/spire/controller-manager-config.yaml` | `trustDomain: orbit.local` | Wrong in every other env |

The base layer is supposed to be the safe default all overlays inherit. Right now it is dev-shaped,
meaning any future staging/prod overlay silently inherits insecure bootstrap, memory key storage,
SQLite, and the wrong trust domain unless someone explicitly patches all of it.

**Fix:** Move these settings to `infrastructure/overlays/local/patches/` as Kustomize
`patchesStrategicMerge` patches. Base gets production-safe defaults: secure bootstrap, disk
`KeyManager`, a postgres `DataStore` stub, and a parameterised cluster name.

```
infrastructure/overlays/local/patches/
  spire-agent-insecure-bootstrap.yaml
  spire-agent-memory-keymanager.yaml
  spire-server-sqlite.yaml
  controller-manager-local.yaml
```

---

### 5. SPIRE Server and Controller Manager run as root — no `securityContext` anywhere

The SPIRE StatefulSet (`infrastructure/base/spire/server.yaml`) has zero `securityContext` at pod or
container level. Both `spire-server` and `spire-controller-manager` default to UID 0. SPIRE is the
trust anchor and CA for the entire cluster. Running it as root is ironic given it exists to enforce
identity.

**Fix:**
```yaml
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: spire-server
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            readOnlyRootFilesystem: true
```

---

## 🟡 Significant — Tech Debt, Reliability, and Developer Experience

### 6. Postgres wait-loop copy-pasted three times

The `pg_isready` polling block appears verbatim in:
- `db-up` (lines 108–111)
- `db-reset` (lines 126–129)
- `dev-up` (lines 179–182)

This is pure copy-paste debt — and it is solving a problem that is already solved. `docker-compose.yml`
defines healthchecks on both `postgres` and `redis`. The correct fix is to rely on
`depends_on: condition: service_healthy` and remove all three loops entirely.

```yaml
# docker-compose.yml — the healthchecks already exist, just wire them up:
services:
  postgres:
    healthcheck:  # already defined ✓
  redis:
    healthcheck:  # already defined ✓
```

Then `docker compose up -d postgres redis` already blocks until healthy. The Makefile loops are
redundant.

---

### 7. `goose` and `air` installed at `@latest` — non-reproducible

```makefile
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/air-verse/air@latest
```

Two engineers who ran `make install-tools` six months apart have different versions. Goose major
versions can have migration format changes.

**Fix:** Pin them like everything else:
```makefile
GOOSE_VERSION=v3.24.1
AIR_VERSION=v1.61.7

go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
go install github.com/air-verse/air@$(AIR_VERSION)
```

---

### 8. No minimum-version enforcement on tool presence checks

Every install check does `if ! command -v tool > /dev/null` — presence only, not version. An engineer
with goose `v3.18` from two years ago passes the check silently and may hit migration compatibility
issues. Same for `kubectl` in `install-tools-cluster.sh`.

**Fix:** Add a `check-tool-versions` target that validates semver floors and emits a warning with
expected vs detected version. At minimum, add a comment per tool stating the minimum supported version.

---

### 9. CRDs fetched from live GitHub URL at cluster boot time

`dev-cluster-up.sh` step 5:
```bash
kubectl apply -f "https://raw.githubusercontent.com/spiffe/spire-controller-manager/${SPIRE_CM_VERSION}/config/crd/bases/..."
```

This:
- Fails in offline or VPN-restricted environments
- Is not reviewed by your team when the upstream file changes
- Creates a subtle supply chain risk
- Is not version-locked in a meaningful way (the file content at that URL could change)

**Fix:** Vendor the three CRD YAML files:
```
infrastructure/base/spire/crds/
  spiffeid.spiffe.io_clusterfederatedtrustdomains.yaml
  spiffeid.spiffe.io_clusterspiffeids.yaml
  spiffeid.spiffe.io_clusterstaticentries.yaml
```
Add them to `infrastructure/base/spire/kustomization.yaml`. Update intentionally when bumping
`SPIRE_CM_VERSION` by re-running the `curl` and committing the result. Cluster setup becomes fully
offline-capable.

User Question: So does this mean we are going to move this into kustomize to manage instead of a shell script?

---

### 10. `CLUSTER_NAME` hardcoded independently in both cluster scripts

`dev-cluster-up.sh` line 14: `CLUSTER_NAME="orbit-local"`
`dev-cluster-down.sh` line 4: `CLUSTER_NAME="orbit-local"`

Two files, same constant, no shared source of truth. Change one, forget the other.

**Fix:** Create `scripts/cluster/config.sh`:
```bash
#!/usr/bin/env bash
CLUSTER_NAME="${CLUSTER_NAME:-orbit-local}"
SPIRE_CM_VERSION="0.6.3"
ISTIO_VERSION="1.21.2"
K3D_CONFIG="${REPO_ROOT}/infrastructure/overlays/local/k3d-config.yaml"
```
Then `source "$(dirname "$0")/config.sh"` at the top of both scripts. Also makes
`CLUSTER_NAME` environment-overridable for engineers who want multiple clusters.

---

### 11. Istio images not pre-fetched — the slowest pull is skipped

`PREFETCH_IMAGES` in `dev-cluster-up.sh` contains SPIRE images but not Istio. The `istiod` and
`proxyv2` images are several hundred MB each and are the actual first-run bottleneck. The pre-fetch
mechanism exists specifically to solve this but pre-fetches the wrong things.

**Fix:**
```bash
PREFETCH_IMAGES=(
  # existing SPIRE images...
  "docker.io/istio/pilot:${ISTIO_VERSION}"
  "docker.io/istio/proxyv2:${ISTIO_VERSION}"
)
```

User Question: Can this also be done with kustomize by pre-pulling the images into the cluster before applying the manifests that reference them? Instead of having them in a shell script?

---

### 12. Two separate kustomization trees with no root aggregator

`dev-cluster-up.sh` runs two separate `kubectl apply -k` commands:
1. `infrastructure/overlays/local/` — SPIRE + NetworkPolicy
2. `infrastructure/services/overlays/local/` — ClusterSPIFFEIDs

These should be unified under a single root kustomization so `kubectl apply -k infrastructure/overlays/local/`
deploys everything, and `kubectl diff -k` shows a complete diff in one command.

**Fix:**
```yaml
# infrastructure/overlays/local/kustomization.yaml
resources:
  - ../../base/orbit-system-namespace
  - ../../base/spire
  - ../../base/network-policy
  - ../../../infrastructure/services/overlays/local   # ← add this
```

---

### 13. `local.yaml` and `development.yaml` differ by exactly one line

The only difference between these two files is `logFormat: console` vs `logFormat: json`. Every other
field — `app.version`, `server.port`, `server.basePath`, `server.shutdownTimeoutSecs`,
`observability.serviceName`, `otlpEndpoint`, `samplingRate`, `otlpInsecure` — is duplicated verbatim.

**Fix:** Introduce `backend/cmd/orbit/config/base.yaml` with all shared values. Then `local.yaml` and
`development.yaml` each contain only their single override. Viper supports `MergeInConfig()` for exactly
this pattern.

```yaml
# base.yaml — all shared config
server:
  port: 6069
  basePath: /api/v1
  shutdownTimeoutSecs: 5
# ...

# local.yaml — only the override
logFormat: console

# development.yaml — only the override
logFormat: json
```

User Feedback: No need to address application code for now, this will be updated when we are building out the application. We are focusing on local development for the moment. Note this is good feedback, just not something we will implement just yet. Can you add a note to the ADR for config? yaml-based config that this is a future enhancement we want. Can you also add a short feature spec to the folder docs/product/scratch/ . Just has to be short, enough for an agent in the future to pick up and turn into a proper spec. 

---

### 14. `app.version: "0.1.0"` baked into config YAML

Version should never be in a config file. It belongs in the binary via `ldflags` at build time so
`git describe` or a CI tag feeds through automatically and the binary always reports the right version.

**Fix:**
```makefile
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build-backend:
	@cd $(BACKEND_DIR) && $(GOBUILD) \
		-ldflags "-X main.version=$(GIT_VERSION)" \
		-o ../$(BINARY_DIR)/$(BINARY_NAME) ./cmd/orbit
```
Remove `app.version` from both config YAMLs.

---

## 🟢 Minor — Polish and Best Practice

### 15. Cluster tools install to GOPATH; deprecated `kubectl` flag

`install-tools-cluster.sh` installs `k3d`, `kubectl`, `istioctl`, and `helm` into
`$(go env GOPATH)/bin`. This makes Go a prerequisite for cluster tooling even though they have nothing
to do with each other. Also `kubectl version --client --short` is deprecated since `v1.28` and emits
deprecation warnings on current versions.

**Fix:** Install to `~/.local/bin` (with `mkdir -p`). Replace `kubectl version --client --short` with
`kubectl version --client -o json | jq -r .clientVersion.gitVersion`.

---

### 16. `spire-controller-manager` has no liveness/readiness probes, and its health endpoint is disabled

`controller-manager-config.yaml` sets `healthProbeBindAddress: "0"` which disables the health endpoint
entirely. The StatefulSet then has no probes anyway. Kubernetes cannot detect if the controller manager
is hung.

**Fix:** Enable the health endpoint on a real port and add probes:
```yaml
# controller-manager-config.yaml
health:
  healthProbeBindAddress: ":8083"

# server.yaml — spire-controller-manager container
livenessProbe:
  httpGet:
    path: /healthz
    port: 8083
  initialDelaySeconds: 15
readinessProbe:
  httpGet:
    path: /readyz
    port: 8083
  initialDelaySeconds: 5
```

---

### 17. NetworkPolicy allows ALL pods in `orbit-system` to reach SPIRE server port 8081

The `allow-spire-server-grpc` policy has no `podSelector` on the `from` side. Only SPIRE agents need
that port. Every pod in the namespace can reach the cluster CA.

**Fix:**
```yaml
- from:
  - podSelector:
      matchLabels:
        app: spire-agent
  ports:
  - protocol: TCP
    port: 8081
```

---

### 18. `clean-all` calls `db-down` then redundantly calls `docker compose down -v`

```makefile
clean-all: clean db-down
	docker compose down -v
```

`db-down` already runs `docker compose down`. Then `clean-all` runs `docker compose down -v` again.
The `db-down` dependency is dead weight. Remove it and keep only the `-v` call directly in `clean-all`.

---

### 19. Stub `up`/`down` targets appear in `make help` output

The `@ignore` prefix in the help comment does not suppress them from the grep pattern. They render in
`make help` as real targets with the description `@ignore Ignore standalone 'up' target`. Either remove
these stubs entirely (preferred) or strip their `##` comment so they don't match the help grep.

---

### 20. No `Dockerfile` for the backend

There is no defined image build. Local dev uses `go run`, CI uses `go build`. The contract between
local dev and "what runs in the cluster" is undefined — this becomes a concrete problem the moment
you want to deploy the backend into the Tier 2 k3d cluster or to staging.

**Fix:** Add a multi-stage distroless `Dockerfile` at the repo root or alongside `docker-compose.yml`.
Even a minimal one establishes the contract:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY backend/ .
RUN go build -o /orbit ./cmd/orbit

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /orbit /orbit
ENTRYPOINT ["/orbit"]
```

User Feedback: Do not worry about this for now, we are not building out the application yet, we will add this when we have a working application and need to containerize it.

---

## Summary Table

| # | Area | Severity | Effort |
|---|------|----------|--------|
| 1 | `bootstrap` missing `.env` scaffold | 🔴 Critical | 5 min |
| 2 | `backend/.env` has no `.env.example` | 🔴 Critical | 10 min |
| 3 | `golangci-lint` version diverges local ↔ CI | 🔴 Critical | 5 min |
| 4 | SPIRE dev config in `base/` layer | 🔴 Critical | 1–2 hrs |
| 5 | SPIRE runs as root, no `securityContext` | 🔴 Critical | 30 min |
| 6 | Postgres wait-loop copy-pasted 3× | 🟡 Significant | 20 min |
| 7 | `goose` and `air` unpinned `@latest` | 🟡 Significant | 5 min |
| 8 | No minimum version enforcement for tools | 🟡 Significant | 30 min |
| 9 | CRDs fetched from live GitHub URL | 🟡 Significant | 30 min |
| 10 | `CLUSTER_NAME` duplicated across scripts | 🟡 Significant | 15 min |
| 11 | Istio images not pre-fetched | 🟡 Significant | 5 min |
| 12 | Two kustomize trees, no root aggregator | 🟡 Significant | 20 min |
| 13 | `local.yaml` ≈ `development.yaml` (1 diff) | 🟡 Significant | 20 min |
| 14 | `app.version` baked into config YAML | 🟡 Significant | 20 min |
| 15 | Cluster tools install to GOPATH; stale kubectl flag | 🟢 Minor | 15 min |
| 16 | Controller-manager probes missing; health endpoint disabled | 🟢 Minor | 20 min |
| 17 | NetworkPolicy too broad to SPIRE server | 🟢 Minor | 10 min |
| 18 | `clean-all` redundant `db-down` call | 🟢 Minor | 2 min |
| 19 | Stub `up`/`down` targets leak into `make help` | 🟢 Minor | 2 min |
| 20 | No `Dockerfile` for the backend | 🟢 Minor | 1 hr |

---

## Recommended Attack Order

**Fix now — 30 min total, eliminates all onboarding failures:**
- Issues 1, 2, 3, 7 — `.env` scaffolding + all tool version pinning

**Fix this sprint — security correctness + infra hygiene:**
- Issues 4, 5 — SPIRE base layer cleanup + `securityContext`
- Issues 9, 12 — vendor CRDs + unify kustomize trees
- Issue 6 — remove wait-loop duplication; rely on `depends_on` healthcheck

**Fix in a quiet hour:**
- Issues 10, 11, 13, 14, 15, 16, 17 — config consolidation + polish

---

## What's Actually Good

The bones are solid. Worth calling out:

- **Two-tier dev model** (docker-compose for daily Go work, k3d for full cluster) is the right architecture — engineers shouldn't need a cluster to iterate on business logic
- **`make help`** with colour and auto-generated descriptions is excellent DX
- **`gitleaks` + pre-commit hook** wired into `make setup` is the right pattern — security tooling is opt-out not opt-in
- **Kustomize overlay structure** is headed in the right direction — the `base/` + `overlays/` separation is correct, just needs the dev-unsafe config moved out of base
- **`.env.example`** exists at the root with clear comments — just needs to be wired into `bootstrap` and replicated for `backend/`
- **`GITLEAKS_VERSION`** is properly pinned and consistent between Makefile and CI — the model is right, just needs to be applied to the other tools
- **`go-version-file: backend/go.mod`** in CI is correct — Go version is derived from the module, not hardcoded in the workflow

These are all fixable in a focused afternoon of work.

# Orbit Control Plane - Development Makefile
# ===========================================

# Load environment variables from .env if it exists
-include .env
export

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Project directories
BACKEND_DIR=./backend
FRONTEND_DIR=./frontend
CMD_DIR=./backend/cmd/orbit
MIGRATIONS_DIR=./backend/internal/platform/storage/migrations

# Binary output
BINARY_DIR=.bin

# Tool versions (pin for reproducible installs)
GITLEAKS_VERSION=v8.27.2
GOLANGCI_LINT_VERSION=v2.11.3
GOOSE_VERSION=v3.24.1
AIR_VERSION=v1.61.7
NODE_VERSION=24
PNPM_VERSION=10.32.1
PROTOC_GEN_GO_VERSION=v1.36.11
PROTOC_GEN_GO_GRPC_VERSION=v1.5.1
BUF_VERSION=v1.55.1

# Database connection string
DB_URL=postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Colima (Lima-based Docker runtime on macOS) uses 192.168.5.2 as its internal DNS,
# which conflicts with k3d's DNS-fix behaviour. Auto-detect Colima and disable the fix.
# Developers on Docker Desktop can override this in .env: K3D_FIX_DNS=1
K3D_FIX_DNS ?= $(if $(shell which colima 2>/dev/null),0,)

# Frontend public URL — baked into the Next.js client bundle at Docker build time.
# Override for non-local environments (e.g. CI): NEXT_PUBLIC_APP_URL=https://my.host docker buildx bake orbit-frontend
NEXT_PUBLIC_APP_URL ?= https://orbit.local.clearroute.io:8443

# Docker image publishing (generic backend/Dockerfile via docker buildx bake)
DOCKER_BAKE_FILE ?= docker-bake.hcl
IMAGE_REGISTRY ?= ghcr.io
IMAGE_NAMESPACE ?= clear-route
IMAGE_TAG ?= latest
IMAGE_REPOSITORY_PREFIX ?= $(IMAGE_REGISTRY)/$(IMAGE_NAMESPACE)/orbit-
BACKEND_IMAGE_GROUP ?= backend-images
DOCKER_BUILD_PLATFORMS ?= linux/amd64
DOCKER_PUSH_PLATFORMS ?= linux/amd64,linux/arm64

# Colors for output
COLOR_RESET=\033[0m
COLOR_BOLD=\033[1m
COLOR_RED=\033[31m
COLOR_GREEN=\033[32m
COLOR_YELLOW=\033[33m
COLOR_BLUE=\033[36m

.DEFAULT_GOAL := help

# ===========================================
# Help Target
# ===========================================

.PHONY: help
help: ## Display this help screen
	@echo "$(COLOR_BOLD)Orbit Control Plane - Available Targets$(COLOR_RESET)"
	@echo ""
	@grep -Eh '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2}'
	@echo ""

# ===========================================
# Setup & Installation
# ===========================================

.PHONY: env
env: ## Scaffold .env from .env.example (no-op if .env already exists)
	@if [ -f .env ]; then \
		echo "$(COLOR_YELLOW).env already exists, skipping$(COLOR_RESET)"; \
	else \
		cp .env.example .env; \
		echo "$(COLOR_GREEN)✓ .env created from .env.example — review and update values before continuing$(COLOR_RESET)"; \
	fi

.PHONY: setup
setup: install-tools install-tools-cluster tidy frontend-install hooks setup-dns setup-tls ## Complete project setup (install tools, tidy dependencies, install hooks)
	@echo "$(COLOR_GREEN)✓ Setup complete$(COLOR_RESET)"
	@if [ ! -f secrets/tls.crt ]; then \
		echo "$(COLOR_YELLOW)Note: TLS certs were not generated (non-macOS without mkcert).$(COLOR_RESET)"; \
		echo "$(COLOR_YELLOW)      'make dev-up' will generate a self-signed fallback automatically.$(COLOR_RESET)"; \
	fi

.PHONY: setup-dns
setup-dns: ## Configure *.orbit.local.clearroute.io DNS on macOS via dnsmasq (idempotent, no-op on Linux)
	@chmod +x scripts/setup-dns.sh
	@bash scripts/setup-dns.sh

.PHONY: hooks
hooks: ## Configure git to use the project's tracked hooks from .githooks/
	@git config core.hooksPath .githooks
	@echo "$(COLOR_GREEN)✓ Git hooks installed (.githooks/)$(COLOR_RESET)"

# ===========================================
# Worktree Management
# ===========================================

.PHONY: worktree-new
worktree-new: ## Create a sibling worktree for a branch (usage: make worktree-new BRANCH=feature/my-feature)
	$(if $(BRANCH),,$(error BRANCH is required — usage: make worktree-new BRANCH=feature/my-feature))
	@SLUG=$$(echo "$(BRANCH)" | tr '/' '-'); \
	WORKTREE_DIR="../orbit-$$SLUG"; \
	if [ -d "$$WORKTREE_DIR" ]; then \
		echo "$(COLOR_YELLOW)⚠ Worktree already exists at $$WORKTREE_DIR$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	echo "$(COLOR_YELLOW)Creating worktree for branch '$(BRANCH)'...$(COLOR_RESET)"; \
	git fetch origin main --quiet; \
	if git show-ref --verify --quiet "refs/heads/$(BRANCH)"; then \
		git worktree add "$$WORKTREE_DIR" "$(BRANCH)"; \
	elif git show-ref --verify --quiet "refs/remotes/origin/$(BRANCH)"; then \
		git worktree add --track -b "$(BRANCH)" "$$WORKTREE_DIR" "origin/$(BRANCH)"; \
	else \
		git worktree add -b "$(BRANCH)" "$$WORKTREE_DIR" origin/main; \
		git push origin "$(BRANCH)"; \
		git -C "$$WORKTREE_DIR" branch --set-upstream-to="origin/$(BRANCH)" "$(BRANCH)"; \
	fi; \
	echo "$(COLOR_YELLOW)Symlinking local dev files...$(COLOR_RESET)"; \
	ln -s "$(CURDIR)/.env" "$$WORKTREE_DIR/.env"; \
	ln -s "$(CURDIR)/secrets" "$$WORKTREE_DIR/secrets"; \
	mkdir -p "$$WORKTREE_DIR/config/dex"; \
	ln -s "$(CURDIR)/config/dex/.env" "$$WORKTREE_DIR/config/dex/.env"; \
	echo "$(COLOR_YELLOW)Configuring git hooks...$(COLOR_RESET)"; \
	git -C "$$WORKTREE_DIR" config core.hooksPath .githooks; \
	echo "$(COLOR_GREEN)✓ Worktree ready$(COLOR_RESET)"; \
	echo ""; \
	echo "  Branch:  $(BRANCH)"; \
	echo "  Path:    $$(cd "$$WORKTREE_DIR" && pwd)"; \
	echo ""; \
	echo "  Next steps:"; \
	echo "    cd $$WORKTREE_DIR && make dev-up"; \
	echo ""

.PHONY: worktree-remove
worktree-remove: ## Remove a sibling worktree (usage: make worktree-remove BRANCH=feature/my-feature [FORCE=1])
	$(if $(BRANCH),,$(error BRANCH is required — usage: make worktree-remove BRANCH=feature/my-feature))
	@SLUG=$$(echo "$(BRANCH)" | tr '/' '-'); \
	WORKTREE_DIR="../orbit-$$SLUG"; \
	if [ ! -d "$$WORKTREE_DIR" ]; then \
		echo "$(COLOR_YELLOW)⚠ No worktree found at $$WORKTREE_DIR$(COLOR_RESET)"; \
		exit 1; \
	fi; \
	echo "$(COLOR_YELLOW)Removing worktree for branch '$(BRANCH)'...$(COLOR_RESET)"; \
	if [ "$(FORCE)" = "1" ]; then \
		echo "$(COLOR_YELLOW)⚠ FORCE=1 set — uncommitted changes in the worktree will be lost.$(COLOR_RESET)"; \
		git worktree remove "$$WORKTREE_DIR" --force; \
	else \
		git worktree remove "$$WORKTREE_DIR" || { \
			echo "$(COLOR_RED)✗ Worktree has uncommitted changes. Commit or discard them first,$(COLOR_RESET)"; \
			echo "$(COLOR_RED)  or re-run with FORCE=1 to discard them:$(COLOR_RESET)"; \
			echo "$(COLOR_RED)    make worktree-remove BRANCH=$(BRANCH) FORCE=1$(COLOR_RESET)"; \
			exit 1; \
		}; \
	fi; \
	git worktree prune; \
	echo "$(COLOR_GREEN)✓ Worktree removed and refs pruned$(COLOR_RESET)"

.PHONY: worktree-list
worktree-list: ## List all active worktrees
	@git worktree list

.PHONY: install-tools
install-tools: ## Install development tools (goose, golangci-lint, air, gitleaks, buf, protoc-gen-go)
	@echo "$(COLOR_YELLOW)Installing development tools...$(COLOR_RESET)"
	@echo "  Installing goose $(GOOSE_VERSION)..."
	@$(GOCMD) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	@echo "  Installing air $(AIR_VERSION)..."
	@$(GOCMD) install github.com/air-verse/air@$(AIR_VERSION)
	@echo "  Installing protoc-gen-go $(PROTOC_GEN_GO_VERSION)..."
	@$(GOCMD) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	@echo "  Installing protoc-gen-go-grpc $(PROTOC_GEN_GO_GRPC_VERSION)..."
	@$(GOCMD) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	@BUF_INSTALLED=$$(buf --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$BUF_INSTALLED" != "$(BUF_VERSION:v%=%)" ]; then \
		echo "  Installing buf $(BUF_VERSION) (installed: $${BUF_INSTALLED:-none})..."; \
		BUF_OS=$$(uname -s); BUF_ARCH=$$(uname -m); \
		curl -sSfL --retry 3 "https://github.com/bufbuild/buf/releases/download/$(BUF_VERSION)/buf-$${BUF_OS}-$${BUF_ARCH}" \
			-o "$$(go env GOPATH)/bin/buf" && chmod +x "$$(go env GOPATH)/bin/buf"; \
	else \
		echo "  buf $(BUF_VERSION) already installed"; \
	fi
	@LINT_INSTALLED=$$(golangci-lint --version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$LINT_INSTALLED" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "  Installing golangci-lint $(GOLANGCI_LINT_VERSION) (installed: $${LINT_INSTALLED:-none})..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	else \
		echo "  golangci-lint $(GOLANGCI_LINT_VERSION) already installed"; \
	fi
	@GL_INSTALLED=$$(gitleaks version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$GL_INSTALLED" != "$(GITLEAKS_VERSION)" ]; then \
		echo "  Installing gitleaks $(GITLEAKS_VERSION) (installed: $${GL_INSTALLED:-none})..."; \
		GL_TAG=$(GITLEAKS_VERSION); \
		GL_VER=$${GL_TAG#v}; \
		GL_OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
		GL_ARCH=$$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/'); \
		GL_URL="https://github.com/gitleaks/gitleaks/releases/download/$${GL_TAG}/gitleaks_$${GL_VER}_$${GL_OS}_$${GL_ARCH}.tar.gz"; \
		curl -sSfL "$${GL_URL}" | tar -xz -C $$(go env GOPATH)/bin gitleaks; \
	else \
		echo "  gitleaks $(GITLEAKS_VERSION) already installed"; \
	fi
	@echo "$(COLOR_GREEN)✓ Tools installed$(COLOR_RESET)"

.PHONY: proto-gen
proto-gen: ## Generate Go code from proto files (requires buf — run make install-tools first)
	@echo "$(COLOR_YELLOW)Generating proto files...$(COLOR_RESET)"
	@cd backend && buf generate
	@echo "$(COLOR_GREEN)✓ Proto files generated$(COLOR_RESET)"

.PHONY: check-tool-versions
check-tool-versions: ## Verify installed tool versions match pinned versions in Makefile
	@echo "$(COLOR_YELLOW)Checking tool versions...$(COLOR_RESET)"
	@INSTALLED=$$(golangci-lint --version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$INSTALLED" = "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "  $(COLOR_GREEN)✓ golangci-lint $$INSTALLED$(COLOR_RESET)"; \
	else \
		echo "  $(COLOR_YELLOW)⚠ golangci-lint: installed=$${INSTALLED:-none}  expected=$(GOLANGCI_LINT_VERSION) — run: make install-tools$(COLOR_RESET)"; \
	fi
	@INSTALLED=$$(gitleaks version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$INSTALLED" = "$(GITLEAKS_VERSION)" ]; then \
		echo "  $(COLOR_GREEN)✓ gitleaks $$INSTALLED$(COLOR_RESET)"; \
	else \
		echo "  $(COLOR_YELLOW)⚠ gitleaks: installed=$${INSTALLED:-none}  expected=$(GITLEAKS_VERSION) — run: make install-tools$(COLOR_RESET)"; \
	fi
	@INSTALLED=$$(goose --version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$INSTALLED" = "$(GOOSE_VERSION)" ]; then \
		echo "  $(COLOR_GREEN)✓ goose $$INSTALLED$(COLOR_RESET)"; \
	else \
		echo "  $(COLOR_YELLOW)⚠ goose: installed=$${INSTALLED:-none}  expected=$(GOOSE_VERSION) — run: make install-tools$(COLOR_RESET)"; \
	fi
	@INSTALLED=$$(air -v 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$INSTALLED" = "$(AIR_VERSION)" ]; then \
		echo "  $(COLOR_GREEN)✓ air $$INSTALLED$(COLOR_RESET)"; \
	else \
		echo "  $(COLOR_BLUE)ℹ air: installed=$${INSTALLED:-none}  expected=$(AIR_VERSION) — run: make install-tools$(COLOR_RESET)"; \
	fi
	@. scripts/cluster/versions.env && \
	INSTALLED=$$(helm version --short 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$INSTALLED" = "$${HELM_VERSION}" ]; then \
		echo "  $(COLOR_GREEN)✓ helm $$INSTALLED$(COLOR_RESET)"; \
	else \
		echo "  $(COLOR_YELLOW)⚠ helm: installed=$${INSTALLED:-none}  expected=$${HELM_VERSION} — run: make install-tools-cluster$(COLOR_RESET)"; \
	fi


.PHONY: tidy
tidy: ## Tidy and verify go.mod dependencies
	@echo "$(COLOR_YELLOW)Tidying dependencies...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOMOD) tidy
	@cd $(BACKEND_DIR) && $(GOMOD) verify
	@echo "$(COLOR_GREEN)✓ Dependencies tidied$(COLOR_RESET)"

# ===========================================
# Database Management
# ===========================================

.PHONY: db-up
db-up: ## Start the PostgreSQL database (docker-compose)
	@echo "$(COLOR_YELLOW)Starting database...$(COLOR_RESET)"
	@docker compose up -d --wait postgres
	@echo "$(COLOR_GREEN)✓ Database started and ready$(COLOR_RESET)"

.PHONY: db-down
db-down: ## Stop the PostgreSQL database
	@echo "$(COLOR_YELLOW)Stopping database...$(COLOR_RESET)"
	@docker compose down
	@echo "$(COLOR_GREEN)✓ Database stopped$(COLOR_RESET)"

.PHONY: db-reset
db-reset: ## Reset database (destroy volume, recreate)
	@echo "$(COLOR_YELLOW)Resetting database...$(COLOR_RESET)"
	@docker compose down -v
	@docker compose up -d --wait postgres
	@echo "$(COLOR_GREEN)✓ Database reset complete$(COLOR_RESET)"

.PHONY: db-logs
db-logs: ## Show database logs
	@docker compose logs -f postgres

.PHONY: db-shell
db-shell: ## Open a psql shell to the database
	@docker compose exec postgres psql -U $${DB_USER:-orbit} -d $${DB_NAME:-orbit}

# Collect all per-service migration files into a temp directory for local dev.
# Each service owns its migrations under backend/cmd/{service}/migrations/.
# Goose orders them by timestamp prefix so cross-service ordering is preserved.
COLLECT_MIGRATIONS = \
	MIGRATIONS_TEMP=$$(mktemp -d); \
	find backend/cmd -path '*/migrations/*.sql' -exec cp {} "$$MIGRATIONS_TEMP/" \; ;

.PHONY: migrate-up
migrate-up: ## Run all database migrations up (collects all per-service migrations)
	@echo "$(COLOR_YELLOW)Running migrations up...$(COLOR_RESET)"
	@$(COLLECT_MIGRATIONS) \
		GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir "$$MIGRATIONS_TEMP" up; \
		EXIT=$$?; rm -rf "$$MIGRATIONS_TEMP"; exit $$EXIT
	@echo "$(COLOR_GREEN)✓ Migrations up complete$(COLOR_RESET)"

.PHONY: migrate-down
migrate-down: ## Roll back the last database migration
	@echo "$(COLOR_YELLOW)Rolling back last migration...$(COLOR_RESET)"
	@$(COLLECT_MIGRATIONS) \
		GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir "$$MIGRATIONS_TEMP" down; \
		EXIT=$$?; rm -rf "$$MIGRATIONS_TEMP"; exit $$EXIT
	@echo "$(COLOR_GREEN)✓ Migration rolled back$(COLOR_RESET)"

.PHONY: migrate-status
migrate-status: ## Show migration status
	@echo "$(COLOR_YELLOW)Migration status:$(COLOR_RESET)"
	@$(COLLECT_MIGRATIONS) \
		GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir "$$MIGRATIONS_TEMP" status; \
		EXIT=$$?; rm -rf "$$MIGRATIONS_TEMP"; exit $$EXIT

.PHONY: migrate-create
migrate-create: ## Create a new migration (usage: make migrate-create service=identity name=migration_name)
	@if [ -z "$(name)" ] || [ -z "$(service)" ]; then \
		echo "$(COLOR_YELLOW)Usage: make migrate-create service=<service> name=<migration_name>$(COLOR_RESET)"; \
		echo "$(COLOR_YELLOW)  e.g. make migrate-create service=identity name=add_sessions_index$(COLOR_RESET)"; \
		exit 1; \
	fi
	@if [ ! -d "backend/cmd/$(service)/migrations" ]; then \
		echo "$(COLOR_RED)Error: backend/cmd/$(service)/migrations does not exist$(COLOR_RESET)"; \
		exit 1; \
	fi
	@echo "$(COLOR_YELLOW)Creating migration for service '$(service)': $(name)$(COLOR_RESET)"
	@goose -dir backend/cmd/$(service)/migrations create $(name) sql
	@echo "$(COLOR_GREEN)✓ Migration created in backend/cmd/$(service)/migrations/$(COLOR_RESET)"

# ===========================================
# Local Development Environment (Tier 1 — docker-compose)
# ===========================================

.PHONY: dev-up
dev-up: ## Start Postgres + Redis + glauth + Dex (local OIDC provider) for daily Go development
	@if [ ! -f secrets/signing.key ]; then \
		echo "$(COLOR_YELLOW)→ secrets/signing.key not found — generating RSA signing key...$(COLOR_RESET)"; \
		$(MAKE) --no-print-directory gen-signing-key; \
	fi
	@if [ ! -f secrets/tls.crt ] || [ ! -f secrets/tls.key ]; then \
		echo "$(COLOR_YELLOW)→ TLS certificates not found — generating for Dex HTTPS...$(COLOR_RESET)"; \
		$(MAKE) --no-print-directory setup-tls; \
	fi
	@echo "$(COLOR_YELLOW)Starting local services (Postgres + Redis + glauth + Dex + port proxy)...$(COLOR_RESET)"
	@docker compose up -d --wait postgres redis glauth dex port-proxy
	@echo "$(COLOR_GREEN)✓ Postgres + Redis + glauth + Dex + port proxy ready$(COLOR_RESET)"
	@echo "  Dex OIDC issuer:  https://dex.orbit.local.clearroute.io:5556/dex"
	@echo "  Test user:        dev@orbit.local.clearroute.io / password"
	@echo "  Client ID:        orbit-local  (no secret — public client)"
	@echo ""
	@echo "  Authenticate:     make auth-device"

.PHONY: dev-down
dev-down: ## Stop Postgres + Redis
	@echo "$(COLOR_YELLOW)Stopping local services...$(COLOR_RESET)"
	@docker compose down
	@echo "$(COLOR_GREEN)✓ Local services stopped$(COLOR_RESET)"

.PHONY: dev-down-clean
dev-down-clean: ## Stop Postgres + Redis and remove volumes
	@echo "$(COLOR_YELLOW)Stopping local services and removing volumes...$(COLOR_RESET)"
	@docker compose down -v
	@echo "$(COLOR_GREEN)✓ Local services stopped and volumes removed$(COLOR_RESET)"

# ===========================================
# Local Development Environment (Tier 2 — k3d cluster)
# ===========================================

.PHONY: install-tools-cluster
install-tools-cluster: ## Install cluster tooling (k3d, kubectl, helm)
	@echo "$(COLOR_YELLOW)Installing cluster tools...$(COLOR_RESET)"
	@bash scripts/cluster/install-tools-cluster.sh
	@echo "$(COLOR_GREEN)✓ Cluster tools installed$(COLOR_RESET)"

.PHONY: dev-cluster
dev-cluster: dev-up ## Bring up the local k3d cluster. Starts postgres + Redis + Dex first, then creates the k3d cluster. Run 'make cluster-operator' next.
	@echo "$(COLOR_YELLOW)Bringing up local k3d cluster...$(COLOR_RESET)"
	@K3D_FIX_DNS=$(K3D_FIX_DNS) bash scripts/cluster/dev-cluster-up.sh

.PHONY: dev-cluster-down
dev-cluster-down: ## Tear down the local k3d cluster
	@echo "$(COLOR_YELLOW)Tearing down local k3d cluster...$(COLOR_RESET)"
	@bash scripts/cluster/dev-cluster-down.sh

# Backend services that are built and pushed during cluster bring-up. Order
# matters only for readability — they're all built serially via redeploy.sh.
# Adding a new service is a one-line edit here plus an entry in redeploy.sh.
REDEPLOY_PLATFORM_SERVICES := \
	identity identity-proxy inference-gateway policy-service policy-gateway \
	egress-gateway agents frontend operator session-manager sandbox-exec
REDEPLOY_TENANT_SERVICES := example

OPA_ENVOY_IMAGE := openpolicyagent/opa:1.15.2-envoy-static

# ===========================================
# Image bake (CI / multi-arch publishing)
# ===========================================

.PHONY: docker-bake-build
docker-bake-build: ## Build images via docker buildx bake and load them into the local Docker daemon
	@echo "$(COLOR_YELLOW)Building $(BACKEND_IMAGE_GROUP) from $(DOCKER_BAKE_FILE) with tags $(IMAGE_REPOSITORY_PREFIX)*:$(IMAGE_TAG) [$(DOCKER_BUILD_PLATFORMS)]...$(COLOR_RESET)"
	@REGISTRY=$(IMAGE_REGISTRY) IMAGE_NAMESPACE=$(IMAGE_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) \
		docker buildx bake -f $(DOCKER_BAKE_FILE) \
			--set '*.platform=$(DOCKER_BUILD_PLATFORMS)' \
			--load $(BACKEND_IMAGE_GROUP)
	@echo "$(COLOR_GREEN)✓ Built images with tags $(IMAGE_REPOSITORY_PREFIX)*:$(IMAGE_TAG)$(COLOR_RESET)"

.PHONY: docker-bake-push
docker-bake-push: ## Build and push images via docker buildx bake (defaults to GHCR)
	@echo "$(COLOR_YELLOW)Publishing $(BACKEND_IMAGE_GROUP) from $(DOCKER_BAKE_FILE) to $(IMAGE_REPOSITORY_PREFIX)*:$(IMAGE_TAG) [$(DOCKER_PUSH_PLATFORMS)]...$(COLOR_RESET)"
	@REGISTRY=$(IMAGE_REGISTRY) IMAGE_NAMESPACE=$(IMAGE_NAMESPACE) IMAGE_TAG=$(IMAGE_TAG) \
		docker buildx bake -f $(DOCKER_BAKE_FILE) \
			--set '*.platform=$(DOCKER_PUSH_PLATFORMS)' \
			--push $(BACKEND_IMAGE_GROUP)
	@echo "$(COLOR_GREEN)✓ Published images to $(IMAGE_REPOSITORY_PREFIX)*:$(IMAGE_TAG)$(COLOR_RESET)"

.PHONY: docker-pull-third-party
docker-pull-third-party: ## Pull third-party sidecar images and push to local registry (OPA envoy)
	@echo "$(COLOR_YELLOW)Pulling $(OPA_ENVOY_IMAGE)...$(COLOR_RESET)"
	@docker pull $(OPA_ENVOY_IMAGE)
	@docker tag $(OPA_ENVOY_IMAGE) localhost:5001/$(OPA_ENVOY_IMAGE)
	@docker push localhost:5001/$(OPA_ENVOY_IMAGE)
	@echo "$(COLOR_GREEN)✓ localhost:5001/$(OPA_ENVOY_IMAGE)$(COLOR_RESET)"

# ===========================================
# Cluster bring-up + bulk operations
# ===========================================

# Build + push every platform image in BUILD_ONLY mode. Used by cluster-operator
# and cluster-update — anything that needs the local registry seeded but does
# its own restart logic later. Serial on purpose (parallel builds overload the
# Colima VM and cascade into mesh/Postgres failures). One prune at the end
# instead of per-service so cache pruning happens just once.
.PHONY: redeploy-platform
redeploy-platform: ## Build + push every platform service to the local registry (no rollout)
	@for svc in $(REDEPLOY_PLATFORM_SERVICES); do \
		bash scripts/cluster/redeploy.sh $$svc --build-only || exit $$?; \
	done
	@docker image prune -f > /dev/null

.PHONY: redeploy-tenant
redeploy-tenant: ## Build + push every tenant service to the local registry (no rollout)
	@for svc in $(REDEPLOY_TENANT_SERVICES); do \
		bash scripts/cluster/redeploy.sh $$svc --build-only || exit $$?; \
	done
	@docker image prune -f > /dev/null

.PHONY: redeploy-warm
redeploy-warm: redeploy-platform redeploy-tenant ## Pre-build every service to warm BuildKit caches (no rollout) — run once at start of session for fast subsequent redeploys
	@echo "$(COLOR_GREEN)✓ All caches warm — single-service redeploys should now be fast$(COLOR_RESET)"

# redeploy-platform-bake — opt-in alternative to redeploy-platform that drives
# `docker buildx bake` against docker-bake.{hcl,local.hcl}. Buildx (v0.22)
# doesn't expose a --no-parallel flag, so we serialize by invoking bake
# **once per target** in a shell loop. We still get the bake wins (per-target
# graph dedup of mod-download + COPY layers, single tagged image set, single
# config source), without the across-target parallelism that wedges Colima.
# Your memory documents that parallel image builds wedge the Colima VM.
.PHONY: redeploy-platform-bake
redeploy-platform-bake: ## (Experimental) Build + push every platform service via docker buildx bake (serial loop — one target at a time)
	@GIT_SHA="$$(git rev-parse --short HEAD 2>/dev/null || echo dev)"; \
	if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then GIT_SHA="$${GIT_SHA}-dirty"; fi; \
	echo "$(COLOR_YELLOW)Baking platform images (serial, GIT_SHA=$$GIT_SHA)...$(COLOR_RESET)"; \
	for tgt in orbit-identity orbit-identity-proxy orbit-inference-gateway orbit-policy-service orbit-policy-gateway orbit-egress-gateway orbit-agents orbit-frontend orbit-operator orbit-session-manager orbit-sandbox-exec; do \
		GIT_SHA="$$GIT_SHA" \
			docker buildx bake \
				-f docker-bake.hcl -f docker-bake.local.hcl \
				--load $$tgt || exit $$?; \
	done
	@if [ -f secrets/mkcert-ca.pem ]; then \
		echo "$(COLOR_YELLOW)Layering mkcert CA into orbit-identity...$(COLOR_RESET)"; \
		printf 'FROM localhost:5001/orbit-identity:latest\nCOPY mkcert-ca.pem /usr/local/share/ca-certificates/mkcert-ca.crt\nRUN update-ca-certificates\n' | \
			docker build -q -t localhost:5001/orbit-identity:latest -f - secrets/ > /dev/null; \
	fi
	@echo "$(COLOR_YELLOW)Pushing all platform images to localhost:5001...$(COLOR_RESET)"
	@for svc in $(REDEPLOY_PLATFORM_SERVICES); do \
		docker push --quiet localhost:5001/orbit-$$svc:latest > /dev/null && \
		echo "  ✓ orbit-$$svc"; \
	done
	@docker image prune -f > /dev/null
	@echo "$(COLOR_GREEN)✓ Platform images built and pushed via bake$(COLOR_RESET)"

.PHONY: redeploy-tenant-bake
redeploy-tenant-bake: ## (Experimental) Build + push every tenant service via docker buildx bake (serial — one target at a time)
	@GIT_SHA="$$(git rev-parse --short HEAD 2>/dev/null || echo dev)"; \
	if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then GIT_SHA="$${GIT_SHA}-dirty"; fi; \
	echo "$(COLOR_YELLOW)Baking tenant images (serial, GIT_SHA=$$GIT_SHA)...$(COLOR_RESET)"; \
	for tgt in orbit-example; do \
		GIT_SHA="$$GIT_SHA" \
			docker buildx bake \
				-f docker-bake.hcl -f docker-bake.local.hcl \
				--load $$tgt || exit $$?; \
	done
	@for svc in $(REDEPLOY_TENANT_SERVICES); do \
		docker push --quiet localhost:5001/orbit-$$svc:latest > /dev/null && \
		echo "  ✓ orbit-$$svc"; \
	done
	@docker image prune -f > /dev/null
	@echo "$(COLOR_GREEN)✓ Tenant images built and pushed via bake$(COLOR_RESET)"

# Re-push every locally-built image to the registry (no build). Recover from a
# Colima/k3d restart without rebuilding anything.
.PHONY: cluster-restore
cluster-restore: ## Re-push locally-built images to the registry after a Colima/k3d restart
	@echo "$(COLOR_YELLOW)Syncing locally-built images to local registry...$(COLOR_RESET)"
	@for svc in $(REDEPLOY_PLATFORM_SERVICES) $(REDEPLOY_TENANT_SERVICES); do \
		if docker image inspect orbit-$$svc:latest > /dev/null 2>&1; then \
			echo "  pushing orbit-$$svc:latest"; \
			docker tag orbit-$$svc:latest localhost:5001/orbit-$$svc:latest; \
			docker push localhost:5001/orbit-$$svc:latest > /dev/null; \
		fi; \
	done
	@if docker image inspect $(OPA_ENVOY_IMAGE) > /dev/null 2>&1; then \
		echo "  pushing $(OPA_ENVOY_IMAGE)"; \
		docker tag $(OPA_ENVOY_IMAGE) localhost:5001/$(OPA_ENVOY_IMAGE); \
		docker push localhost:5001/$(OPA_ENVOY_IMAGE) > /dev/null; \
	fi
	@docker image prune -f > /dev/null
	@echo "$(COLOR_GREEN)✓ Registry synced$(COLOR_RESET)"

.PHONY: cluster-operator
cluster-operator: redeploy-platform docker-pull-third-party ## Install the Orbit operator and bring up the full platform (SPIRE + Istio + Orbit services)
	@echo "$(COLOR_YELLOW)Installing Orbit operator...$(COLOR_RESET)"
	@helm upgrade --install orbit-operator charts/orbit-operator \
		-f deploy/local/operator-values.yaml \
		--namespace orbit-operator --create-namespace \
		--timeout 60s --wait
	@echo "$(COLOR_YELLOW)Applying OrbitPlatform CR...$(COLOR_RESET)"
	@kubectl apply -f deploy/local/orbit-platform.yaml
	@echo "  Waiting for orbit-system namespace (created by operator)..."
	@until kubectl get namespace orbit-system &>/dev/null; do sleep 3; done
	@echo "$(COLOR_YELLOW)Creating orbit-system secrets (DB + Redis + signing key)...$(COLOR_RESET)"
	@bash scripts/cluster/create-secrets.sh orbit-system
	@echo "$(COLOR_YELLOW)Creating mkcert CA ConfigMap for Dex TLS verification...$(COLOR_RESET)"
	@$(MAKE) cluster-ca-configmap
	@echo "$(COLOR_YELLOW)Resolving Docker container IPs and patching OrbitPlatform CR...$(COLOR_RESET)"
	@bash scripts/cluster/resolve-docker-ips.sh
	@echo "$(COLOR_YELLOW)Waiting for SPIRE + Istio (operator is installing)...$(COLOR_RESET)"
	@echo "  Waiting for spire-system namespace..."
	@until kubectl get namespace spire-system &>/dev/null; do sleep 3; done
	@echo "  Waiting for spire-agent daemonset..."
	@until kubectl get daemonset spire-agent -n spire-system &>/dev/null; do sleep 3; done
	@kubectl rollout status daemonset/spire-agent -n spire-system --timeout=300s
	@echo "  Waiting for istio-system namespace..."
	@until kubectl get namespace istio-system &>/dev/null; do sleep 3; done
	@echo "  Waiting for istiod deployment..."
	@until kubectl get deployment istiod -n istio-system &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/istiod -n istio-system --timeout=180s
	@echo "$(COLOR_YELLOW)Creating TLS secret for Istio gateway...$(COLOR_RESET)"
	@$(MAKE) cluster-tls-secret
	@echo "  Waiting for orbit-identity (operator reconciling orbit-config + orbit chart)..."
	@until kubectl get deployment orbit-identity -n orbit-system &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/orbit-identity -n orbit-system --timeout=180s
	@echo "  Waiting for orbit-frontend (operator reconciling orbit chart)..."
	@until kubectl get deployment orbit-frontend -n orbit-system &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/orbit-frontend -n orbit-system --timeout=180s
	@echo "$(COLOR_GREEN)✓ Platform ready — SPIRE, Istio, and Orbit services are running$(COLOR_RESET)"
	@echo "  Frontend:          https://orbit.local.clearroute.io:8443"
	@echo "  Inference gateway: https://api.orbit.local.clearroute.io:8443/v1"
	@echo "  (HTTP on :8880 redirects to HTTPS)"
	@echo "$(COLOR_YELLOW)Next: 'make cluster-deploy' to register tenants and deploy workloads$(COLOR_RESET)"

.PHONY: cluster-deploy
cluster-deploy: redeploy-tenant ## Build tenant images, register tenants, and deploy workloads
	@echo "$(COLOR_YELLOW)Deploying tenant workloads...$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)Registering tenants (applying OrbitTenant CRs)...$(COLOR_RESET)"
	@kubectl apply -f deploy/local/tenants/
	@echo "  Waiting for tenant namespaces (operator provisioning)..."
	@until kubectl get namespace orbit-tenant-example &>/dev/null; do sleep 3; done
	@echo "  Waiting for orbit-identity to load tenant OIDC config..."
	@until kubectl get configmap orbit-tenants -n orbit-system -o jsonpath='{.data.tenants\.yaml}' 2>/dev/null | grep -q "id: example"; do sleep 2; done
	@echo "$(COLOR_YELLOW)Creating tenant secrets...$(COLOR_RESET)"
	@bash scripts/cluster/create-secrets.sh orbit-tenant-example
	@echo "$(COLOR_YELLOW)Registering tenant connections (GitHub if an App key is present)...$(COLOR_RESET)"
	@if [ -f secrets/orbit-github-app.pem ]; then \
		kubectl create secret generic github-app-key -n orbit-tenant-example --from-file=key.pem=secrets/orbit-github-app.pem --dry-run=client -o yaml | kubectl apply -f - ; \
		kubectl apply -f deploy/local/connections/ ; \
		echo "  ✓ GitHub connection registered from secrets/orbit-github-app.pem"; \
	else \
		echo "  (no secrets/orbit-github-app.pem — skipping GitHub connection; it is optional)"; \
	fi
	@echo "$(COLOR_YELLOW)Applying tenant workload fixtures...$(COLOR_RESET)"
	@kubectl apply -f deploy/local/fixtures/
	@echo "$(COLOR_YELLOW)Waiting for example workload to be ready...$(COLOR_RESET)"
	@until kubectl get deployment orbit-example -n orbit-tenant-example &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/orbit-example -n orbit-tenant-example --timeout=180s
	@echo "$(COLOR_GREEN)✓ Tenant workloads deployed$(COLOR_RESET)"

.PHONY: cluster-update
cluster-update: redeploy-platform redeploy-tenant ## Rebuild every service image and restart deployments
	@echo "$(COLOR_YELLOW)Restarting all Orbit deployments...$(COLOR_RESET)"
	@kubectl rollout restart deployment -n orbit-system
	@kubectl rollout restart deployment -n orbit-tenant-example 2>/dev/null || true
	@echo "$(COLOR_YELLOW)Waiting for rollouts to complete...$(COLOR_RESET)"
	@kubectl rollout status deployment -n orbit-system --timeout=120s
	@kubectl rollout status deployment -n orbit-tenant-example --timeout=120s 2>/dev/null || true
	@echo "$(COLOR_GREEN)✓ All services updated with latest images$(COLOR_RESET)"

.PHONY: cluster-bedrock-creds
cluster-bedrock-creds: ## Refresh AWS Bedrock credentials in the cluster from your local SSO session (set AWS_PROFILE in .env)
	@bash scripts/cluster/refresh-bedrock-creds.sh

# ===========================================
# Redeploy — single dispatcher for "I changed one service, push it"
# ===========================================
#
# All targets below funnel through scripts/cluster/redeploy.sh, which holds the
# per-service registry (Dockerfile, build context, build args, namespace,
# workload kind/name, post-build hooks). Adding a new service is a single edit
# in that script — Makefile just exposes shortcuts.
#
# Usage:
#   make redeploy SERVICE=identity                       # build + push + rollout
#   make redeploy SERVICE=session-manager TENANT=example # tenant-scoped
#   make redeploy SERVICE=sandbox-exec BUILD_ONLY=1      # build + push only
#   make redeploy SERVICE=identity NO_BUILD=1            # rollout only
#
# Or use the per-service shortcut targets (redeploy-identity, redeploy-frontend, …).

REDEPLOY_FLAGS := \
	$(if $(TENANT),--tenant $(TENANT)) \
	$(if $(SLUG),--slug $(SLUG)) \
	$(if $(BUILD_ONLY),--build-only) \
	$(if $(NO_BUILD),--no-build)

.PHONY: redeploy
redeploy: ## Build, push, and restart a single service (usage: make redeploy SERVICE=identity [TENANT=…] [BUILD_ONLY=1] [NO_BUILD=1])
	$(if $(SERVICE),,$(error SERVICE is required — try: make redeploy-list))
	@bash scripts/cluster/redeploy.sh $(SERVICE) $(REDEPLOY_FLAGS)

.PHONY: redeploy-list
redeploy-list: ## List every service known to `make redeploy`
	@bash scripts/cluster/redeploy.sh --help 2>&1 | sed -n '/^Services:/,/^Flags:/p' | sed '$$d'

.PHONY: redeploy-status
redeploy-status: ## Show drift between cluster-deployed images and the local registry
	@bash scripts/cluster/redeploy-status.sh

.PHONY: redeploy-identity
redeploy-identity: ## Redeploy identity (rebuilds with mkcert CA layer for Dex TLS)
	@bash scripts/cluster/redeploy.sh identity $(REDEPLOY_FLAGS)

.PHONY: redeploy-identity-proxy
redeploy-identity-proxy: ## Redeploy identity-proxy sidecar (deletes injected pods so the operator re-injects)
	@bash scripts/cluster/redeploy.sh identity-proxy $(REDEPLOY_FLAGS)

.PHONY: redeploy-inference-gateway
redeploy-inference-gateway: ## Redeploy the inference gateway
	@bash scripts/cluster/redeploy.sh inference-gateway $(REDEPLOY_FLAGS)

.PHONY: redeploy-policy-service
redeploy-policy-service: ## Redeploy the policy service (PDP/PAP)
	@bash scripts/cluster/redeploy.sh policy-service $(REDEPLOY_FLAGS)

.PHONY: redeploy-policy-gateway
redeploy-policy-gateway: ## Redeploy the policy gateway (PEP)
	@bash scripts/cluster/redeploy.sh policy-gateway $(REDEPLOY_FLAGS)

.PHONY: redeploy-egress-gateway
redeploy-egress-gateway: ## Redeploy the egress gateway (per-tenant credential broker)
	@bash scripts/cluster/redeploy.sh egress-gateway $(REDEPLOY_FLAGS)

.PHONY: redeploy-agents
redeploy-agents: ## Redeploy orbit-agents (agent control plane)
	@bash scripts/cluster/redeploy.sh agents $(REDEPLOY_FLAGS)

.PHONY: redeploy-frontend
redeploy-frontend: ## Redeploy the frontend
	@bash scripts/cluster/redeploy.sh frontend $(REDEPLOY_FLAGS)

.PHONY: redeploy-operator
redeploy-operator: ## Redeploy the orbit-operator (charts are baked into the image)
	@bash scripts/cluster/redeploy.sh operator $(REDEPLOY_FLAGS)

.PHONY: redeploy-example
redeploy-example: ## Redeploy the example tenant workload (default tenant=example, override with TENANT=…)
	@bash scripts/cluster/redeploy.sh example $(REDEPLOY_FLAGS)

.PHONY: redeploy-session-manager
redeploy-session-manager: ## Redeploy session-manager (statefulset, default tenant=example, override with TENANT=… SLUG=…)
	@bash scripts/cluster/redeploy.sh session-manager $(REDEPLOY_FLAGS)

.PHONY: redeploy-sandbox-exec
redeploy-sandbox-exec: ## Build + push sandbox-exec (no rollout — picked up by next agent run)
	@bash scripts/cluster/redeploy.sh sandbox-exec $(REDEPLOY_FLAGS)

.PHONY: prune
prune: ## Prune dangling Docker images and cap the BuildKit cache at 5 GB
	@echo "$(COLOR_YELLOW)Pruning dangling images...$(COLOR_RESET)"
	@docker image prune -f
	@echo "$(COLOR_YELLOW)Pruning BuildKit cache (keeping ≤5 GB)...$(COLOR_RESET)"
	@docker buildx prune -f --keep-storage 5GB
	@echo "$(COLOR_GREEN)✓ Pruned$(COLOR_RESET)"

# ===========================================
# Development Workflow
# ===========================================

.PHONY: dev
dev: ## Run identity service in development mode with auto-reload (air)
	@echo "$(COLOR_YELLOW)Starting identity service in development mode...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
	export GODEBUG=netdns=cgo; \
	if command -v air > /dev/null; then \
		air; \
	else \
		echo "$(COLOR_BLUE)Air not installed. Running without live reload...$(COLOR_RESET)"; \
		echo "$(COLOR_BLUE)Install air for live reload: make install-tools$(COLOR_RESET)"; \
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/identity/main.go; \
	fi

.PHONY: run
run: run-identity ## Alias for run-identity

.PHONY: run-identity
run-identity: ## Run the identity service
	@echo "$(COLOR_YELLOW)Starting identity service...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
		cd $(BACKEND_DIR) && GODEBUG=netdns=cgo $(GOCMD) run ./cmd/identity/main.go

.PHONY: run-inference-gateway
run-inference-gateway: ## Run the inference gateway (set provider env vars first — see backend/cmd/inference-gateway/config/local.yaml)
	@echo "$(COLOR_YELLOW)Starting inference gateway on :8081...$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)  JWKS: $${ORBIT_TOKEN_JWKS_URL:-http://localhost:8080/api/v1/identity/.well-known/jwks.json}$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)  Tip (Bedrock):  export BEDROCK_ENABLED=true BEDROCK_REGION=us-east-1 AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)  Tip (Vertex):   gcloud auth application-default login  # once; then set VERTEX_ENABLED=true VERTEX_PROJECT_ID=... VERTEX_LOCATION=us-central1$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
	export ORBIT_TOKEN_JWKS_URL=$${ORBIT_TOKEN_JWKS_URL:-http://localhost:8080/api/v1/identity/.well-known/jwks.json}; \
	export APP_ENV=local; \
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/inference-gateway/main.go

.PHONY: run-example
run-example: ## Run the example service
	@echo "$(COLOR_YELLOW)Starting example service...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/example/main.go

.PHONY: build-backend
build-backend: proto-gen ## Build all service binaries to .bin directory (runs proto-gen first)
	@echo "$(COLOR_YELLOW)Building service binaries...$(COLOR_RESET)"
	@mkdir -p $(BINARY_DIR)
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-identity ./cmd/identity
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-example ./cmd/example
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-agents ./cmd/orbit-agents
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-session-manager ./cmd/session-manager
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-shell ./cmd/orbit-shell
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-runtime-mcp ./cmd/orbit-runtime-mcp
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/sandbox-exec ./cmd/sandbox-exec
	@echo "$(COLOR_GREEN)✓ Binaries built to $(BINARY_DIR)/$(COLOR_RESET)"

# ===========================================
# Testing & Quality
# ===========================================

.PHONY: test-integration
test-integration: auth-refresh-k3d ## Run integration tests (requires dev-cluster to be running)
	@echo "$(COLOR_YELLOW)Running integration tests...$(COLOR_RESET)"
	@kubectl port-forward svc/orbit-policy-service -n orbit-system 8083:80 >/tmp/orbit-pf-policy.log 2>&1 & \
		PF_PID=$$!; \
		sleep 2; \
		cd $(BACKEND_DIR) && $(GOTEST) -v -race -timeout=5m -tags=integration ./...; \
		STATUS=$$?; \
		kill $$PF_PID 2>/dev/null || true; \
		if [ $$STATUS -eq 0 ]; then echo "$(COLOR_GREEN)✓ Integration tests passed$(COLOR_RESET)"; fi; \
		exit $$STATUS

.PHONY: test
test: ## Run unit tests with coverage (no cluster required)
	@echo "$(COLOR_YELLOW)Running tests...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOTEST) -race -timeout=5m -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "$(COLOR_BOLD)Total coverage:$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOCMD) tool cover -func=coverage.out | tail -n 1

.PHONY: test-verbose
test-verbose: ## Run tests with verbose output
	@echo "$(COLOR_YELLOW)Running tests (verbose)...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOTEST) -v -race -timeout=5m -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "$(COLOR_BOLD)Coverage report:$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOCMD) tool cover -func=coverage.out

.PHONY: test-quiet
test-quiet: ## Run tests with minimal output (CI/agent-friendly)
	@echo "$(COLOR_YELLOW)Running tests (quiet mode)...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOTEST) -race -timeout=5m -coverprofile=coverage.out -covermode=atomic ./... 2>&1 | \
		grep -E '(PASS|FAIL|coverage:|^ok|^FAIL)' || true
	@echo ""
	@cd $(BACKEND_DIR) && $(GOCMD) tool cover -func=coverage.out | tail -n 1

.PHONY: test-coverage
test-coverage: test ## Run tests and generate HTML coverage report
	@cd $(BACKEND_DIR) && $(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(COLOR_GREEN)✓ Coverage report: backend/coverage.html$(COLOR_RESET)"

.PHONY: secrets
secrets: ## Scan for secrets and leaked credentials (gitleaks)
	@echo "$(COLOR_YELLOW)Scanning for secrets...$(COLOR_RESET)"
	@if command -v gitleaks > /dev/null; then \
		gitleaks detect --source . --config .gitleaks.toml --redact; \
		echo "$(COLOR_GREEN)✓ No secrets detected$(COLOR_RESET)"; \
	else \
		echo "$(COLOR_YELLOW)gitleaks not installed. Run 'make install-tools'$(COLOR_RESET)"; \
		exit 1; \
	fi

.PHONY: lint
lint: ## Run golangci-lint
	@echo "$(COLOR_YELLOW)Running linters...$(COLOR_RESET)"
	@if command -v golangci-lint > /dev/null; then \
		if cd $(BACKEND_DIR) && $(GOLINT) run ./...; then \
			echo "$(COLOR_GREEN)✓ Linting passed$(COLOR_RESET)"; \
		else \
			echo "$(COLOR_RESET)"; \
			exit 1; \
		fi \
	else \
		echo "$(COLOR_YELLOW)golangci-lint not installed. Run 'make install-tools'$(COLOR_RESET)"; \
		exit 1; \
	fi

.PHONY: fmt
fmt: ## Format code with gofmt
	@echo "$(COLOR_YELLOW)Formatting code...$(COLOR_RESET)"
	@$(GOFMT) -s -w $(BACKEND_DIR)
	@cd $(BACKEND_DIR) && $(GOCMD) fmt ./...
	@echo "$(COLOR_GREEN)✓ Code formatted$(COLOR_RESET)"

.PHONY: vet
vet: ## Run go vet
	@echo "$(COLOR_YELLOW)Running go vet...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOCMD) vet ./...
	@echo "$(COLOR_GREEN)✓ Vet passed$(COLOR_RESET)"

.PHONY: fix
fix: ## Run go fix
	@echo "$(COLOR_YELLOW)Running go fix...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOCMD) fix ./...
	@echo "$(COLOR_GREEN)✓ Fixes applied$(COLOR_RESET)"

# ===========================================
# Frontend
# ===========================================

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies (enforces nvm or fnm + Node LTS + pnpm, idempotent)
	@echo "$(COLOR_YELLOW)Setting up frontend environment...$(COLOR_RESET)"
	@# ── 1. Check for nvm or fnm ────────────────────────────────────────────────
	@export NVM_DIR="$${NVM_DIR:-$$HOME/.nvm}"; \
	if command -v fnm > /dev/null 2>&1; then \
		eval "$$(fnm env)"; \
		NODE_MANAGER="fnm"; \
	elif [ -f "$$NVM_DIR/nvm.sh" ]; then \
		. "$$NVM_DIR/nvm.sh"; \
		NODE_MANAGER="nvm"; \
	else \
		echo "$(COLOR_RED)✗ Neither nvm nor fnm found$(COLOR_RESET)"; \
		echo "  Install nvm: https://github.com/nvm-sh/nvm"; \
		echo "  Or install fnm: https://github.com/Schniz/fnm"; \
		exit 1; \
	fi; \
	echo "  Using $$NODE_MANAGER for Node management..."; \
	echo "  Ensuring Node $(NODE_VERSION) via $$NODE_MANAGER (skipped if already installed)..."; \
	$$NODE_MANAGER install $(NODE_VERSION); \
	$$NODE_MANAGER use $(NODE_VERSION); \
	echo "  Enabling Corepack..."; \
	corepack enable; \
	if pnpm --version 2>/dev/null | grep -q "^$(PNPM_VERSION)$$"; then \
		echo "  pnpm $(PNPM_VERSION) already installed — skipping"; \
	else \
		echo "  Installing pnpm $(PNPM_VERSION) via Corepack..."; \
		corepack install -g pnpm@$(PNPM_VERSION); \
	fi; \
	cd $(FRONTEND_DIR) && pnpm install $(if $(CI),--frozen-lockfile,)
	@echo "$(COLOR_GREEN)✓ Frontend dependencies installed$(COLOR_RESET)"

.PHONY: frontend-dev
frontend-dev: ## Start the frontend dev server (Next.js, port 3000)
	@echo "$(COLOR_YELLOW)Starting frontend dev server...$(COLOR_RESET)"
	@set -a && . ./.env && set +a && cd $(FRONTEND_DIR) && pnpm dev

.PHONY: frontend-build
frontend-build: ## Build the frontend for production
	@echo "$(COLOR_YELLOW)Building frontend...$(COLOR_RESET)"
	@set -a && . ./.env && set +a && cd $(FRONTEND_DIR) && pnpm build
	@echo "$(COLOR_GREEN)✓ Frontend built$(COLOR_RESET)"

.PHONY: frontend-lint
frontend-lint: ## Lint the frontend (eslint)
	@echo "$(COLOR_YELLOW)Linting frontend...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm lint
	@echo "$(COLOR_GREEN)✓ Frontend lint passed$(COLOR_RESET)"

.PHONY: frontend-type-check
frontend-type-check: ## Type-check the frontend (tsc --noEmit)
	@echo "$(COLOR_YELLOW)Type-checking frontend...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm type-check
	@echo "$(COLOR_GREEN)✓ Frontend type-check passed$(COLOR_RESET)"

.PHONY: frontend-fmt
frontend-fmt: ## Format frontend code (prettier --write locally, --check in CI)
	@echo "$(COLOR_YELLOW)Formatting frontend...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm exec prettier $(if $(CI),--check,--write) .
	@echo "$(COLOR_GREEN)✓ Frontend formatting OK$(COLOR_RESET)"

.PHONY: frontend-test-unit
frontend-test-unit: ## Run frontend unit tests (Terra first, then app)
	@echo "$(COLOR_YELLOW)Running frontend unit tests...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra run test:unit
	@cd $(FRONTEND_DIR) && pnpm run test:unit
	@echo "$(COLOR_GREEN)✓ Frontend unit tests passed$(COLOR_RESET)"

.PHONY: frontend-test-integration
frontend-test-integration: ## Run frontend integration tests (MSW + jsdom)
	@echo "$(COLOR_YELLOW)Running frontend integration tests...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm run test:integration
	@echo "$(COLOR_GREEN)✓ Frontend integration tests passed$(COLOR_RESET)"

.PHONY: frontend-test-terra
frontend-test-terra: ## Run Terra component unit tests in isolation (happy-dom)
	@echo "$(COLOR_YELLOW)Running Terra unit tests...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra run test:unit
	@echo "$(COLOR_GREEN)✓ Terra unit tests passed$(COLOR_RESET)"

.PHONY: frontend-test-e2e
frontend-test-e2e: ## Run frontend E2E tests (Playwright + Chromium, spins up Next.js dev server)
	@echo "$(COLOR_YELLOW)Running frontend E2E tests...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm run test:e2e
	@echo "$(COLOR_GREEN)✓ Frontend E2E tests passed$(COLOR_RESET)"

.PHONY: frontend-test
frontend-test: frontend-test-unit frontend-test-integration ## Run all frontend tests (unit + integration; use frontend-test-e2e for Playwright)

.PHONY: frontend-check
frontend-check: frontend-lint frontend-type-check frontend-fmt frontend-test ## Run all frontend checks (eslint + tsc + prettier + tests)

.PHONY: frontend-clean
frontend-clean: ## Remove frontend build artifacts and installed dependencies
	@echo "$(COLOR_YELLOW)Cleaning frontend...$(COLOR_RESET)"
	@rm -rf $(FRONTEND_DIR)/.next $(FRONTEND_DIR)/node_modules
	@echo "$(COLOR_GREEN)✓ Frontend cleaned$(COLOR_RESET)"

.PHONY: frontend-storybook
frontend-storybook: ## Start Storybook — Terra + App/Shared components (port 6006)
	@echo "$(COLOR_YELLOW)Starting Storybook...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm storybook

.PHONY: frontend-storybook-build
frontend-storybook-build: ## Build Storybook as a static site
	@echo "$(COLOR_YELLOW)Building Storybook...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm build-storybook
	@echo "$(COLOR_GREEN)✓ Storybook built$(COLOR_RESET)"

.PHONY: frontend-storybook-terra
frontend-storybook-terra: ## Start Terra-isolated Storybook (Terra package only, port 6006)
	@echo "$(COLOR_YELLOW)Starting Terra-isolated Storybook...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra storybook

# ===========================================
# Cleanup
# ===========================================

.PHONY: clean
clean: ## Clean build artifacts and temporary files
	@echo "$(COLOR_YELLOW)Cleaning build artifacts...$(COLOR_RESET)"
	@rm -rf $(BINARY_DIR)
	@cd $(BACKEND_DIR) && $(GOCLEAN)
	@cd $(BACKEND_DIR) && rm -f coverage.out coverage.html
	@echo "$(COLOR_GREEN)✓ Clean complete$(COLOR_RESET)"

.PHONY: clean-all
clean-all: clean ## Clean everything including database volumes
	@echo "$(COLOR_YELLOW)Cleaning all artifacts and stopping services...$(COLOR_RESET)"
	@docker compose down -v
	@echo "$(COLOR_GREEN)✓ All clean$(COLOR_RESET)"

# ===========================================
# Identity & Auth
# ===========================================

.PHONY: auth-device
auth-device: ## Authenticate via Device Authorization Grant (local binary on :6069)
	@chmod +x scripts/auth-device.sh
	@./scripts/auth-device.sh \
		--api-base "$${ORBIT_API_BASE:-http://localhost:8080/api/v1}" \
		--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"

.PHONY: auth-device-k3d
auth-device-k3d: ## Authenticate via Device Authorization Grant (k3d cluster via port-forward)
	@chmod +x scripts/auth-device.sh
	@echo "$(COLOR_CYAN)→ Starting port-forward orbit-identity → localhost:6069$(COLOR_RESET)"
	@kubectl port-forward svc/orbit-identity -n orbit-system 6069:80 >/tmp/orbit-pf.log 2>&1 & \
		PF_PID=$$!; \
		sleep 2; \
		./scripts/auth-device.sh \
			--api-base "http://localhost:6069/api/v1" \
			--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"; \
		STATUS=$$?; \
		kill $$PF_PID 2>/dev/null || true; \
		exit $$STATUS

.PHONY: auth-refresh
auth-refresh: ## Refresh the access token in /tmp/orbit_credentials.json (local binary on :8080)
	@chmod +x scripts/auth-refresh.sh
	@./scripts/auth-refresh.sh \
		--api-base "$${ORBIT_API_BASE:-http://localhost:8080/api/v1}" \
		--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"

.PHONY: auth-refresh-k3d
auth-refresh-k3d: ## Refresh the access token in /tmp/orbit_credentials.json (k3d cluster via port-forward)
	@chmod +x scripts/auth-refresh.sh
	@echo "$(COLOR_CYAN)→ Starting port-forward orbit-identity → localhost:6069$(COLOR_RESET)"
	@kubectl port-forward svc/orbit-identity -n orbit-system 6069:80 >/tmp/orbit-pf.log 2>&1 & \
		PF_PID=$$!; \
		./scripts/auth-refresh.sh \
			--api-base "http://localhost:6069/api/v1" \
			--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"; \
		STATUS=$$?; \
		kill $$PF_PID 2>/dev/null || true; \
		exit $$STATUS

.PHONY: setup-tls
setup-tls: ## Generate a locally-trusted TLS cert for *.orbit.local.clearroute.io (installs mkcert + CA if needed)
	@chmod +x scripts/setup-tls.sh
	@bash scripts/setup-tls.sh

.PHONY: cluster-tls-secret
cluster-tls-secret: ## Create the orbit-gateway-tls TLS secret in istio-system (run after setup-tls)
	@[ -f secrets/tls.crt ] && [ -f secrets/tls.key ] || \
		(echo "$(COLOR_RED)✗ secrets/tls.crt / secrets/tls.key not found — run 'make setup-tls' first$(COLOR_RESET)" && exit 1)
	@kubectl create secret tls orbit-gateway-tls \
		--cert=secrets/tls.crt \
		--key=secrets/tls.key \
		--namespace istio-system \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "$(COLOR_GREEN)✓ orbit-gateway-tls secret applied in istio-system$(COLOR_RESET)"

.PHONY: cluster-ca-configmap
cluster-ca-configmap: ## Create a ConfigMap with the mkcert root CA in orbit-system (for Dex TLS verification)
	@[ -f secrets/mkcert-ca.pem ] || \
		(echo "$(COLOR_RED)✗ secrets/mkcert-ca.pem not found — run 'make setup-tls' first$(COLOR_RESET)" && exit 1)
	@kubectl create configmap mkcert-ca \
		--from-file=mkcert-ca.pem=secrets/mkcert-ca.pem \
		--namespace orbit-system \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "$(COLOR_GREEN)✓ mkcert-ca ConfigMap applied in orbit-system$(COLOR_RESET)"

.PHONY: gen-signing-key
gen-signing-key: ## Write a 4096-bit RSA signing key to secrets/signing.key
	@mkdir -p secrets
	@openssl genrsa -out secrets/signing.key 4096 2>/dev/null
	@chmod 600 secrets/signing.key
	@echo ""
	@echo "$(COLOR_GREEN)✓ Key written to secrets/signing.key (mode 600)$(COLOR_RESET)"
	@echo ""
	@echo "Make sure your .env contains:"
	@echo ""
	@echo "  ORBIT_TOKEN_SIGNING_KEY_FILE=secrets/signing.key"
	@echo ""

.PHONY: dex-hash
dex-hash: ## Write config/dex/.env with a fresh bcrypt hash  (usage: make dex-hash password=yourpassword)
	$(if $(password),,$(error password is required — usage: make dex-hash password=yourpassword))
	@echo "$(COLOR_YELLOW)Generating bcrypt hash for '$(password)'...$(COLOR_RESET)"
	@mkdir -p config/dex
	@HASH=$$(docker run --rm httpd:alpine htpasswd -bnBC 10 "" "$(password)" 2>/dev/null | tr -d ':\n'); \
	ESCAPED=$$(printf '%s' "$$HASH" | sed 's/[$$]/&&/g'); \
	printf 'DEX_USER_PASSWORD_HASH=%s\n' "$$ESCAPED" > config/dex/.env; \
	echo "$(COLOR_GREEN)✓ config/dex/.env written$(COLOR_RESET)"; \
	echo "  Test user: dev@orbit.local.clearroute.io / $(password)"

.PHONY: dex-env
dex-env: ## Bootstrap config/dex/.env with the default dev password (no-op if it already exists)
	@if [ -f config/dex/.env ]; then \
		echo "$(COLOR_YELLOW)config/dex/.env already exists, skipping$(COLOR_RESET)"; \
	else \
		$(MAKE) dex-hash password=password; \
	fi

# ===========================================
# Convenience Targets
# ===========================================

.PHONY: check
check: tidy fmt vet fix lint secrets test frontend-check ## Run all checks (tidy, fmt, vet, lint, secrets, test, frontend-check)
	@echo "$(COLOR_GREEN)✓ All checks passed$(COLOR_RESET)"

.PHONY: all
all: tidy check build-backend ## Run all quality checks and build

.PHONY: bootstrap
bootstrap: env setup dev-up migrate-up ## Bootstrap project (scaffold .env, setup tools, start services, run migrations)
	@echo "$(COLOR_GREEN)✓ Project bootstrapped and ready$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)Run 'make dev' to start Orbit in development mode$(COLOR_RESET)"

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

# Database connection string
DB_URL=postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Colima (Lima-based Docker runtime on macOS) uses 192.168.5.2 as its internal DNS,
# which conflicts with k3d's DNS-fix behaviour. Auto-detect Colima and disable the fix.
# Developers on Docker Desktop can override this in .env: K3D_FIX_DNS=1
K3D_FIX_DNS ?= $(if $(shell which colima 2>/dev/null),0,)

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
setup: install-tools install-tools-cluster tidy frontend-install hooks setup-dns ## Complete project setup (install tools, tidy dependencies, install hooks)
	@echo "$(COLOR_GREEN)✓ Setup complete$(COLOR_RESET)"

.PHONY: setup-dns
setup-dns: ## Configure *.orbit.local DNS on macOS via dnsmasq (idempotent, no-op on Linux)
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
	if git show-ref --verify --quiet "refs/heads/$(BRANCH)"; then \
		git worktree add "$$WORKTREE_DIR" "$(BRANCH)"; \
	elif git show-ref --verify --quiet "refs/remotes/origin/$(BRANCH)"; then \
		git worktree add --track -b "$(BRANCH)" "$$WORKTREE_DIR" "origin/$(BRANCH)"; \
	else \
		git worktree add -b "$(BRANCH)" "$$WORKTREE_DIR"; \
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
	echo "    cd $$WORKTREE_DIR && make dev-up"

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
install-tools: ## Install development tools (goose, golangci-lint, air, gitleaks)
	@echo "$(COLOR_YELLOW)Installing development tools...$(COLOR_RESET)"
	@echo "  Installing goose $(GOOSE_VERSION)..."
	@$(GOCMD) install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	@echo "  Installing air $(AIR_VERSION)..."
	@$(GOCMD) install github.com/air-verse/air@$(AIR_VERSION)
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
dev-up: ## Start Postgres + Redis + Dex (local OIDC provider) for daily Go development
	@if [ ! -f secrets/signing.key ]; then \
		echo "$(COLOR_YELLOW)→ secrets/signing.key not found — generating RSA signing key...$(COLOR_RESET)"; \
		$(MAKE) --no-print-directory gen-signing-key; \
	fi
	@if [ ! -f config/dex/.env ]; then \
		echo "$(COLOR_YELLOW)→ config/dex/.env not found — generating Dex credentials (password: 'password')...$(COLOR_RESET)"; \
		$(MAKE) --no-print-directory dex-env; \
	fi
	@echo "$(COLOR_YELLOW)Starting local services (Postgres + Redis + Dex)...$(COLOR_RESET)"
	@docker compose up -d --wait postgres redis dex
	@echo "$(COLOR_GREEN)✓ Postgres + Redis + Dex ready$(COLOR_RESET)"
	@echo "  Dex OIDC issuer:  http://dex.orbit.local:5556/dex"
	@echo "  Test user:        dev@orbit.local / password"
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

# PLATFORM_SERVICES are orbit-system services owned by the operator.
# They must be built before 'make cluster-operator' so the operator can pull them.
PLATFORM_SERVICES := identity identity-proxy inference-gateway

# TENANT_SERVICES are workload images deployed into tenant namespaces.
# They are built during 'make cluster-deploy'.
TENANT_SERVICES := example

.PHONY: docker-build
docker-build: docker-build-platform docker-build-tenant ## Build all service Docker images

.PHONY: docker-build-platform
docker-build-platform: ## Build platform service images (identity, identity-proxy)
	@for svc in $(PLATFORM_SERVICES); do \
		echo "$(COLOR_YELLOW)Building orbit-$$svc:latest...$(COLOR_RESET)"; \
		docker build --build-arg SERVICE=$$svc -t orbit-$$svc:latest -f backend/Dockerfile backend/; \
		echo "$(COLOR_GREEN)✓ orbit-$$svc:latest$(COLOR_RESET)"; \
	done

.PHONY: docker-build-tenant
docker-build-tenant: ## Build tenant workload images (example)
	@for svc in $(TENANT_SERVICES); do \
		echo "$(COLOR_YELLOW)Building orbit-$$svc:latest...$(COLOR_RESET)"; \
		docker build --build-arg SERVICE=$$svc -t orbit-$$svc:latest -f backend/Dockerfile backend/; \
		echo "$(COLOR_GREEN)✓ orbit-$$svc:latest$(COLOR_RESET)"; \
	done

.PHONY: docker-push
docker-push: docker-push-platform docker-push-tenant ## Push all service images to local registry

.PHONY: docker-push-platform
docker-push-platform: ## Push platform service images to local registry
	@for svc in $(PLATFORM_SERVICES); do \
		echo "$(COLOR_YELLOW)Pushing orbit-$$svc to local registry...$(COLOR_RESET)"; \
		docker tag orbit-$$svc:latest localhost:5001/orbit-$$svc:latest; \
		docker push localhost:5001/orbit-$$svc:latest; \
		echo "$(COLOR_GREEN)✓ localhost:5001/orbit-$$svc:latest$(COLOR_RESET)"; \
	done

.PHONY: docker-push-tenant
docker-push-tenant: ## Push tenant workload images to local registry
	@for svc in $(TENANT_SERVICES); do \
		echo "$(COLOR_YELLOW)Pushing orbit-$$svc to local registry...$(COLOR_RESET)"; \
		docker tag orbit-$$svc:latest localhost:5001/orbit-$$svc:latest; \
		docker push localhost:5001/orbit-$$svc:latest; \
		echo "$(COLOR_GREEN)✓ localhost:5001/orbit-$$svc:latest$(COLOR_RESET)"; \
	done

.PHONY: docker-build-operator
docker-build-operator: ## Build the orbit-operator Docker image (uses Dockerfile.operator — bakes charts into image)
	@echo "$(COLOR_YELLOW)Building orbit-operator:latest...$(COLOR_RESET)"
	@docker build -t orbit-operator:latest -f Dockerfile.operator .
	@echo "$(COLOR_GREEN)✓ orbit-operator:latest$(COLOR_RESET)"

.PHONY: docker-push-operator
docker-push-operator: ## Push orbit-operator image to local registry
	@echo "$(COLOR_YELLOW)Pushing orbit-operator to local registry...$(COLOR_RESET)"
	@docker tag orbit-operator:latest localhost:5001/orbit-operator:latest
	@docker push localhost:5001/orbit-operator:latest
	@echo "$(COLOR_GREEN)✓ localhost:5001/orbit-operator:latest$(COLOR_RESET)"

.PHONY: cluster-operator
cluster-operator: docker-build-operator docker-push-operator docker-build-platform docker-push-platform ## Install the Orbit operator and bring up the full platform (SPIRE + Istio + Orbit services)
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
	@echo "  Waiting for orbit-identity (operator reconciling orbit-config + orbit chart)..."
	@until kubectl get deployment orbit-identity -n orbit-system &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/orbit-identity -n orbit-system --timeout=180s
	@echo "$(COLOR_GREEN)✓ Platform ready — SPIRE, Istio, and Orbit services are running$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)Next: 'make cluster-deploy' to register tenants and deploy workloads$(COLOR_RESET)"

.PHONY: cluster-update-operator
cluster-update-operator: docker-build-operator docker-push-operator ## Rebuild and redeploy the operator (e.g. after changing reconciler logic)
	@echo "$(COLOR_YELLOW)Upgrading Orbit operator...$(COLOR_RESET)"
	@helm upgrade --install orbit-operator charts/orbit-operator \
		-f deploy/local/operator-values.yaml \
		--namespace orbit-operator \
		--timeout 60s --wait
	@kubectl rollout restart deployment/orbit-operator -n orbit-operator
	@kubectl rollout status deployment/orbit-operator -n orbit-operator --timeout=60s
	@echo "$(COLOR_GREEN)✓ orbit-operator updated$(COLOR_RESET)"

.PHONY: cluster-deploy
cluster-deploy: docker-build-tenant docker-push-tenant ## Build tenant images, register tenants, and deploy workloads
	@echo "$(COLOR_YELLOW)Deploying tenant workloads...$(COLOR_RESET)"
	@echo "$(COLOR_YELLOW)Registering tenants (applying OrbitTenant CRs)...$(COLOR_RESET)"
	@kubectl apply -f deploy/local/tenants/
	@echo "  Waiting for tenant namespaces (operator provisioning)..."
	@until kubectl get namespace orbit-tenant-example &>/dev/null; do sleep 3; done
	@echo "  Waiting for orbit-identity to load tenant OIDC config..."
	@until kubectl get configmap orbit-tenants -n orbit-system -o jsonpath='{.data.tenants\.yaml}' 2>/dev/null | grep -q "id: example"; do sleep 2; done
	@echo "$(COLOR_YELLOW)Creating tenant secrets...$(COLOR_RESET)"
	@bash scripts/cluster/create-secrets.sh orbit-tenant-example
	@echo "$(COLOR_YELLOW)Applying tenant workload fixtures...$(COLOR_RESET)"
	@kubectl apply -f deploy/local/fixtures/
	@echo "$(COLOR_YELLOW)Waiting for example workload to be ready...$(COLOR_RESET)"
	@until kubectl get deployment orbit-example -n orbit-tenant-example &>/dev/null; do sleep 3; done
	@kubectl rollout status deployment/orbit-example -n orbit-tenant-example --timeout=180s
	@echo "$(COLOR_GREEN)✓ Tenant workloads deployed$(COLOR_RESET)"

.PHONY: cluster-update
cluster-update: docker-build docker-push ## Rebuild service images and restart deployments
	@echo "$(COLOR_YELLOW)Updating Orbit services in cluster...$(COLOR_RESET)"
	@kubectl rollout restart deployment -n orbit-system
	@kubectl rollout restart deployment -n orbit-tenant-example 2>/dev/null || true
	@echo "$(COLOR_YELLOW)Waiting for rollouts to complete...$(COLOR_RESET)"
	@kubectl rollout status deployment -n orbit-system --timeout=120s
	@kubectl rollout status deployment -n orbit-tenant-example --timeout=120s 2>/dev/null || true
	@echo "$(COLOR_GREEN)✓ All services updated with latest images$(COLOR_RESET)"

.PHONY: cluster-update-service
cluster-update-service: ## Build, push and restart a single service (usage: make cluster-update-service SERVICE=identity)
	$(if $(SERVICE),,$(error SERVICE is required — usage: make cluster-update-service SERVICE=identity))
	@echo "$(COLOR_YELLOW)Building orbit-$(SERVICE):latest...$(COLOR_RESET)"
	@docker build --build-arg SERVICE=$(SERVICE) -t orbit-$(SERVICE):latest -f backend/Dockerfile backend/
	@echo "$(COLOR_YELLOW)Pushing orbit-$(SERVICE) to local registry...$(COLOR_RESET)"
	@docker tag orbit-$(SERVICE):latest localhost:5001/orbit-$(SERVICE):latest
	@docker push localhost:5001/orbit-$(SERVICE):latest
	@echo "$(COLOR_YELLOW)Restarting orbit-$(SERVICE)...$(COLOR_RESET)"
	@kubectl rollout restart deployment/orbit-$(SERVICE) -n orbit-system
	@kubectl rollout status deployment/orbit-$(SERVICE) -n orbit-system --timeout=120s
	@echo "$(COLOR_GREEN)✓ orbit-$(SERVICE) updated$(COLOR_RESET)"

# ===========================================
# Development Workflow
# ===========================================

.PHONY: dev
dev: ## Run identity service in development mode with auto-reload (air)
	@echo "$(COLOR_YELLOW)Starting identity service in development mode...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
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
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/identity/main.go

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
build-backend: ## Build all service binaries to .bin directory
	@echo "$(COLOR_YELLOW)Building service binaries...$(COLOR_RESET)"
	@mkdir -p $(BINARY_DIR)
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-identity ./cmd/identity
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/orbit-example ./cmd/example
	@echo "$(COLOR_GREEN)✓ Binaries built: $(BINARY_DIR)/orbit-identity, $(BINARY_DIR)/orbit-example$(COLOR_RESET)"

# ===========================================
# Testing & Quality
# ===========================================

.PHONY: test-integration
test-integration: ## Run integration tests (requires dev-cluster to be running)
	@echo "$(COLOR_YELLOW)Running integration tests...$(COLOR_RESET)"
	@cd $(BACKEND_DIR) && $(GOTEST) -v -race -timeout=5m -tags=integration ./...
	@echo "$(COLOR_GREEN)✓ Integration tests passed$(COLOR_RESET)"

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
frontend-install: ## Install frontend dependencies (enforces nvm + Node LTS + pnpm, idempotent)
	@echo "$(COLOR_YELLOW)Setting up frontend environment...$(COLOR_RESET)"
	@# ── 1. Enforce nvm ────────────────────────────────────────────────────────
	@export NVM_DIR="$${NVM_DIR:-$$HOME/.nvm}"; \
	if [ ! -f "$$NVM_DIR/nvm.sh" ]; then \
		echo "$(COLOR_RED)✗ nvm not found at $$NVM_DIR$(COLOR_RESET)"; \
		echo "  Install nvm: https://github.com/nvm-sh/nvm"; \
		exit 1; \
	fi; \
	. "$$NVM_DIR/nvm.sh"; \
	echo "  Ensuring Node LTS via nvm (skipped if already installed)..."; \
	nvm install $(NODE_VERSION); \
	nvm use $(NODE_VERSION); \
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
	@cd $(FRONTEND_DIR) && pnpm dev

.PHONY: frontend-build
frontend-build: ## Build the frontend for production
	@echo "$(COLOR_YELLOW)Building frontend...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm build
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

# TODO: add frontend-test as a dependency once frontend tests are implemented
.PHONY: frontend-check
frontend-check: frontend-lint frontend-type-check frontend-fmt ## Run all frontend checks (eslint + tsc + prettier)

.PHONY: frontend-clean
frontend-clean: ## Remove frontend build artifacts and installed dependencies
	@echo "$(COLOR_YELLOW)Cleaning frontend...$(COLOR_RESET)"
	@rm -rf $(FRONTEND_DIR)/.next $(FRONTEND_DIR)/node_modules
	@echo "$(COLOR_GREEN)✓ Frontend cleaned$(COLOR_RESET)"

.PHONY: frontend-storybook
frontend-storybook: ## Start Terra design system Storybook (port 6006)
	@echo "$(COLOR_YELLOW)Starting Terra Storybook...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra storybook

.PHONY: frontend-storybook-build
frontend-storybook-build: ## Build Terra Storybook as a static site
	@echo "$(COLOR_YELLOW)Building Terra Storybook...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra build-storybook
	@echo "$(COLOR_GREEN)✓ Terra Storybook built$(COLOR_RESET)"

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
		--api-base "$${ORBIT_API_BASE:-http://localhost:6069/api/v1}" \
		--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"

.PHONY: auth-device-k3d
auth-device-k3d: ## Authenticate via Device Authorization Grant (k3d cluster via port-forward)
	@chmod +x scripts/auth-device.sh
	@echo "$(COLOR_CYAN)→ Starting port-forward orbit-identity → localhost:6069$(COLOR_RESET)"
	@kubectl port-forward svc/orbit-identity -n orbit-system 6069:80 >/tmp/orbit-pf.log 2>&1 & \
		PF_PID=$$!; \
		sleep 2; \
		./scripts/auth-device.sh \
			--api-base "$${ORBIT_API_BASE:-http://localhost:6069/api/v1}" \
			--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),example)}"; \
		STATUS=$$?; \
		kill $$PF_PID 2>/dev/null || true; \
		exit $$STATUS

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
	echo "  Test user: dev@orbit.local / $(password)"

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
bootstrap: env dex-env setup dev-up migrate-up ## Bootstrap project (scaffold .env, setup tools, start services, run migrations)
	@echo "$(COLOR_GREEN)✓ Project bootstrapped and ready$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)Run 'make dev' to start Orbit in development mode$(COLOR_RESET)"



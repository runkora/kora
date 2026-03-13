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
BINARY_NAME=orbit
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
setup: install-tools install-tools-cluster tidy frontend-install hooks ## Complete project setup (install tools, tidy dependencies, install hooks)
	@echo "$(COLOR_GREEN)✓ Setup complete$(COLOR_RESET)"

.PHONY: hooks
hooks: ## Configure git to use the project's tracked hooks from .githooks/
	@git config core.hooksPath .githooks
	@echo "$(COLOR_GREEN)✓ Git hooks installed (.githooks/)$(COLOR_RESET)"

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

.PHONY: migrate-up
migrate-up: ## Run all database migrations up
	@echo "$(COLOR_YELLOW)Running migrations up...$(COLOR_RESET)"
	@GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" \
		goose -dir $(MIGRATIONS_DIR) up
	@echo "$(COLOR_GREEN)✓ Migrations up complete$(COLOR_RESET)"

.PHONY: migrate-down
migrate-down: ## Roll back the last database migration
	@echo "$(COLOR_YELLOW)Rolling back last migration...$(COLOR_RESET)"
	@GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" \
		goose -dir $(MIGRATIONS_DIR) down
	@echo "$(COLOR_GREEN)✓ Migration rolled back$(COLOR_RESET)"

.PHONY: migrate-status
migrate-status: ## Show migration status
	@echo "$(COLOR_YELLOW)Migration status:$(COLOR_RESET)"
	@GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" \
		goose -dir $(MIGRATIONS_DIR) status

.PHONY: migrate-create
migrate-create: ## Create a new migration (usage: make migrate-create name=migration_name)
	@if [ -z "$(name)" ]; then \
		echo "$(COLOR_YELLOW)Usage: make migrate-create name=<migration_name>$(COLOR_RESET)"; \
		exit 1; \
	fi
	@echo "$(COLOR_YELLOW)Creating migration: $(name)$(COLOR_RESET)"
	@goose -dir $(MIGRATIONS_DIR) create $(name) sql
	@echo "$(COLOR_GREEN)✓ Migration created$(COLOR_RESET)"

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
	@echo "  Dex OIDC issuer:  http://localhost:5556/dex"
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
install-tools-cluster: ## Install cluster tooling (k3d, kubectl, helm) — istioctl not required, Istio runs via Helm
	@echo "$(COLOR_YELLOW)Installing cluster tools...$(COLOR_RESET)"
	@bash scripts/cluster/install-tools-cluster.sh
	@echo "$(COLOR_GREEN)✓ Cluster tools installed$(COLOR_RESET)"

.PHONY: dev-cluster
dev-cluster: dev-up ## Bring up the local k3d cluster (SPIRE + Istio + NetworkPolicy). Starts postgres first (required by SPIRE).
	@echo "$(COLOR_YELLOW)Bringing up local k3d cluster...$(COLOR_RESET)"
	@bash scripts/cluster/dev-cluster-up.sh

.PHONY: dev-cluster-down
dev-cluster-down: ## Tear down the local k3d cluster
	@echo "$(COLOR_YELLOW)Tearing down local k3d cluster...$(COLOR_RESET)"
	@bash scripts/cluster/dev-cluster-down.sh

.PHONY: docker-build
docker-build: ## Build Orbit backend Docker image (for local cluster development)
	@echo "$(COLOR_YELLOW)Building Orbit Docker image...$(COLOR_RESET)"
	@cd backend && docker build -t orbit-backend:latest -f Dockerfile .
	@echo "$(COLOR_GREEN)✓ Docker image built: orbit-backend:latest$(COLOR_RESET)"

.PHONY: docker-push
docker-push: ## Push Orbit backend Docker image to local registry (for local cluster development)
	@echo "$(COLOR_YELLOW)Pushing Orbit Docker image to local registry...$(COLOR_RESET)"
	@docker tag orbit-backend:latest localhost:5000/orbit-backend:latest
	@docker push localhost:5000/orbit-backend:latest
	@echo "$(COLOR_GREEN)✓ Docker image pushed to local registry: localhost:5000/orbit-backend:latest$(COLOR_RESET)"

.PHONY: cluster-deploy
cluster-deploy: # docker-build docker-push ## Deploy Orbit backend to local k3d cluster
	@echo "$(COLOR_YELLOW)Deploying Orbit backend to local cluster...$(COLOR_RESET)"
	kubectl create secret generic orbit-secrets -n orbit-system \
		--from-literal=DB_PASSWORD="${DB_PASSWORD}" \
		--from-literal=REDIS_PASSWORD="${REDIS_PASSWORD}" \
		--dry-run=client -o yaml | kubectl apply -f -

	@kustomize build k8s | kubectl apply -f -
	@echo "$(COLOR_GREEN)✓ Orbit backend deployed to cluster$(COLOR_RESET)"

# ===========================================
# Development Workflow
# ===========================================

.PHONY: dev
dev: ## Run Orbit in development mode with auto-reload support
	@echo "$(COLOR_YELLOW)Starting Orbit in development mode...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
	if command -v air > /dev/null; then \
		air; \
	else \
		echo "$(COLOR_BLUE)Air not installed. Running without live reload...$(COLOR_RESET)"; \
		echo "$(COLOR_BLUE)Install air for live reload: make install-tools$(COLOR_RESET)"; \
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/orbit/main.go; \
	fi

.PHONY: run
run: ## Run Orbit control plane
	@echo "$(COLOR_YELLOW)Starting Orbit...$(COLOR_RESET)"
	@set -a; [ -f .env ] && . .env; set +a; \
		cd $(BACKEND_DIR) && $(GOCMD) run ./cmd/orbit/main.go

.PHONY: build-backend
build-backend: ## Build Orbit binary to .bin directory
	@echo "$(COLOR_YELLOW)Building Orbit...$(COLOR_RESET)"
	@mkdir -p $(BINARY_DIR)
	@cd $(BACKEND_DIR) && $(GOBUILD) -o ../$(BINARY_DIR)/$(BINARY_NAME) ./cmd/orbit
	@echo "$(COLOR_GREEN)✓ Binary built: $(BINARY_DIR)/$(BINARY_NAME)$(COLOR_RESET)"

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

# ===========================================
# Frontend
# ===========================================

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies (pnpm install)
	@echo "$(COLOR_YELLOW)Installing frontend dependencies...$(COLOR_RESET)"
	@corepack install -g pnpm@$(PNPM_VERSION)
	@cd $(FRONTEND_DIR) && pnpm install $(if $(CI),--frozen-lockfile,)
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

.PHONY: frontend-fmt
frontend-fmt: ## Format frontend code (prettier --write locally, --check in CI)
	@echo "$(COLOR_YELLOW)Formatting frontend...$(COLOR_RESET)"
	@cd $(FRONTEND_DIR) && pnpm exec prettier $(if $(CI),--check,--write) .
	@echo "$(COLOR_GREEN)✓ Frontend formatting OK$(COLOR_RESET)"

.PHONY: frontend-check
frontend-check: frontend-lint frontend-fmt ## Run all frontend checks (eslint + prettier)

.PHONY: frontend-clean
frontend-clean: ## Remove frontend build artifacts and installed dependencies
	@echo "$(COLOR_YELLOW)Cleaning frontend...$(COLOR_RESET)"
	@rm -rf $(FRONTEND_DIR)/.next $(FRONTEND_DIR)/node_modules
	@echo "$(COLOR_GREEN)✓ Frontend cleaned$(COLOR_RESET)"

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
auth-device: ## Authenticate via Device Authorization Grant (Dex local OIDC or any real provider)
	@chmod +x scripts/auth-device.sh
	@./scripts/auth-device.sh \
		--api-base "$${ORBIT_API_BASE:-http://localhost:6069/api/v1}" \
		--tenant-id "$${ORBIT_TENANT_ID:-$(or $(tenant),local)}"

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



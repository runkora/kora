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
CMD_DIR=./backend/cmd/orbit
MIGRATIONS_DIR=./backend/internal/platform/storage/migrations

# Binary output
BINARY_NAME=orbit
BINARY_DIR=.bin

# Database connection string
DB_URL=postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable

# Colors for output
COLOR_RESET=\033[0m
COLOR_BOLD=\033[1m
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
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2}'
	@echo ""

# ===========================================
# Setup & Installation
# ===========================================

.PHONY: setup
setup: install-tools tidy ## Complete project setup (install tools, tidy dependencies)
	@echo "$(COLOR_GREEN)✓ Setup complete$(COLOR_RESET)"

.PHONY: install-tools
install-tools: ## Install development tools (goose, golangci-lint, air, gitleaks)
	@echo "$(COLOR_YELLOW)Installing development tools...$(COLOR_RESET)"
	@$(GOCMD) install github.com/pressly/goose/v3/cmd/goose@latest
	@$(GOCMD) install github.com/air-verse/air@latest
	@if ! command -v golangci-lint > /dev/null; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
	else \
		echo "golangci-lint already installed"; \
	fi
	@if ! command -v gitleaks > /dev/null; then \
		echo "Installing gitleaks..."; \
		GL_TAG=$$(curl -sSf https://api.github.com/repos/gitleaks/gitleaks/releases/latest | grep '"tag_name"' | cut -d'"' -f4); \
		GL_VER=$${GL_TAG#v}; \
		GL_OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
		GL_ARCH=$$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/'); \
		GL_URL="https://github.com/gitleaks/gitleaks/releases/download/$${GL_TAG}/gitleaks_$${GL_VER}_$${GL_OS}_$${GL_ARCH}.tar.gz"; \
		echo "Downloading $${GL_URL}"; \
		curl -sSfL "$${GL_URL}" | tar -xz -C $$(go env GOPATH)/bin gitleaks; \
	else \
		echo "gitleaks already installed"; \
	fi
	@echo "$(COLOR_GREEN)✓ Tools installed$(COLOR_RESET)"

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
	docker-compose up -d
	@echo "$(COLOR_GREEN)✓ Database started$(COLOR_RESET)"

.PHONY: db-down
db-down: ## Stop the PostgreSQL database
	@echo "$(COLOR_YELLOW)Stopping database...$(COLOR_RESET)"
	docker-compose down
	@echo "$(COLOR_GREEN)✓ Database stopped$(COLOR_RESET)"

.PHONY: db-reset
db-reset: db-down db-up ## Reset database (stop, start fresh)
	@echo "$(COLOR_YELLOW)Waiting for database to be ready...$(COLOR_RESET)"
	@sleep 3
	@echo "$(COLOR_GREEN)✓ Database reset complete$(COLOR_RESET)"

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
# Development Workflow
# ===========================================

.PHONY: dev
dev: ## Run Orbit in development mode with auto-reload support
	@echo "$(COLOR_YELLOW)Starting Orbit in development mode...$(COLOR_RESET)"
	@set -a; [ -f backend/.env ] && . backend/.env; set +a; \
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
	@set -a; [ -f backend/.env ] && . backend/.env; set +a; \
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

.PHONY: test
test: ## Run tests with coverage
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
		cd $(BACKEND_DIR) && $(GOLINT) run ./...; \
		echo "$(COLOR_GREEN)✓ Linting passed$(COLOR_RESET)"; \
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
clean-all: clean db-down ## Clean everything including database
	@echo "$(COLOR_YELLOW)Cleaning all artifacts and stopping services...$(COLOR_RESET)"
	@docker-compose down -v
	@echo "$(COLOR_GREEN)✓ All clean$(COLOR_RESET)"

# ===========================================
# Convenience Targets
# ===========================================

.PHONY: check
check: tidy fmt vet lint secrets test ## Run all checks (tidy, fmt, vet, lint, secrets, test)
	@echo "$(COLOR_GREEN)✓ All checks passed$(COLOR_RESET)"

.PHONY: all
all: tidy check build ## Run all quality checks and build

.PHONY: bootstrap
bootstrap: setup db-up migrate-up ## Bootstrap project (setup tools, start db, run migrations)
	@echo "$(COLOR_GREEN)✓ Project bootstrapped and ready$(COLOR_RESET)"
	@echo "$(COLOR_BLUE)Run 'make dev' to start Orbit in development mode$(COLOR_RESET)"

# ===========================================
# Utility Targets (hidden from help)
# ===========================================

.PHONY: up down
up: ## @ignore Ignore standalone 'up' target
	@true

down: ## @ignore Ignore standalone 'down' target
	@true

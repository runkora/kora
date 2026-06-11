#!/bin/bash
# SessionStart hook for Claude Code on the web.
#
# Prepares a fresh remote container so an agent can build, lint and test Orbit
# without any manual bootstrap. The container's filesystem is cached after this
# hook completes, so subsequent sessions reuse the warmed Go/pnpm caches and the
# installed tooling.
#
# What this does NOT do: it does not start Postgres/Redis/Dex. The web
# environment has no Docker daemon, so the service-backed integration suite
# (`make test-integration`, files behind the `integration` build tag) and
# `make dev` cannot run here. The backend unit tests use mocks and need no DB,
# so `make test`, `make lint`, `make build-backend` and the full frontend gate
# all work.
set -euo pipefail

# Web-only: skip entirely on a developer's local machine, which already has its
# own `make setup` toolchain.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

ROOT="${CLAUDE_PROJECT_DIR:-$(pwd)}"
GOBIN="$(go env GOPATH)/bin"
mkdir -p "$GOBIN"
export PATH="$GOBIN:$PATH"

# Pinned versions — keep in sync with the Makefile.
GOLANGCI_LINT_VERSION="v2.11.3"
GITLEAKS_VERSION="v8.27.2"

echo "==> [orbit hook] Installing backend dev tools"

# golangci-lint (carries Orbit's custom ruleguard rules under rules/).
LINT_INSTALLED="$(golangci-lint --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
if [ "${LINT_INSTALLED#v}" != "${GOLANGCI_LINT_VERSION#v}" ]; then
  echo "    golangci-lint ${GOLANGCI_LINT_VERSION} (installed: ${LINT_INSTALLED:-none})"
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sh -s -- -b "$GOBIN" "$GOLANGCI_LINT_VERSION"
else
  echo "    golangci-lint ${GOLANGCI_LINT_VERSION} already installed"
fi

# gitleaks (the `make check` secret scan).
GL_INSTALLED="$(gitleaks version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
if [ "${GL_INSTALLED#v}" != "${GITLEAKS_VERSION#v}" ]; then
  echo "    gitleaks ${GITLEAKS_VERSION} (installed: ${GL_INSTALLED:-none})"
  GL_VER="${GITLEAKS_VERSION#v}"
  GL_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  GL_ARCH="$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')"
  curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/${GITLEAKS_VERSION}/gitleaks_${GL_VER}_${GL_OS}_${GL_ARCH}.tar.gz" \
    | tar -xz -C "$GOBIN" gitleaks
else
  echo "    gitleaks ${GITLEAKS_VERSION} already installed"
fi

# Warm the Go toolchain (go.mod pins 1.26 via GOTOOLCHAIN=auto), module cache
# and build cache. This is the slow first-run step; it is cached thereafter.
echo "==> [orbit hook] Warming Go module + build cache (backend)"
( cd "$ROOT/backend" && go build ./... )

# Frontend dependencies. --frozen-lockfile keeps installs reproducible against
# the committed pnpm-lock.yaml while still populating the pnpm content store.
echo "==> [orbit hook] Installing frontend dependencies (pnpm)"
( cd "$ROOT/frontend" && pnpm install --frozen-lockfile )

# Make the installed Go tools (golangci-lint, gitleaks) discoverable to every
# command the agent runs this session.
if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  echo "export PATH=\"$GOBIN:\$PATH\"" >> "$CLAUDE_ENV_FILE"
fi

echo "==> [orbit hook] Ready: make build-backend / make test / make lint / make frontend-check"

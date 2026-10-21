# Pinned versions of kora's development toolchain. The Makefile includes this
# file and `docker buildx bake` reads it as a second definition file, so each
# value is quoted for HCL.
#
# Other files pin some of the same tools and must change with this file:
# .github/workflows/backend-ci.yml (golangci-lint), .github/workflows/secret-scan.yml
# (gitleaks), .github/workflows/frontend-ci.yml (Node and pnpm) and the
# packageManager field in frontend/package.json (pnpm) and scripts/cluster/versions.env
# (helm).
#
# GO_VERSION must equal the go directive in backend/go.mod (`make check-versions`).

GO_VERSION = "1.26.10"
GITLEAKS_VERSION = "v8.27.2"
GOLANGCI_LINT_VERSION = "v2.11.3"
NODE_VERSION = "24"
PNPM_VERSION = "10.32.1"
PROTOC_GEN_GO_VERSION = "v1.36.11"
PROTOC_GEN_GO_GRPC_VERSION = "v1.5.1"
BUF_VERSION = "v1.55.1"
DEADCODE_VERSION = "v0.48.0"
HELM_VERSION = "v4.3.0"

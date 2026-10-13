# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder

# SERVICE must be the name of the directory under cmd/ (e.g. identity, example).
ARG SERVICE
RUN test -n "$SERVICE" || (echo "ERROR: --build-arg SERVICE=<service> is required" && exit 1)

WORKDIR /app

ENV GOPRIVATE=github.com/clear-route/*
ENV GONOSUMDB=github.com/clear-route/*
ENV GONOPROXY=github.com/clear-route/*

COPY go.mod go.sum ./
# TODO: use .netrc + docker secrets for private modules in CI
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY . .

# The builder runs on the native build platform and Go cross-compiles for the
# target, so multi-arch builds never compile under QEMU emulation.
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /orbit ./cmd/${SERVICE}

# Stage only the YAML config files this binary actually loads at runtime,
# preserving relative paths under /app. Filtering to *.yaml keeps Go source
# (config.go, etc.) out of the runtime image. Directories that do not exist
# for a given SERVICE are tolerated silently, so config-less services
# (orbit-shell, orbit-runtime-mcp) still build.
#
#   1. cmd/${SERVICE}/config          — service-owned settings.
#   2. internal/platform/observability/config — imported by all runtime binaries.
#      internal/platform/brand/config         — the product name; imported by
#                                               every binary that speaks to
#                                               people or agents.
#   3. internal/platform/storage/config       — imported by binaries that touch
#                                               Postgres/Redis; harmless for
#                                               those that do not.
#   4. internal/identity/config       — only consumed by the identity binary
#                                       today, small enough to copy universally.
#   5. internal/platform/token/config — imported by binaries that verify
#                                       platform tokens.
RUN mkdir -p /configs && \
    find /app/cmd/${SERVICE}/config \
         /app/internal/platform/observability/config \
         /app/internal/platform/brand/config \
         /app/internal/platform/storage/config \
         /app/internal/identity/config \
         /app/internal/platform/token/config \
         -maxdepth 2 -name '*.yaml' 2>/dev/null | while read -r f; do \
        rel="${f#/app/}"; \
        mkdir -p "/configs/$(dirname "$rel")"; \
        cp "$f" "/configs/$rel"; \
    done

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0

# Non-root user matching the chart securityContext (runAsUser: 65532,
# runAsNonRoot: true, per charts/orbit/templates/*/deployment.yaml). Baking the
# drop into the image keeps the guarantee wherever the chart is not the
# deployment surface (direct docker run, forensics, ad-hoc orchestrators) and
# mirrors Dockerfile.operator's user model.
RUN addgroup -g 65532 nonroot && adduser -u 65532 -G nonroot -s /sbin/nologin -D nonroot

WORKDIR /app

RUN apk add --no-cache ca-certificates

# Config files: same relative structure as local dev so the loader resolves
# paths identically in both environments. The staging dir from the builder
# stage contains only the YAML config files this binary actually loads.
COPY --from=builder --chown=65532:65532 /configs/ ./

COPY --from=builder --chown=65532:65532 /orbit ./orbit

USER 65532:65532

EXPOSE 8080
CMD ["./orbit"]

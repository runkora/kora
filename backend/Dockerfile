# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder

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

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /orbit ./cmd/${SERVICE}/main.go

# Stage only the YAML config files this binary actually loads at runtime,
# preserving relative paths under /app. Filtering to *.yaml keeps Go source
# (config.go, etc.) out of the runtime image. Directories that do not exist
# for a given SERVICE are tolerated silently, so config-less services
# (orbit-shell, orbit-runtime-mcp) still build.
#
#   1. cmd/${SERVICE}/config          — service-owned settings.
#   2. internal/platform/observability/config — imported by all runtime binaries.
#   3. internal/platform/storage/config       — imported by binaries that touch
#                                               Postgres/Redis; harmless for
#                                               those that do not.
#   4. internal/identity/config       — only consumed by the identity binary
#                                       today, small enough to copy universally.
RUN mkdir -p /configs && \
    find /app/cmd/${SERVICE}/config \
         /app/internal/platform/observability/config \
         /app/internal/platform/storage/config \
         /app/internal/identity/config \
         -maxdepth 2 -name '*.yaml' 2>/dev/null | while read -r f; do \
        rel="${f#/app/}"; \
        mkdir -p "/configs/$(dirname "$rel")"; \
        cp "$f" "/configs/$rel"; \
    done

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

WORKDIR /app

RUN apk add --no-cache ca-certificates

# Config files: same relative structure as local dev so the loader resolves
# paths identically in both environments. The staging dir from the builder
# stage contains all */config/*.yaml files collected above.
COPY --from=builder /configs/ ./

COPY --from=builder /orbit ./orbit

EXPOSE 8080
CMD ["./orbit"]

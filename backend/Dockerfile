# syntax=docker/dockerfile:1

# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

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

# Collect every */config/*.yaml file into /configs/ maintaining the same relative
# path structure. This means the runtime stage can do a single COPY regardless of
# which internal packages have config dirs, and the loader sees the exact same
# layout it sees on disk during local development.
RUN find /app -path "*/config/*.yaml" | while read f; do \
        rel="${f#/app/}"; \
        mkdir -p "/configs/$(dirname "$rel")"; \
        cp "$f" "/configs/$rel"; \
    done

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

WORKDIR /app

RUN apk add --no-cache ca-certificates

# Config files: same relative structure as local dev so the loader resolves
# paths identically in both environments. The staging dir from the builder
# stage contains all */config/*.yaml files collected above.
COPY --from=builder /configs/ ./

COPY --from=builder /orbit ./orbit

EXPOSE 8080
CMD ["./orbit"]

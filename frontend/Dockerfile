# =============================================================================
# Orbit Frontend — multi-stage Docker build
# =============================================================================
#
# Build context: frontend/  (self-contained pnpm workspace)
#
# Usage (from repo root):
#   docker build -t orbit-frontend:latest \
#     --build-arg NEXT_PUBLIC_APP_URL=https://orbit.local.clearroute.io:8443 \
#     frontend/
#
# NEXT_PUBLIC_APP_URL must be provided as a build arg because Next.js inlines
# NEXT_PUBLIC_* variables into the client-side JavaScript bundle at build time.
# It is also injected at runtime via the orbit-frontend-config ConfigMap for
# server-side code that reads process.env.NEXT_PUBLIC_APP_URL directly.
# =============================================================================

# ── Stage 1: deps ─────────────────────────────────────────────────────────────
# Install all workspace dependencies with a frozen lockfile.
# Separated from the build stage so Docker layer caching skips re-installs
# when only source files change (not package.json or pnpm-lock.yaml).
FROM node:24-alpine AS deps

WORKDIR /app

# Enable corepack and pin pnpm to the version locked in package.json.
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./

# Copy workspace package manifests before installing so pnpm can resolve the
# @orbit/terra workspace dependency without copying all source files.
COPY src/packages/terra/package.json ./src/packages/terra/

RUN --mount=type=cache,target=/root/.local/share/pnpm/store/v3 \
    corepack prepare pnpm@10.32.1 --activate && \
    pnpm install --frozen-lockfile

# ── Stage 2: builder ──────────────────────────────────────────────────────────
# Copy all source files and produce a standalone Next.js build.
FROM deps AS builder

WORKDIR /app

COPY . .

# NEXT_PUBLIC_APP_URL is inlined into the client bundle at build time.
ARG NEXT_PUBLIC_APP_URL=https://orbit.local.clearroute.io:8443
ENV NEXT_PUBLIC_APP_URL=${NEXT_PUBLIC_APP_URL}

# ORBIT_API_URL and BFF_SESSION_SECRET are server-side only, but Next.js
# evaluates server modules during `next build` to collect page data, so they
# must be present at build time to pass validation. These placeholder values
# are never used at runtime — the real values are injected via the
# orbit-frontend-config ConfigMap and orbit-frontend-secrets Secret.
ARG ORBIT_API_URL=http://orbit-identity.orbit-system/api/v1
ARG INFERENCE_GATEWAY_URL=http://orbit-inference-gateway.orbit-system/v1
ARG POLICY_GATEWAY_URL=http://orbit-policy-gateway.orbit-system/v1
ARG POLICY_SERVICE_URL=http://orbit-policy-service.orbit-system/api/v1
ARG ORBIT_AGENTS_URL=http://orbit-agents.orbit-system
ARG ORBIT_TRIGGERS_URL=http://orbit-triggers.orbit-system/api/v1
ARG TOOL_GATEWAY_URL=http://orbit-tool-gateway.orbit-system
# hadolint ignore=DL3025
# BFF_SESSION_SECRET is a placeholder value only — Next.js evaluates server
# modules at build time to collect page data, so the variable must be present
# to pass validation. The real secret is injected at runtime via the
# orbit-frontend-secrets Kubernetes Secret and never baked into the image.
ARG BFF_SESSION_SECRET=build-time-placeholder-not-used-at-runtime
ENV ORBIT_API_URL=${ORBIT_API_URL} \
    INFERENCE_GATEWAY_URL=${INFERENCE_GATEWAY_URL} \
    POLICY_GATEWAY_URL=${POLICY_GATEWAY_URL} \
    POLICY_SERVICE_URL=${POLICY_SERVICE_URL} \
    ORBIT_AGENTS_URL=${ORBIT_AGENTS_URL} \
    ORBIT_TRIGGERS_URL=${ORBIT_TRIGGERS_URL} \
    TOOL_GATEWAY_URL=${TOOL_GATEWAY_URL} \
    BFF_SESSION_SECRET=${BFF_SESSION_SECRET}

# Standalone output traces all runtime dependencies and produces a self-contained
# server.js entry point — no separate node_modules install needed in the runner.
ENV NEXT_OUTPUT=standalone

RUN pnpm build

# ── Stage 3: runner ───────────────────────────────────────────────────────────
# Minimal production image — only the standalone output, static assets, and public dir.
FROM node:24-alpine AS runner

WORKDIR /app

# Non-root user matching uid/gid 65532 used by all other Orbit service containers.
RUN addgroup --system --gid 65532 orbit && \
    adduser  --system --uid 65532 --ingroup orbit orbit

# Standalone output — includes server.js entry point and traced node_modules.
COPY --from=builder --chown=orbit:orbit /app/.next/standalone ./

# Static assets must be copied separately — standalone output does not include them.
COPY --from=builder --chown=orbit:orbit /app/.next/static ./.next/static

# Public directory (favicon, robots.txt, etc.).
COPY --from=builder --chown=orbit:orbit /app/public ./public

USER orbit

EXPOSE 3000

ENV PORT=3000 \
    HOSTNAME=0.0.0.0 \
    NODE_ENV=production

CMD ["node", "server.js"]

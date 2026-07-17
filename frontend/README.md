# Orbit Frontend

The Orbit dashboard: a Next.js (App Router) app with a BFF (Backend-For-Frontend)
server layer that proxies the Go control plane. All product JSX is built on the
**Terra** design system; raw HTML primitives are banned by ESLint.

## Prerequisites

- **Node 24** (`>=24 <25`), pinned in `.nvmrc`. Use `nvm install 24 && nvm use`.
- **pnpm 10** (`>=10 <11`), pinned to `pnpm@10.32.1` via `packageManager`. Enable
  it with Corepack: `corepack enable`. This repo is pnpm-only: a `preinstall`
  hook (`only-allow pnpm`) blocks `npm` / `yarn`.

`make frontend-install` (run from the repo root) does all of the above: enforces
nvm + Node 24, enables Corepack, installs pnpm 10, and runs `pnpm install`.

## Setup

```bash
# from the repo root — installs Node/pnpm and dependencies
make frontend-install

# or, if your Node + pnpm are already correct, from frontend/
pnpm install
```

### Environment

The BFF reads its backend URLs and session secret from the environment. In the
cluster these are injected by the `orbit-frontend-config` ConfigMap and the
`orbit-frontend-secrets` Secret (`BFF_SESSION_SECRET`, written by
`create-secrets.sh` from the root `.env`). You do not set any of them by hand.

There is no host frontend server, so there is no `frontend/.env.local` to fill
in. The Playwright E2E runner supplies its own env inline (see
`playwright.config.ts`).

## Developing the frontend

The frontend runs only in the cluster. A change ships as a Docker image build and
a rollout, the same loop as every backend service:

```bash
make redeploy SERVICE=frontend
```

The app is served at `https://orbit.local.clearroute.io:8443`. The image bakes in
`NEXT_PUBLIC_APP_URL=https://orbit.local.clearroute.io:8443` at build time
(overridable via the `NEXT_PUBLIC_APP_URL` make variable, see the root
`Makefile`), so every change means a full rebuild and rollout: there is no hot
module reload. Bring the environment up first with `make up`; see
[`docs/guides/local-development.md`](../docs/guides/local-development.md).

Lint, type-check, tests, and Storybook run on the host, no cluster required:

```bash
make frontend-lint          # eslint (incl. Terra rules)
make frontend-type-check    # tsc --noEmit
make frontend-test          # unit + integration
pnpm storybook              # Storybook on :6006
```

## Architecture (BFF: server vs. client)

The browser never talks to the Go backend directly. Server-side code in
`src/server/` (and Server Components) holds the backend URLs and session secret
and is marked `import 'server-only'` so it can never be bundled into the
browser. Client-initiated requests go to Next.js Route Handlers under
`src/app/api/*` (the BFF), which attach the session and proxy to the appropriate
backend service. The full data-flow pattern (server service module → BFF route →
client service → hook → context → RSC page) is documented in the
[frontend state golden path](docs/golden-paths/frontend-state-pattern.md).

## Scripts

Run from `frontend/` with `pnpm <script>`, or via the `make frontend-*` wrappers
from the repo root.

| Script             | Command                                            | Make wrapper                     | Purpose                                  |
| ------------------ | -------------------------------------------------- | -------------------------------- | ---------------------------------------- |
| `dev`              | `next dev`                                         | None                             | Booted by the Playwright E2E runner only |
| `build`            | `next build`                                       | `make frontend-build`            | Production build                         |
| `start`            | `next start`                                       | None                             | Serve a production build                 |
| `lint`             | `eslint .`                                         | `make frontend-lint`             | ESLint (incl. Terra rules)               |
| `type-check`       | `tsc --noEmit`                                     | `make frontend-type-check`       | TypeScript check                         |
| `storybook`        | `storybook dev -p 6006`                            | None                             | Storybook on :6006                       |
| `build-storybook`  | `storybook build`                                  | None                             | Static Storybook build                   |
| `test:unit`        | `vitest run --config vitest.config.unit.ts`        | `make frontend-test-unit`        | Unit tests (jsdom)                       |
| `test:integration` | `vitest run --config vitest.config.integration.ts` | `make frontend-test-integration` | Integration tests (MSW + jsdom)          |
| `test:e2e`         | `playwright test`                                  | `make frontend-test-e2e`         | E2E (Playwright + Chromium)              |

Coverage variants: `test:coverage:unit`, `test:coverage:integration`.
`make frontend-check` runs lint + type-check + format + unit/integration tests.

## Before you commit

This gate is mandatory for any frontend change. Both must pass with zero errors:

```bash
pnpm lint && pnpm tsc --noEmit
```

## Terra design system (required)

All product JSX must use Terra (`@orbit/terra`): `<div>`, `<p>`, `<span>`,
`<button>`, etc. are banned by ESLint with no escape hatches. See
[`AGENTS.md` → **Frontend Development**](../AGENTS.md) for the full rule table,
the raw-HTML → Terra replacement map, the three-tier component placement model,
and the `@tier` annotation rules. Do not restate those rules here.

## Reference docs

See [`docs/`](docs/) for the full index. The four core references:

- [Terra design system](docs/reference/terra-design-system.md): component usage, props, examples, verification checklist.
- [Component placement](docs/reference/component-placement.md): the three-tier model, full Terra inventory, `@tier` annotations.
- [App-level shared components](docs/reference/app-components.md): the Tier 2 (`@orbit/components`) inventory.
- [Frontend state golden path](docs/golden-paths/frontend-state-pattern.md): fetching, caching, and reactive state for data-driven pages.

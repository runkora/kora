# Orbit Frontend

The Orbit dashboard — a Next.js (App Router) app with a BFF (Backend-For-Frontend)
server layer that proxies the Go control plane. All product JSX is built on the
**Terra** design system; raw HTML primitives are banned by ESLint.

## Prerequisites

- **Node 24** (`>=24 <25`) — pinned in `.nvmrc`. Use `nvm install 24 && nvm use`.
- **pnpm 10** (`>=10 <11`), pinned to `pnpm@10.32.1` via `packageManager`. Enable
  it with Corepack: `corepack enable`. This repo is pnpm-only — a `preinstall`
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

The BFF server layer (`src/server/config.ts`) **requires** six env vars and the
app throws on boot if any is missing. There are no defaults.

- **Running via `make frontend-dev`** — the root Makefile sources the repo root
  `.env` and re-exports everything into the pnpm sub-shell, so the root `.env`
  already covers the frontend. Nothing else to do.
- **Running `pnpm dev` directly from `frontend/`** — Next.js does not see the
  root `.env`. Copy the example and fill it in:

  ```bash
  cp .env.example .env.local
  ```

  See [`.env.example`](.env.example) for every variable, local dev values, and
  the honest note on `ORBIT_AGENTS_URL` (agents is a cluster-only service that
  collides with identity on the host). Generate `BFF_SESSION_SECRET` (min 32
  chars) with `openssl rand -base64 32`.

Required: `ORBIT_API_URL`, `INFERENCE_GATEWAY_URL`, `POLICY_SERVICE_URL`,
`POLICY_GATEWAY_URL`, `ORBIT_AGENTS_URL`, `BFF_SESSION_SECRET`.

## Running the dev server

```bash
# from the repo root (recommended — propagates env from root .env)
make frontend-dev

# or directly, after creating frontend/.env.local
pnpm dev
```

The app starts on http://localhost:3000. For a working login you also need the
backend running — at minimum the identity service (`make run-identity`, :8080)
and Dex/infra (`make dev-up`); see the root `README` / `AGENTS.md` for the full
backend bootstrap.

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

| Script             | Command                                            | Make wrapper                     | Purpose                         |
| ------------------ | -------------------------------------------------- | -------------------------------- | ------------------------------- |
| `dev`              | `next dev`                                         | `make frontend-dev`              | Start the dev server on :3000   |
| `build`            | `next build`                                       | `make frontend-build`            | Production build                |
| `start`            | `next start`                                       | —                                | Serve a production build        |
| `lint`             | `eslint .`                                         | `make frontend-lint`             | ESLint (incl. Terra rules)      |
| `type-check`       | `tsc --noEmit`                                     | `make frontend-type-check`       | TypeScript check                |
| `storybook`        | `storybook dev -p 6006`                            | —                                | Storybook on :6006              |
| `build-storybook`  | `storybook build`                                  | —                                | Static Storybook build          |
| `test:unit`        | `vitest run --config vitest.config.unit.ts`        | `make frontend-test-unit`        | Unit tests (jsdom)              |
| `test:integration` | `vitest run --config vitest.config.integration.ts` | `make frontend-test-integration` | Integration tests (MSW + jsdom) |
| `test:e2e`         | `playwright test`                                  | `make frontend-test-e2e`         | E2E (Playwright + Chromium)     |

Coverage variants: `test:coverage:unit`, `test:coverage:integration`.
`make frontend-check` runs lint + type-check + format + unit/integration tests.

## Before you commit

This gate is mandatory for any frontend change — both must pass with zero errors:

```bash
pnpm lint && pnpm tsc --noEmit
```

## Terra design system (required)

All product JSX must use Terra (`@orbit/terra`) — `<div>`, `<p>`, `<span>`,
`<button>`, etc. are banned by ESLint with no escape hatches. See
[`AGENTS.md` → **Frontend Development**](../AGENTS.md) for the full rule table,
the raw-HTML → Terra replacement map, the three-tier component placement model,
and the `@tier` annotation rules. Do not restate those rules here.

## Reference docs

See [`docs/`](docs/) for the full index. The four core references:

- [Terra design system](docs/reference/terra-design-system.md) — component usage, props, examples, verification checklist.
- [Component placement](docs/reference/component-placement.md) — the three-tier model, full Terra inventory, `@tier` annotations.
- [App-level shared components](docs/reference/app-components.md) — the Tier 2 (`@orbit/components`) inventory.
- [Frontend state golden path](docs/golden-paths/frontend-state-pattern.md) — fetching, caching, and reactive state for data-driven pages.

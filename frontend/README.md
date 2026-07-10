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

The BFF server layer (`src/server/config.ts`) calls `requireEnv(...)` for every
backend URL and the session secret, so the app throws on boot if any is missing.
There are no defaults.

- **Running via `make frontend-dev`**: the root Makefile sources the repo root
  `.env` and re-exports everything into the pnpm sub-shell, so the root `.env`
  already covers the frontend. Nothing else to do.
- **Running `pnpm dev` directly from `frontend/`**: Next.js does not see the
  root `.env`. Copy the example and fill it in:

  ```bash
  cp .env.example .env.local
  ```

  See [`.env.example`](.env.example) for every variable, local dev values, and
  the honest note on `ORBIT_AGENTS_URL` (agents is a cluster-only service that
  collides with identity on the host). Generate `BFF_SESSION_SECRET` (min 32
  chars) with `openssl rand -base64 32`.

Required (one `requireEnv(...)` call each in `src/server/config.ts`):
`ORBIT_API_URL`, `INFERENCE_GATEWAY_URL`, `POLICY_SERVICE_URL`,
`POLICY_GATEWAY_URL`, `ORBIT_AGENTS_URL`, `TOOL_GATEWAY_URL`,
`BFF_SESSION_SECRET`. (`TOOL_GATEWAY_URL` and `ORBIT_AGENTS_URL` point at
cluster-only services. See the note below and in `.env.example`.)

## Running the dev server

```bash
# from the repo root (recommended — propagates env from root .env)
make frontend-dev

# or directly, after creating frontend/.env.local
pnpm dev
```

The app starts on http://localhost:3000. For a working login you also need the
backend running: at minimum the identity service (`make run-identity`, :8080)
and Dex/infra (`make dev-up`); see the root `README` / `AGENTS.md` for the full
backend bootstrap.

### Two ways to run the frontend: pick the right loop

There are two distinct frontend loops. Use the fast host loop for almost all UI
work; only reach for the cluster loop when a feature depends on a cluster-only
backend service.

| Loop               | Command                          | Speed              | What it is                                                                                                                         | When you need it                                                                                                                 |
| ------------------ | -------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| **Host (fast)**    | `pnpm dev` / `make frontend-dev` | Instant hot-reload | `next dev` on the host at :3000, reading the root `.env` (or `frontend/.env.local`)                                                | The default for all UI work: styling, components, pages backed by host-reachable services (identity, inference gateway, policy). |
| **Cluster (slow)** | `make redeploy-frontend`         | Minutes per change | Builds a Docker image (`docker buildx bake`) and rolls it into the k3d cluster, served at `https://orbit.local.clearroute.io:8443` | When you must exercise the app against cluster-only services, or verify the production image build itself.                       |

The cluster image bakes in `NEXT_PUBLIC_APP_URL=https://orbit.local.clearroute.io:8443`
at build time (overridable via the `NEXT_PUBLIC_APP_URL` make variable, see the
root `Makefile`), so it is not a hot-reload loop: every change means a full
rebuild + rollout.

> **The agents pages need the cluster.** `orbit-agents` runs in-cluster only
> (it binds `:8080` there and has no host run target), and the MCP tool gateway
> (`TOOL_GATEWAY_URL`) is likewise cluster-only. Under the host loop those env
> vars are satisfied by placeholders so the BFF can boot, but the agents and
> tool-catalog pages will error until you point them at the cluster (or a
> port-forward) or run the whole frontend in-cluster via `make redeploy-frontend`.

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
| `start`            | `next start`                                       | None                             | Serve a production build        |
| `lint`             | `eslint .`                                         | `make frontend-lint`             | ESLint (incl. Terra rules)      |
| `type-check`       | `tsc --noEmit`                                     | `make frontend-type-check`       | TypeScript check                |
| `storybook`        | `storybook dev -p 6006`                            | None                             | Storybook on :6006              |
| `build-storybook`  | `storybook build`                                  | None                             | Static Storybook build          |
| `test:unit`        | `vitest run --config vitest.config.unit.ts`        | `make frontend-test-unit`        | Unit tests (jsdom)              |
| `test:integration` | `vitest run --config vitest.config.integration.ts` | `make frontend-test-integration` | Integration tests (MSW + jsdom) |
| `test:e2e`         | `playwright test`                                  | `make frontend-test-e2e`         | E2E (Playwright + Chromium)     |

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

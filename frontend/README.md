# Frontend

The kora dashboard is a Next.js App Router application with a backend-for-frontend (BFF) layer. Product UI is built with the Hades design system.

## Set up

The frontend requires Node 24 and pnpm 10. From the repository root, install the pinned toolchain and dependencies with:

```bash
make frontend-install
```

If the correct Node and pnpm versions are already active, run `pnpm install` from `frontend/` instead.

Kubernetes injects the deployed application's backend URLs and session secret. Playwright supplies the environment for its own test server.

## Develop

The product frontend runs in the local cluster. Bring kora up, then rebuild and roll the frontend after a change:

```bash
make up
make redeploy SERVICE=frontend
```

The dashboard is served at `https://orbit.local.clearroute.io`. There is no hot module reload in the cluster workflow. Each product change requires a new image and rollout.

Linting, type checking, unit tests, integration tests, and Storybook run on the host:

```bash
make frontend-lint
make frontend-type-check
make frontend-test
pnpm storybook
```

To open Storybook over a VPN, set `STORYBOOK_ALLOWED_HOSTS` in the repository root `.env` (or the shell) to a comma-separated list of hostnames. A leading dot matches the domain and every subdomain, for example `STORYBOOK_ALLOWED_HOSTS=.ts.net`. IP addresses are always accepted.

Run pnpm commands from `frontend/`. The equivalent `make frontend-*` targets run from the repository root.

## Architecture

The browser does not call the Go services directly. Server Components and modules under `src/server/` hold backend URLs and session state. Client requests pass through Next.js Route Handlers under `src/app/api/`, which attach the session and proxy the request to the appropriate service.

The [frontend state golden path](docs/golden-paths/frontend-state-pattern.md) documents the complete data flow from server service module to BFF route, client service, hook, context, and page.

## Before you commit

Frontend changes must pass linting and type checking with no errors:

```bash
pnpm lint
pnpm tsc --noEmit
```

Run the relevant unit, integration, or end-to-end tests for the behaviour you changed. The full frontend gate is available from the repository root:

```bash
make frontend-check
```

## Documentation

- [Frontend documentation](docs/README.md): the index for frontend reference material and golden paths.
- [Hades design system](docs/reference/hades-design-system.md): component APIs, usage, and verification.
- [Component placement](docs/reference/component-placement.md): the three-tier component model and `@tier` rules.
- [App-level components](docs/reference/app-components.md): shared kora-aware components exported by `@orbit/components`.
- [Frontend testing](../docs/guides/frontend-testing.md): the test suites and where each kind of test belongs.
- [Agent development guide](../AGENTS.md): the machine-enforced frontend conventions, including the Hades rules.

# Frontend Test Strategy — Product Requirements Document

**Status:** Approved  
**Created:** 2026-03-23
**Last Updated:** 2026-04-08
**Owner:** Frontend
**Stakeholders:** Engineering, Product

---

## Executive Summary

The Orbit frontend has zero test coverage and no test infrastructure. With the Terra design system (`@orbit/terra`) established as a stable, independent workspace package, and the application growing beyond a placeholder page — with a working OIDC auth flow, a BFF session layer, and a dashboard shell — shipping without a test strategy means every change carries unquantified regression risk.

This PRD defines a layered, package-aware test strategy that gives each layer of the frontend stack — workspace packages, app components, BFF routes, and end-to-end flows — the right type of test, the right tooling, and a clear execution model that scales as the monorepo grows. A central principle is that **integration tests prioritise manipulating real application state over mocking** — tests intercept at the HTTP boundary via MSW, not at the module level, so the actual component logic and data-fetching pipeline are exercised. Where mocking is unavoidable, it is centralised in a single, typed `test-utils/` directory shared across all test tiers.

---

## Problem Statement

### Current State

The frontend codebase contains:

- **`frontend/src/packages/terra/`** (`@orbit/terra`) — a fully implemented design system with 21+ component families, a token system, and Storybook stories for every component. The package already has `vite@6.3.5`, `@vitejs/plugin-react`, `@storybook/addon-a11y`, and `@storybook/addon-interactions` installed. There are no unit or component tests.
- **`frontend/src/app/`** — a Next.js 16 App Router application with a dashboard, a working OIDC auth flow, and BFF API routes under `src/app/api/` (session creation, HRD discover, logout). Page-scoped components live in colocated `_components/` directories (e.g., `app/(dashboard)/_components/`, `app/(auth)/login/_components/`). There are no tests.
- **`frontend/src/components/`** — shared app-level components (`HealthStatus`, `LogoutButton`, `links/`). There are no tests.
- **`frontend/src/lib/`** — shared utilities including `lib/routing/` (`api-routes.ts`, `routes.ts`, `external-urls.ts`) and `lib/oidc-client.ts`. There are no tests.
- **`frontend/src/server/`** — server-only modules (`client.ts`, `config.ts`, `session.ts`, `auth-config.ts`) that the BFF routes depend on. There are no tests.
- **`frontend/src/shared/`** — shared TypeScript types and utilities. There are no tests.

No `vitest.config.ts`, `jest.config.ts`, `playwright.config.ts`, or any test file exists anywhere in the frontend. No `test` script exists in any `package.json`. The CI pipeline has no test step.

### User Pain Points

- **Developers modifying Terra components** have no automated check that a change to a layout primitive hasn't broken the 20 other components that may depend on it. The only signal is a manual Storybook inspection.
- **Developers building or modifying BFF route handlers** cannot verify that a handler correctly maps backend errors to the right HTTP responses, or validates request shapes — without running the full stack.
- **Developers modifying shared components** (`HealthStatus`, `DashboardSidebar`) cannot verify that data-fetching logic, loading states, and error states all render correctly without manually running the app.
- **AI assistants (Forge)** generating new components or modifying existing ones have no executable specification to validate against. Changes that look syntactically correct can silently break behavior.
- **Reviewers in CI** approve PRs without any automated quality gate. All regression detection happens manually or in production.
- **Future contributors** adding new workspace packages have no established pattern to follow — each contributor makes independent, likely inconsistent choices.

### Business Impact

- Undetected regressions in Terra components propagate to every page that uses them, multiplying the blast radius of a single bad commit.
- The BFF boundary — the critical security layer that seals iron-session cookies, proxies OIDC token exchange, and prevents the browser from calling the Go backend directly — has no coverage. A bug in error-handling or session sealing could silently affect all users.
- Without tests, refactoring is high-risk. The codebase becomes harder to evolve as it grows, increasing development cost.
- The absence of a test pattern means technical debt compounds as new pages and packages are added.

---

## Goals and Objectives

### Primary Goals

1. Establish a **layered, package-aware test architecture** that assigns the right test type to the right layer: unit tests for pure logic, component tests for rendering and interaction, integration tests for BFF routes and page-level components, and end-to-end tests for critical user flows.
2. Give **Terra (`@orbit/terra`) its own isolated test suite** that runs entirely within the package, with no dependency on the Next.js app. Tests must be fast enough to run on every save during component development.
3. Give the **Next.js application** a test suite covering BFF route handlers, shared components, and server-side modules using a Next.js-compatible test runner.
4. Establish **integration testing that prioritises real application state over module mocking** — tests should intercept at the HTTP boundary (via MSW), not replace entire modules, so the actual component logic and data-fetching pipeline are exercised.
5. Provide a **single, canonical `test-utils/` location** for all shared mocking utilities — MSW handlers, factory functions, and provider wrappers — typed against real application interfaces. Ad-hoc inline mocks are not acceptable.
6. Add **Playwright end-to-end tests** for the critical happy-path flows that span the full frontend stack (browser through BFF), with the Go backend mocked at the network level.
7. Establish a **template for future workspace packages** so that any new package added under `src/packages/*/` inherits the same test setup pattern with no ambiguity.

### Success Metrics

- A single `make` target from the repo root runs all frontend tests across all packages and exits non-zero on any failure.
- Terra's component tests achieve ≥ 30% statement coverage at the end of Phase 1, growing to ≥ 70% by Phase 3.
- App-level tests achieve ≥ 15% statement coverage at the end of Phase 1, growing to ≥ 45% by Phase 3.
- Every BFF route handler has at least one test for the happy path and one for each mapped error condition — this is a mandatory per-route requirement, not a coverage threshold.
- Every shared component in `src/components/` has at least one rendering test.
- All tests run in CI on every PR targeting the `frontend/` subtree.
- A new workspace package can be added under `src/packages/*/` and be test-ready by following a one-page setup guide, with no changes to the root config.
- Playwright E2E tests run against a production-like Next.js build, covering at minimum: page load, health status display (Go backend mocked), and authenticated session state.

### Non-Goals

- **Snapshot testing** — not adopted. Snapshot tests for component output are brittle against CVA class reordering, Tailwind version upgrades, and token renames, producing high churn with low signal. Stable, readable `toEqual` assertions on discrete values are used instead of snapshots for any pure-data structure that warrants pinning.
- **`@storybook/addon-interactions` CI integration** — not adopted. Storybook is the visual development and documentation environment for Terra. It is not a test runner. `addon-interactions` enables interaction playback inside the Storybook UI during development; it is not wired into CI and is not a substitute for Vitest component tests. This is a firm decision, not a Phase 3 consideration.
- **Full-stack system tests** — entirely out of scope for this PRD. Tests that require the Go backend, Postgres, or Redis to be running are system-level concerns, not frontend concerns. If and when they are needed, they belong outside `frontend/` and require a dedicated ADR of their own.
- Visual regression / screenshot diffing — a future concern once the component library is stable.
- Backend Go tests — a separate concern in the existing Go project.
- Performance or load testing for the frontend.
- 100% coverage targets — coverage thresholds are a starting baseline, not a ceiling to chase.

---

## User Stories

### Primary User Stories

**As a** developer modifying a Terra component,  
**I want to** run `vitest run` in `frontend/src/packages/terra/` and get pass/fail feedback in under 10 seconds,  
**So that** I can confidently iterate on component variants without running the full app or manually inspecting Storybook.

**Acceptance Criteria:**
- [ ] `test` script exists in `frontend/src/packages/terra/package.json`
- [ ] Tests run via Vitest (reusing the existing `vite.config.ts` transform pipeline)
- [ ] A `Button` component test asserts: renders with correct role, responds to `onClick`, is disabled when `disabled={true}` is passed, renders with `fullWidth` class applied
- [ ] Tests complete in under 10 seconds on a developer laptop for the full Terra suite

---

**As a** developer building or modifying a BFF API route,  
**I want to** test the route handler end-to-end using MSW to intercept the Go backend call — exercising the real `orbitClient` request pipeline — and assert correct responses for success, error, and timeout cases,  
**So that** I can verify the full BFF contract (including `orbitClient` error parsing) without running the Go backend.

**Acceptance Criteria:**
- [ ] The `/api/auth/session` route handler has tests covering: POST happy path (returns session cookie), 4xx backend error (ApiError propagation), backend timeout, and unexpected error
- [ ] MSW intercepts outbound calls from `orbitClient` at the HTTP layer — `orbitClient` itself is not mocked
- [ ] `server-only` is aliased to a no-op in the test environment so route handlers can be imported
- [ ] Tests run via Vitest without requiring `next dev` to be running

---

**As a** developer working on any shared component in `src/components/` or a page-scoped component in `_components/`,  
**I want to** write integration tests that serve controlled HTTP responses via MSW and assert what the component renders across loading, success, and error states,  
**So that** refactoring a component's data-fetching logic doesn't silently break the states that depend on it.

**Acceptance Criteria:**
- [ ] `renderWithProviders()` from `test-utils/` is the standard render harness for all component tests
- [ ] `HealthStatus` has tests for: loading state, healthy response (status and service name rendered), error state (unreachable message rendered) — all driven by MSW handlers
- [ ] `DashboardSidebar` has tests for: renders nav items correctly, renders user email from session response (via MSW), renders fallback when session fetch fails

---

**As a** CI pipeline,  
**I want to** run all frontend tests on every PR and block merge if any test fails,  
**So that** regressions are caught before they land in the main branch.

**Acceptance Criteria:**
- [ ] A single `make` target from the repo root runs all tests across all packages
- [ ] The CI step exits non-zero if any test fails in any package
- [ ] Test output clearly identifies which package and which test file failed
- [ ] Coverage reports are generated and uploaded as CI artifacts (not required to pass, but recorded)

---

**As a** developer adding a new workspace package under `src/packages/`,  
**I want to** follow a documented, one-time setup pattern to get a test suite running,  
**So that** I don't have to make tooling decisions from scratch or diverge from established conventions.

**Acceptance Criteria:**
- [ ] A setup guide (`docs/guides/frontend-testing.md`) documents the exact steps: install Vitest, copy config shape, add `test` script
- [ ] The root `make` target picks up the new package's tests automatically
- [ ] The new package's tests run in full isolation — no shared global state with other packages

---

### Secondary User Stories

**As an** AI assistant (Forge) generating or modifying frontend code,  
**I want to** run the test suite after making changes and see structured pass/fail output,  
**So that** I can self-validate changes without requiring the developer to manually inspect the output.

**As a** developer writing a new Terra component from scratch,  
**I want** a test file template or example I can copy,  
**So that** I can write the first test for a new component in under 5 minutes.

---

## Proposed Solution

### High-Level Approach

The strategy is **four tiers of tests, each with a clear owner and execution context**, with tooling selected to minimize friction against the existing stack.

```
Tier 1 — Unit (pure logic)
  Scope:  src/shared/, src/lib/routing/, pure helper functions
  Runner: Vitest (app-level config)
  Speed:  < 100ms per file

Tier 2 — Component (rendering + interaction)
  Scope:  @orbit/terra components
  Runner: Vitest + React Testing Library + happy-dom
  Speed:  < 10s for full Terra suite (cold start)

Tier 3 — Integration (BFF routes, component + MSW)
  Scope:  src/app/api/ route handlers, src/components/, page _components/
  Runner: Vitest + React Testing Library + jsdom + MSW
  Speed:  < 30s for full app integration suite (cold start)
  Files:  *.integration.test.ts(x) — suffix distinguishes from unit tests at a glance

Tier 4 — End-to-End (critical user flows)
  Scope:  Browser → Next.js BFF (Go backend mocked at network level)
  Runner: Playwright
  Speed:  < 5 minutes for full E2E suite (headless, single browser)
```

### Test File Placement Rules

These rules are not preferences — they are the canonical structure. Any test file that does not follow these rules will not be accepted.

#### Rule 1 — Unit and component tests: colocated with source

Unit tests (Tier 1) and component tests (Tier 2, Terra) live in the same directory as the file they test, with the same base name and a `.test.ts` or `.test.tsx` extension.

```
src/lib/routing/api-routes.ts         → src/lib/routing/api-routes.test.ts
src/packages/terra/src/components/
  button/button.tsx                   → button/button.test.tsx
```

No `__tests__/` subdirectories. No `tests/` directories at any level. The test file is a sibling of the source file, nothing more.

#### Rule 2 — Integration tests: colocated with source, distinct suffix

Integration tests (Tier 3) are colocated with the source file they exercise, but use a **different file suffix** to distinguish them from unit tests: `.integration.test.ts` or `.integration.test.tsx`.

```
src/app/api/auth/session/route.ts     → src/app/api/auth/session/route.integration.test.ts
src/components/health-status.tsx      → src/components/health-status.integration.test.tsx
src/app/(dashboard)/_components/
  dashboard-sidebar.tsx               → dashboard-sidebar.integration.test.tsx
```

The suffix distinction matters for three reasons:
1. **`pnpm` script separation** — `test:unit` and `test:integration` run different file globs, so developers can run only the fast tier locally.
2. **Makefile target separation** — `make frontend-test-unit` and `make frontend-test-integration` map to those scripts, making CI step attribution precise.
3. **Reader intent** — opening a directory, you immediately know which files establish fast invariants and which exercise the full HTTP stack.

**The distinction between a unit test and an integration test is also in the test body:** a unit test never starts an MSW server, never imports from `@orbit/test-utils/msw`, and never reads from `test-utils/factories/`. If your test does any of these things, it is an integration test and must use the `.integration.test.ts(x)` suffix.

#### Rule 3 — E2E tests: top-level `frontend/e2e/`

Playwright tests are never colocated with source files. They live in a dedicated `frontend/e2e/` directory, organised by the user-facing feature or flow they exercise.

```
frontend/e2e/
├── auth/
│   └── login.spec.ts       # HRD form → discover mock → redirect
└── dashboard/
    └── health.spec.ts      # page load, health status display
```

The `spec.ts` / `spec.tsx` extension is used for all Playwright tests to distinguish them from Vitest tests at a glance. Vitest tests use `.test.ts` / `.test.tsx`.

#### Rule 4 — Full-stack system tests: out of scope

Tests that require the Go backend, Postgres, or Redis are **not frontend tests and are not in scope for this PRD or ADR**. The repo-root `e2e/` directory does not exist and must not be created as part of this work. If full-stack system tests are ever warranted, they will be addressed by a dedicated ADR of their own — covering ownership, infrastructure, and CI model — before any files are written.

**The boundary rule:** nothing in `frontend/e2e/` may require the Go backend, Postgres, or Redis to be running. If a test needs the real backend, it does not belong in `frontend/` at all.

#### Rule 5 — Shared test infrastructure: `src/test-utils/`

Test utilities shared across multiple test files live in `frontend/src/test-utils/`. They are never duplicated inline. See the Centralised Mocking Utilities section for the full structure.

#### Rule 6 — No test files in `test-utils/`

`src/test-utils/` contains helpers, not tests. No `.test.ts` or `.spec.ts` files live inside `test-utils/`.

#### Summary table

| Test type | File suffix | Location | pnpm script |
|-----------|-------------|----------|-------------|
| Unit | `*.test.ts(x)` | Colocated with source | `test:unit` |
| Component (Terra) | `*.test.tsx` | Colocated within Terra package | `test:unit` |
| Integration | `*.integration.test.ts(x)` | Colocated with source | `test:integration` |
| E2E — Category A (frontend, Go mocked) | `*.spec.ts` | `frontend/e2e/` | `test:e2e` |
| E2E — Category B (full-stack) | — | **out of scope** — requires dedicated ADR | n/a |
| Shared test infrastructure | n/a | `frontend/src/test-utils/` | n/a |

#### When to write an integration test

An integration test is warranted when **at least one** of the following is true:

1. **The code makes an HTTP call.** Any module that calls `fetch`, `orbitClient.request`, or any other HTTP primitive must have an integration test that exercises that call end-to-end with MSW intercepting — including error branches and timeout behaviour. This is non-negotiable.
2. **The code reads from or writes to an iron-session cookie.** BFF route handlers that call `getIronSession()` must be tested with the session lifecycle active, not stubbed away.
3. **The code exercises more than one module boundary.** If a component imports from `@orbit/lib/routing`, calls a BFF route, and reacts to the response — that is multiple boundaries and warrants integration coverage.
4. **The behaviour depends on React context or provider state.** Components that consume a context (e.g., future session or query provider) and change their output based on it need an integration test that renders the full provider tree via `renderWithProviders()`.

An integration test is **not** warranted when:
- The function is pure (same input → same output, no side effects). Write a unit test.
- The component renders static markup driven entirely by props. Write a component/unit test.
- The only "integration" is prop drilling through three layers. Write a unit test with a well-chosen prop.

If in doubt: **if you need to import anything from `@orbit/test-utils/msw` or `@orbit/test-utils/factories`, it is an integration test.** Name the file accordingly.

---

### Package Isolation Model

Each workspace package owns its own test configuration:

**`frontend/src/packages/terra/`** — has its own `vitest.config.ts` (extending `vite.config.ts`), its own `test` script, and runs completely independently of the Next.js app. Coverage is scoped to `src/components/**` and `src/utils.ts`.

**`frontend/` (the Next.js app)** — has two root-level Vitest configs that together cover `src/app/`, `src/components/`, `src/server/`, `src/lib/`, and `src/shared/`:

- `vitest.config.unit.ts` — includes `**/*.test.ts?(x)`, excludes `**/*.integration.test.ts?(x)` and `src/packages/**`
- `vitest.config.integration.ts` — includes only `**/*.integration.test.ts?(x)`, excludes `src/packages/**`

Both configs exclude `src/packages/**` so that Terra's tests never run under the wrong environment (`jsdom`) or the wrong Vite config. Terra is exercised exclusively by `make frontend-test-terra` / `make frontend-test-unit` via `pnpm --filter @orbit/terra`.

Both configs also include the `server-only` shim alias, the `@orbit/test-utils` path alias pointing at `src/test-utils/`, and `setupFiles: ['./src/test-utils/setup.ts']`.

**Future packages** — any package added under `src/packages/*/` follows the Terra pattern: its own `vitest.config.ts`, its own `test` script, and self-contained coverage. The root `make` target runs all packages.

### No Real HTTP Calls in Any Test

**This is an absolute rule with no exceptions across all test tiers.**

No unit test, integration test, or frontend E2E test may ever make a real HTTP or HTTPS call to any external service — the Go backend, Postgres, Redis, any third-party API, or any other networked endpoint. Every HTTP call must be intercepted and served a controlled, in-process response.

| Tier | How real HTTP is prevented |
|------|--------------------------|
| Unit (Tier 1) | Pure functions make no HTTP calls. If yours does, it is not a unit test. |
| Integration (Tier 3, Node/jsdom) | MSW `setupServer()` with `onUnhandledRequest: 'error'` — any fetch that escapes a handler causes the test to fail immediately. |
| E2E — Category A (Playwright) | `page.route()` intercepts **all** outbound requests from the browser and BFF; unmatched routes return a 500 so the test fails loudly rather than silently hitting a real service. |

The MSW server in `test-utils/msw/server.ts` is configured as:

```ts
export const server = setupServer(...defaultHandlers);
```

`onUnhandledRequest: 'error'` is passed to `server.listen()` in `setup.ts`, not to `setupServer()`:

```ts
// src/test-utils/setup.ts
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
```

> **API note:** `setupServer()` accepts only `RequestHandler[]` as arguments. The `onUnhandledRequest` option is **not** a valid argument to `setupServer()` — it belongs exclusively in the `server.listen()` call. Passing it to `setupServer()` is silently ignored by MSW v2.

`onUnhandledRequest: 'error'` is not a style choice — it is the enforcement mechanism. A test that forgets to register a handler fails instead of silently making a real network request. This guarantees that no test is ever inadvertently passing because it reached a live service.

For Playwright E2E (Category A), the baseline `playwright.config.ts` uses a global `beforeEach` fixture that registers a catch-all `page.route()` handler returning a 503 for any URL not explicitly intercepted by the test. Individual tests register the routes they need before navigation; anything unregistered is blocked with a clear error response rather than silently reaching a live service.

### Integration Testing: State Over Mocking

The guiding principle for Tier 3 tests is to **manipulate real application state rather than replace modules**. In practice this means:

- **MSW intercepts at the HTTP boundary**, not at the module boundary. `orbitClient.request<T>` runs as written, including URL construction, header injection, `AbortSignal` timeout, and `ApiError` parsing. Only the TCP connection is intercepted.
- **Module mocks are used only where unavoidable** — specifically `next/navigation` hooks (`usePathname`, `useRouter`) and the `oidc-client-ts` library (whose `signinRedirect()` triggers a real browser navigation that cannot run in jsdom).
- **`renderWithProviders()`** is the single render harness, wrapping RTL's `render` with all required React contexts and the MSW server lifecycle. Individual test files never import `render` from `@testing-library/react` directly.

**Current architectural note:** Several components (`DashboardSidebar`, `HealthStatus`) use independent `useEffect` + `fetch` patterns for data access. This makes MSW the right tool today — each component's fetch is intercepted and served a controlled response. As the app matures, a session context and/or React Query will allow tests to seed data directly into the query cache or context provider, eliminating HTTP interception for session-aware components. When that architecture lands, the corresponding tests should be updated to use the seeding approach. The MSW handlers remain as the integration-level fallback and for BFF route tests.

### No Snapshot Testing

Snapshot tests are explicitly not adopted for component output. Terra components are built with CVA — the resolved className strings are long, fragile against token renames and Tailwind version changes, and provide no behavioral signal. Any value worth pinning (e.g., the exact string returned by `ApiRoute.session.GET()`) is more clearly expressed as an explicit `toEqual` assertion.

### BFF Route Test Strategy

BFF route handlers import `server-only` at the top of their dependency chain. The test environment neutralises this via a Vitest alias that replaces `server-only` with an empty module. This alias is scoped to the test environment and has no effect on production builds.

The test strategy for BFF routes:

1. MSW `setupServer()` intercepts outbound calls from `orbitClient` to the Go backend URL.
2. The route handler's exported function (`GET`, `POST`) is imported and called directly.
3. MSW serves a controlled response — success, 4xx error, network timeout.
4. The test asserts on the `NextResponse` return value (status, headers, body).

This means `orbitClient`'s full pipeline is exercised — URL construction, `AbortSignal.timeout(10_000)`, `ApiError` parsing — while the Go backend is never contacted.

### Centralised Mocking Utilities (`test-utils/`)

All shared test infrastructure lives in `frontend/src/test-utils/`. This directory is not a workspace package — it has no `package.json` and is never published. It is imported via a `@orbit/test-utils` path alias configured in both `tsconfig.json` and both Vitest app configs (`vitest.config.unit.ts` and `vitest.config.integration.ts`).

```
frontend/src/test-utils/
├── index.ts                    # re-exports: renderWithProviders, createSessionUser, etc.
├── render.tsx                  # renderWithProviders() — RTL render + all React contexts
├── setup.ts                    # @testing-library/jest-dom matchers, MSW server lifecycle
│                               #   beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
│                               #   afterEach(() => server.resetHandlers())
│                               #   afterAll(() => server.close())
├── server-only-shim.js         # intentional no-op — target of the 'server-only' alias in both vitest configs
├── msw/
│   ├── server.ts               # setupServer(...defaultHandlers) — no options; options go in listen()
│   └── handlers/
│       ├── index.ts            # aggregates all domain handlers (the default handler set)
│       ├── auth.ts             # handlers for /api/auth/session, /api/auth/discover, /api/auth/logout
│       └── health.ts           # handler for /api/identity/health
└── factories/
    ├── session.ts              # createSessionUser(), createSessionPayload() — typed against SessionUser, SessionPayload
    └── health.ts               # createHealthResponse() — typed against HealthResponse
```

Key rules:
- **Handlers are typed against real response interfaces.** `createHealthResponse({ status: 'degraded' })` merges overrides into a valid default. When an interface changes, TypeScript surfaces stale mocks at compile time.
- **Handlers are reused across tiers.** Vitest integration tests and Playwright E2E tests import from the same handler files.
- **Terra does not use `test-utils/`.** Terra tests are isolated component behavior tests that construct any needed props inline. The `test-utils/` directory is app-level only.

### E2E Test Categories

Two categories of E2E tests are distinguished:

**Category A — Frontend-isolated** (`frontend/e2e/`): Playwright tests where the Go backend is entirely mocked via `page.route()`. The full Next.js routing, middleware, and BFF pipeline is exercised; only outbound calls from the BFF to the Go backend are intercepted. These are owned by the frontend team, run in `make frontend-test`, and require no external services beyond the Next.js build.

**Category B — Full-stack (out of scope):** Tests that require the real Go backend, Postgres, or Redis are system-level concerns, not frontend concerns. They are entirely out of scope for this PRD. No directory, no CI step, and no Playwright config for them should be created as part of this work. If and when they are needed, a dedicated ADR will define them.

### Storybook's Role

Storybook is the visual development and documentation environment for Terra. It is **not a test runner** and is **not part of the CI test pipeline** defined by this PRD.

- **Vitest component tests** assert behavioral correctness: does the component render the right DOM, respond to events, and handle edge-case props? This is the authoritative source of correctness signal.
- **Storybook stories** document usage, showcase variants, and support manual visual inspection during development.
- **`@storybook/addon-a11y`** (already installed) surfaces accessibility issues within the Storybook UI during development — complementary, not a replacement for Vitest.
- **`@storybook/addon-interactions`** (already installed) enables interaction playback within the Storybook UI — a developer convenience, not a CI gate. It is not wired into `make check`, not invoked by any `make frontend-test*` target, and not counted toward coverage. This is a firm decision, not a future consideration.

If a story breaks because of a component change, that is caught by the Vitest component test for that component — not by Storybook CI. Stories that become stale are a documentation concern, addressed in code review.

### Key Components

- **`frontend/src/packages/terra/vitest.config.ts`** — Terra's isolated test config extending `vite.config.ts`, `environment: 'happy-dom'`, globbing `src/**/*.test.{ts,tsx}`.
- **`frontend/vitest.config.unit.ts`** — App unit tier; `environment: 'jsdom'`; includes `**/*.test.ts?(x)`, excludes `**/*.integration.*` and `src/packages/**`; aliases `server-only` to the no-op shim; aliases `@orbit/test-utils` to `./src/test-utils`; `setupFiles: ['./src/test-utils/setup.ts']`.
- **`frontend/vitest.config.integration.ts`** — App integration tier; same environment, aliases, and `setupFiles` as the unit config; includes only `**/*.integration.test.ts?(x)`, excludes `src/packages/**`.
- **`frontend/playwright.config.ts`** — E2E config targeting a built Next.js app on `localhost:3000`.
- **`frontend/src/test-utils/`** — Centralised mocking utilities: MSW server, handlers, factory functions, `renderWithProviders`.
- **`frontend/src/packages/terra/src/components/**/*.test.tsx`** — Component tests co-located with Terra components.
- **`frontend/src/app/api/**/*.integration.test.ts`** — BFF route integration tests co-located with route handlers.
- **`frontend/src/components/**/*.integration.test.tsx`** — Shared component integration tests (MSW-backed) co-located with components.
- **`frontend/src/app/(dashboard)/_components/**/*.integration.test.tsx`** — Page-scoped component integration tests.
- **`frontend/src/lib/**/*.test.ts`** — Unit tests for routing utilities and pure helpers (no MSW, no factories).
- **`frontend/e2e/`** — Playwright Category A E2E tests.

---

## Scope

### In Scope

- Vitest configuration for `@orbit/terra` (Terra package)
- Vitest configuration for the Next.js app (covering `src/app/`, `src/components/`, `src/server/`, `src/lib/`, `src/shared/`)
- React Testing Library setup for both test environments
- `test-utils/` directory with MSW server, typed handlers, factory functions, and `renderWithProviders`
- MSW setup for BFF route handler integration tests (Phase 1, not deferred)
- Playwright configuration and baseline E2E Category A tests
- Coverage reporting configuration (`@vitest/coverage-v8`)
- Root-level `make` target that runs all packages
- CI integration
- A documented pattern and setup guide for future workspace packages
- An initial set of tests covering: at least 3 Terra components (Button, Badge, Typography), BFF route handler tests for `/api/auth/session`, `HealthStatus` and `DashboardSidebar` shared components, and at least 1 Playwright E2E flow

### Out of Scope

- Snapshot testing (explicitly rejected — see Non-Goals)
- Visual regression / screenshot testing
- Storybook interaction test automation in CI (explicitly rejected — see Non-Goals)
- Full-stack system tests requiring Go backend, Postgres, or Redis (explicitly out of scope — future ADR if ever needed)
- Performance benchmarking
- Backend Go tests

### Future Considerations

- **Visual regression with Chromatic or Percy**: Screenshot diffing for Terra components on every PR.
- **Contract testing**: If a typed API schema is introduced (e.g., OpenAPI), contract tests between the BFF and Go backend.
- **Coverage enforcement**: Ratcheting coverage thresholds upward as the test suite matures.
- **Workspace-level coverage aggregation**: Merging coverage reports from all packages into a single report.
- **State-over-mocking architecture**: Session context and React Query adoption to allow tests to seed data directly into the query cache or context provider, replacing MSW for session-aware component tests.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] A `make` target from the repo root runs all tests across all packages; exits non-zero on any failure
- [ ] Terra has a `vitest.config.ts`, a `test:unit` script, and a `test` script (alias for `test:unit`) in its `package.json`
- [ ] The app has two Vitest configs (`vitest.config.unit.ts` and `vitest.config.integration.ts`), a `test:unit` script, a `test:integration` script, and a `test` script (runs both) in `frontend/package.json`; each config's `include` glob selects exactly one tier — `--include`/`--exclude` CLI flags are not used as they are additive, not exclusive
- [ ] `server-only` is aliased to `./src/test-utils/server-only-shim.js` (a no-op) in both app Vitest configs; `server-only-shim.js` exists in `src/test-utils/`
- [ ] `test-utils/setup.ts` is listed under `setupFiles` in both app Vitest configs; it registers `@testing-library/jest-dom` matchers and the MSW server lifecycle: `beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))`, `afterEach(() => server.resetHandlers())`, `afterAll(() => server.close())`
- [ ] `test-utils/` directory exists with: `server-only-shim.js`, `setup.ts` (MSW lifecycle), `msw/server.ts` (`setupServer(...defaultHandlers)` — no options), typed handlers for `/api/auth/session` and `/api/identity/health`, factory functions for `SessionUser`, `SessionPayload`, and `HealthResponse`, and `renderWithProviders()`
- [ ] `server.listen({ onUnhandledRequest: 'error' })` is called in `setup.ts` `beforeAll` — no exceptions; any unhandled request in a test is a test failure; `onUnhandledRequest` is never passed to `setupServer()`
- [ ] BFF route integration tests for `/api/auth/session` (POST + GET, all error branches) are named `route.integration.test.ts` and use MSW exclusively — no module mocking of `orbitClient`
- [ ] Tests for at least 3 Terra components covering: rendering with props, variant switching, and disabled/loading states where applicable — named `*.test.tsx`
- [ ] `HealthStatus` integration tests covering: loading, healthy, and error display states — named `health-status.integration.test.tsx`, all HTTP via MSW
- [ ] `DashboardSidebar` integration tests covering: renders nav items, renders user email from session response — named `dashboard-sidebar.integration.test.tsx`, all HTTP via MSW
- [ ] Playwright config exists and at least 1 E2E test covers: app loads and health status is displayed (with mocked Go backend via `page.route()`); unmatched routes return an error response
- [ ] All tests run in CI on every PR via separate `make frontend-test-unit`, `make frontend-test-integration`, and `make frontend-test-e2e` targets; `make frontend-test-terra` is available for local Terra-only development workflows
- [ ] Coverage reports are generated (not gated) on every CI run

**Should Have (P1):**
- [ ] Coverage thresholds configured for reporting: Terra ≥ 30% statements, App ≥ 15% statements (reporting-only, never block CI)
- [ ] Per-route mandatory rule enforced via convention: every BFF route handler must have happy-path and all error-branch tests before merge
- [ ] `lib/routing/api-routes.ts` has 100% path coverage (pure functions, trivially testable)
- [ ] `lib/routing/routes.ts` (`isSafeRedirectPath`) has tests for all edge cases (empty string, relative paths, absolute URLs, `//` protocol-relative)
- [ ] A setup guide in `docs/guides/frontend-testing.md` documenting the pattern for new workspace packages
- [ ] Vitest reporters configured for CI (JSON/JUnit output) and local (verbose/dot)
- [ ] `test:watch` script for local development

**Nice to Have (P2):**
- [ ] `test:coverage` command that opens the HTML coverage report
- [ ] A test file template for new Terra components (`.test.tsx` boilerplate in the testing guide)
- [ ] Playwright tests for authentication error states (e.g., HRD discover returns 404)
- [ ] GitHub Actions matrix running Terra tests and app tests in parallel

### Non-Functional Requirements

**Performance:**
- Full Terra test suite completes in under 10 seconds cold start on developer hardware
- Full app integration test suite completes in under 30 seconds cold start on developer hardware
- Vitest watch mode re-run on a single changed file completes in under 3 seconds
- Playwright E2E suite completes in under 5 minutes (headless, single browser)

**Reliability:**
- Tests must be deterministic — no flaky tests allowed to merge. Any flaky test must be quarantined or fixed before it blocks CI.
- E2E tests must not depend on real network calls to the Go backend. The backend must be mocked via `page.route()`.
- MSW handlers must not accidentally proxy to real backends in CI.

**Maintainability:**
- Test file placement follows the rules defined in the Test File Placement Rules section — unit, component, and integration tests are colocated with source; E2E tests live in `frontend/e2e/`; shared infrastructure lives in `src/test-utils/`.
- No shared global test state between packages.
- All MSW handlers and factory functions live in `test-utils/` — zero ad-hoc inline mocks.
- The `server-only` alias is the only framework-specific shim in the test config.
- When a response interface changes in `src/shared/api.ts` or `src/server/auth-config.ts`, TypeScript immediately surfaces stale factory functions in `test-utils/factories/`.

**Developer Experience:**
- Running tests in watch mode during development requires a single command.
- Test output clearly identifies the failing file and assertion without requiring `--reporter=verbose` as a flag.
- Adding a new test file to Terra or the app is discovered automatically (no manual file registration).
- `renderWithProviders()` is the only import a test file needs for component rendering — no per-test MSW server boilerplate.

**Security:**
- Test configurations must not include real credentials, API keys, or connection strings. All secrets in test environments use placeholder values or are read from `.env.test` (gitignored).
- MSW handlers serve synthetic responses only — never proxy to real backend endpoints.
- The `server-only` shim intentionally bypasses the runtime guard. This must be documented clearly in the testing guide so contributors understand: (1) why it exists, (2) that it does not weaken any production security boundary, and (3) that it applies automatically to all server-only modules without per-module configuration.

---

## Dependencies

### Internal Dependencies

- **`vite@6.3.5`** (`@orbit/terra`): Already installed. Vitest extends this directly.
- **`@vitejs/plugin-react@4.5.2`** (`@orbit/terra`): Already installed. Required for JSX transform.
- **`next@16.1.6`** (app): The test environment must be compatible with Next.js App Router patterns — dynamic imports, `server-only` guard, `NextResponse`.
- **`react@19.2.3`** (app): React 19 introduces changes to `act()` and concurrent rendering. RTL version must be compatible.
- **`pnpm-workspace.yaml`** (`frontend/`): Declares `packages: ['src/packages/*']`. The root `make` targets run tests across packages via separate `pnpm run test:unit`, `pnpm run test:integration`, and `pnpm exec playwright test` invocations — **not** a single `pnpm -r run test` call, because integration and E2E tests require different setup and must be attributable separately in CI.

### External Dependencies (new packages to install)

**In `frontend/src/packages/terra/`:**
- `vitest` — test runner
- `@vitest/coverage-v8` — coverage provider
- `@testing-library/react` — component rendering
- `@testing-library/user-event` — user interaction simulation
- `@testing-library/jest-dom` — DOM assertion matchers
- `happy-dom` — lightweight DOM environment

**In `frontend/` (app):**
- `vitest` — test runner
- `@vitest/coverage-v8` — coverage provider
- `@testing-library/react` — component rendering
- `@testing-library/user-event` — user interaction simulation
- `@testing-library/jest-dom` — DOM assertion matchers
- `jsdom` — DOM environment (better Next.js compatibility than happy-dom)
- `msw` — HTTP interception for BFF route and component integration tests
- `@playwright/test` — E2E test runner

All new packages installed as `devDependencies`.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| `server-only` guard breaks test imports for BFF routes and `src/server/` | High | High | Alias `server-only` → empty module in Vitest config. Standard community pattern. |
| React 19 compatibility issues with RTL or Vitest | Medium | Medium | Pin RTL to a React 19-compatible version; check RTL release notes before installing. |
| Tailwind CSS v4 `@theme` tokens not available in jsdom/happy-dom environments | Low | Medium | Token correctness is a Storybook concern. RTL tests assert on class names, not computed styles. Document explicitly. |
| E2E tests become flaky due to timing issues with async fetches | Medium | Medium | Use Playwright's `waitForSelector` / `expect(locator).toBeVisible()` with appropriate timeouts; mock Go backend in E2E via `page.route()`. |
| Developers ignore tests and coverage drifts | Medium | Low | Gate PRs in CI; coverage report uploaded as artifact makes drift visible; per-route mandatory test convention enforced in PR review. |
| msw `setupServer()` in Node.js (Vitest) vs browser (Playwright) | Low | Low | msw v2 uses HTTP interceptors (`setupServer()`) in Node.js and a Service Worker in browser contexts. They share the same handler definitions. The `test-utils/msw/handlers/` files are reused across both environments. |
| New workspace packages added without following the test pattern | Medium | Low | Document the pattern clearly; add a checklist item to the PR template for new packages. |
| `useEffect` + `fetch` pattern makes component tests dependent on MSW timing | Low | Medium | Use `waitFor()` from RTL for all async state assertions. Document this pattern in `test-utils/render.tsx` comments. |

---

## Alternatives Considered

### Alternative 1: Jest instead of Vitest

**Description:** Use Jest as the test runner for both Terra and the app.

**Why not chosen:**
- Terra already uses Vite as its build and Storybook bundler. Jest requires a separate Babel/SWC transform pipeline alongside the existing Vite pipeline — two transform stacks for the same code.
- Vitest's module resolution is identical to Vite's, meaning path aliases (`@orbit/terra`, `@orbit/test-utils`, `@/server/`) work without additional configuration.
- Jest's ESM support is still experimental and requires `--experimental-vm-modules`. The codebase is fully ESM and imports ESM-only packages (`oidc-client-ts`).
- Vitest is significantly faster for watch-mode development due to HMR-aware test reruns.

### Alternative 2: Storybook interaction tests as the sole component test strategy

**Description:** Use `@storybook/addon-interactions` to write component tests within Storybook stories.

**Why not chosen:**
- Storybook interaction tests are tightly coupled to story structure. Testing edge-case props that don't warrant a full story is verbose.
- `storybook test` cold start time (full Storybook build) is much slower than Vitest.
- Storybook's role as living documentation is distinct from the role of a fast feedback test loop. Conflating them leads to stories written for test coverage rather than documentation value.

### Alternative 3: No BFF route handler tests — rely on E2E only

**Description:** Skip integration tests for route handlers; rely on Playwright E2E tests to catch BFF bugs.

**Why not chosen:**
- E2E tests cannot easily cover all error branches in a route handler without complex infrastructure.
- BFF route handler tests with MSW are fast (< 100ms per test) and provide exact assertions on error-mapping logic that E2E tests cannot.
- The BFF boundary is a security-relevant layer — session sealing, token exchange, credential isolation. It deserves isolated, deterministic testing independent of the Go backend state.

### Alternative 4: Snapshot testing for component output

**Description:** Use Vitest's snapshot testing to pin component rendered output.

**Why not chosen:**
- Terra components are built with CVA. Snapshots capture full resolved className strings that are fragile against token renames, Tailwind upgrades, and CVA internal changes. Every such change triggers a snapshot update that developers learn to run reflexively without reading.
- In an AI-assisted workflow (Forge), snapshot failures from routine component regeneration add noise and risk rubber-stamped `--updateSnapshot` runs.
- Any value worth pinning (e.g., the string returned by `ApiRoute.session.GET()`) is more clearly expressed as an explicit `toEqual` assertion that reads as specification, not as a file diff.

### Alternative 5: Module-level mocking for all integration tests

**Description:** Use `vi.mock('@/server/client')` to replace `orbitClient` in BFF route tests, and mock `fetch` globally for component tests.

**Why not chosen:**
- Module-level mocking of `orbitClient` means `client.ts`'s request pipeline — URL construction, `AbortSignal.timeout(10_000)`, `ApiError` parsing — is never exercised. These are the most security-relevant behaviors in the BFF layer.
- Global `fetch` mocking is unpredictable in complex component tests and doesn't compose well with multiple concurrent requests.
- MSW intercepts at the HTTP boundary, allowing the actual code to run while serving controlled responses. This is strictly more faithful to production behavior.

### Alternative 6: Shared `test-utils` workspace package

**Description:** Extract shared test utilities into a dedicated workspace package (`src/packages/test-utils/`) consumed by both Terra and the app.

**Why not chosen:**
- Premature for the current scale. Terra and app test utilities have minimal overlap — Terra doesn't use MSW or session factories.
- Adds a dependency edge from test packages to a shared package, complicating the isolation model.
- A local `src/test-utils/` directory accessed via path alias achieves the same result with no workspace overhead.

---

## Open Questions

All open questions have been resolved.

1. **Next.js 16 App Router test compatibility**: Does Vitest + jsdom correctly handle `server-only` aliasing, `NextResponse` instantiation, and the App Router's async request API?
   - **Status:** Resolved
   - **Decision:** The `server-only` package resolves to a throwing module under Node's default export condition. The fix is a Vitest config alias pointing at an empty shim file. Next.js 16.1.6 ships no `@next/vitest` package — the alias is the complete solution.

2. **E2E backend dependency**: Should Playwright E2E tests run against a fully local stack or should the Go backend be mocked?
   - **Status:** Resolved
   - **Decision:** Go backend must be mocked at the network level via `page.route()`. Category A E2E tests in `frontend/e2e/` require only a built Next.js app — no Go backend, no Docker Compose. Category B full-stack tests are out of scope for this PRD.

3. **Coverage thresholds**: Are the proposed thresholds the right starting point? Should thresholds block CI or only report?
   - **Status:** Resolved
   - **Decision:** Terra ≥ 30% / App ≥ 15% statements for Phase 1, growing each phase. Thresholds are reporting-only and never block CI. Per-route mandatory test rules (happy path + all error branches) are the enforceable gate, not global statement percentages.

4. **Test script naming convention and separation**: Should unit and integration tests share a single script or be split?
   - **Status:** Resolved
   - **Decision:** Unit and integration tests use distinct scripts and Makefile targets. `test:unit` runs only `*.test.ts(x)` (excluding `*.integration.test.ts(x)`). `test:integration` runs only `*.integration.test.ts(x)`. `test` is a convenience alias that runs both. Four Makefile targets exist: `make frontend-test-terra` (Terra in isolation, `happy-dom`), `make frontend-test-unit` (Terra then app, both under their correct environments), `make frontend-test-integration`, and `make frontend-test-e2e`. They are separate steps so a unit test failure does not skip integration test reporting and vice versa. `frontend-test-terra` is also the fast local target when working exclusively on Terra components.

---

## Timeline and Milestones

### Phases

**Phase 1: Foundation** (immediate priority)
- `test-utils/` directory: MSW server, typed handlers for auth and health endpoints, factory functions, `renderWithProviders()`
- Vitest config for Terra; test scripts wired; at least 3 component tests (Button, Badge, Typography)
- Vitest config for the app; `server-only` alias; `@orbit/test-utils` alias
- BFF route handler integration tests for `/api/auth/session` (POST + GET, all error branches) using MSW
- `HealthStatus` integration tests (loading, ok, error) via MSW
- `DashboardSidebar` integration tests (nav items, session data) via MSW
- Unit tests for `lib/routing/api-routes.ts` and `lib/routing/routes.ts`
- Root `make` target running all packages
- CI step added for all tests

**Phase 2: E2E and Coverage**
- Coverage thresholds reported (Terra ≥ 30%, App ≥ 15%; reporting-only, never blocks CI)
- Playwright config and baseline E2E Category A test (app loads, health status renders with mocked backend)
- Coverage reports uploaded as CI artifacts
- `docs/guides/frontend-testing.md` setup guide written
- Expand BFF handler tests to cover `/api/auth/discover` and `/api/auth/logout`

**Phase 3: Maturity**
- Coverage expanded to all existing Terra components (target ≥ 70%)
- App coverage target ≥ 45%
- Additional E2E flows as pages are built (auth flow, session expiry)
- Coverage aggregation across workspace packages evaluated
- Session context / React Query adoption to enable query-cache seeding for component tests; migrate relevant integration tests away from MSW toward provider seeding where appropriate

### Key Milestones

- [ ] PRD Approved
- [ ] ADR Created
- [ ] Phase 1 implementation complete — all tests green in CI
- [ ] Phase 2 implementation complete — E2E running in CI
- [ ] Phase 3 — ongoing as product features land

---

## Appendix

### Related Documents

- [PRD: Orbit Terra Design System](./design-system.md) — establishes the workspace package structure this strategy builds on
- [Terra README](../../frontend/src/packages/terra/README.md) — AI assistant rules including Rule 8 (do not modify Terra while working on app code)
- [ADR-0030](../adr/0030-frontend-test-strategy-and-toolchain.md) — test tooling selection, Next.js compatibility, E2E strategy

### Codebase Reference

| Path | Description |
|------|-------------|
| `frontend/src/packages/terra/` | `@orbit/terra` workspace package |
| `frontend/src/packages/terra/vite.config.ts` | Existing Vite config (Vitest will extend this) |
| `frontend/src/packages/terra/package.json` | Already has `vite`, `@vitejs/plugin-react`, `@storybook/addon-a11y` |
| `frontend/src/components/health-status.tsx` | Shared component — primary integration test target |
| `frontend/src/components/logout-button.tsx` | Shared component |
| `frontend/src/app/(dashboard)/_components/` | Page-scoped dashboard shell components |
| `frontend/src/app/(auth)/login/_components/` | Page-scoped auth components |
| `frontend/src/app/api/auth/session/route.ts` | BFF route handler — primary integration test target |
| `frontend/src/app/api/auth/discover/route.ts` | BFF route handler — HRD proxy |
| `frontend/src/app/api/auth/logout/route.ts` | BFF route handler — session destruction |
| `frontend/src/server/client.ts` | `orbitClient` — tested via MSW interception, not module mock |
| `frontend/src/server/auth-config.ts` | `SessionUser`, `SessionPayload`, `BackendSessionResponse` — factory types |
| `frontend/src/lib/routing/api-routes.ts` | `ApiRoute` — unit testable, pure functions |
| `frontend/src/lib/routing/routes.ts` | `isSafeRedirectPath` — unit testable, security-relevant |
| `frontend/src/shared/api.ts` | `HealthResponse` — factory type |
| `frontend/src/test-utils/` | Centralised mocking utilities (to be created) |
| `frontend/pnpm-workspace.yaml` | Declares `packages: ['src/packages/*']` |

### Folder Structure

File suffix rules in one place:
- `*.test.ts(x)` — unit or component test. No MSW. No imports from `@orbit/test-utils/msw` or `@orbit/test-utils/factories`. Pure functions, rendering with props, event handlers.
- `*.integration.test.ts(x)` — integration test. Must use MSW (`@orbit/test-utils/msw`) and/or factories. Exercises HTTP calls, session lifecycle, provider trees, or multi-module boundaries.
- `*.spec.ts` — E2E test (Playwright). Lives in `frontend/e2e/` only. Go backend mocked via `page.route()`.
- No file inside `test-utils/` is itself a test file.

```
frontend/
├── e2e/                                      # Category A: frontend-isolated Playwright tests only
│   ├── auth/
│   │   └── login.spec.ts
│   └── dashboard/
│       └── health.spec.ts
├── src/
│   ├── test-utils/                           # Shared test infrastructure — no *.test.ts files here
│   │   ├── index.ts
│   │   ├── render.tsx                        # renderWithProviders()
│   │   ├── setup.ts                          # jest-dom matchers + server.listen({ onUnhandledRequest: 'error' })
│   │   ├── server-only-shim.js               # empty module aliased to 'server-only' in Vitest config
│   │   ├── msw/
│   │   │   ├── server.ts                     # setupServer(...defaultHandlers) — options belong in listen()
│   │   │   └── handlers/
│   │   │       ├── index.ts
│   │   │       ├── auth.ts                   # /api/auth/session, /api/auth/discover, /api/auth/logout
│   │   │       └── health.ts                 # /api/identity/health
│   │   └── factories/
│   │       ├── session.ts                    # SessionUser, SessionPayload, BackendSessionResponse
│   │       └── health.ts                     # HealthResponse
│   ├── app/
│   │   ├── (auth)/login/
│   │   │   ├── page.tsx
│   │   │   ├── page.test.tsx                 # unit: renders form, displays error prop
│   │   │   └── _components/
│   │   │       ├── email-sign-in-form.tsx
│   │   │       ├── email-sign-in-form.test.tsx             # unit: field rendering, validation messages
│   │   │       └── email-sign-in-form.integration.test.tsx # integration: calls /api/auth/discover via MSW
│   │   ├── (dashboard)/
│   │   │   ├── page.tsx
│   │   │   ├── page.test.tsx                 # unit: renders with hardcoded data (current state)
│   │   │   └── _components/
│   │   │       ├── dashboard-sidebar.tsx
│   │   │       ├── dashboard-sidebar.test.tsx             # unit: renders nav items with props
│   │   │       ├── dashboard-sidebar.integration.test.tsx # integration: fetches session via MSW
│   │   │       ├── dashboard-header.tsx
│   │   │       └── dashboard-header.test.tsx              # unit: renders title, slot props
│   │   └── api/auth/
│   │       ├── session/
│   │       │   ├── route.ts
│   │       │   └── route.integration.test.ts  # integration: POST/GET, iron-session, all error branches
│   │       ├── discover/
│   │       │   ├── route.ts
│   │       │   └── route.integration.test.ts  # integration: proxy + error mapping
│   │       └── logout/
│   │           ├── route.ts
│   │           └── route.integration.test.ts  # integration: session destruction + redirect
│   ├── components/
│   │   ├── health-status.tsx
│   │   ├── health-status.test.tsx             # unit: loading/ok/error render with prop injection
│   │   ├── health-status.integration.test.tsx # integration: real fetch intercepted by MSW
│   │   ├── logout-button.tsx
│   │   └── logout-button.test.tsx             # unit: renders, fires click handler
│   ├── server/
│   │   ├── client.ts
│   │   └── client.integration.test.ts         # integration: ApiError parsing, timeout, URL construction
│   ├── lib/routing/
│   │   ├── api-routes.ts
│   │   ├── api-routes.test.ts                 # unit: route builder return values (pure functions)
│   │   ├── routes.ts
│   │   └── routes.test.ts                     # unit: isSafeRedirectPath (security-relevant, pure)
│   └── packages/terra/src/components/
│       └── button/
│           ├── button.tsx
│           └── button.test.tsx                # unit/component: variants, sizes, asChild, fullWidth
├── vitest.config.unit.ts                     # App unit tier: jsdom, server-only shim, @orbit/test-utils alias, excludes *.integration.* and src/packages/**
├── vitest.config.integration.ts              # App integration tier: same aliases + setupFiles, includes only *.integration.*, excludes src/packages/**
└── playwright.config.ts
```

**pnpm scripts (`frontend/package.json`):**
```json
"test:unit":        "vitest run --config vitest.config.unit.ts",
"test:integration": "vitest run --config vitest.config.integration.ts",
"test":             "pnpm run test:unit && pnpm run test:integration"
```

> **Why two config files, not `--include`/`--exclude` flags?** Vitest's `--include` CLI flag is *additive* against the default include glob — it does not *replace* the default, so `--include '**/*.integration.test.ts?(x)'` causes all tests to run. Using a dedicated config per tier, each with its own `include` glob, is the correct mechanism for mutually exclusive tier selection.

**Terra pnpm scripts (`frontend/src/packages/terra/package.json`):**
```json
"test:unit": "vitest run",
"test":      "pnpm run test:unit"
```
Terra has no integration tests (it is a pure rendering library with no HTTP calls).

**Makefile targets:**
```makefile
# Run only Terra's component tests (happy-dom, Terra's own vitest.config.ts)
frontend-test-terra:
	cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra run test:unit

# Run all unit tests: Terra first, then the app (jsdom, vitest.config.unit.ts)
# App config excludes src/packages/** so Terra tests never run under jsdom
frontend-test-unit:
	cd $(FRONTEND_DIR) && pnpm --filter @orbit/terra run test:unit
	cd $(FRONTEND_DIR) && pnpm run test:unit

frontend-test-integration:
	cd $(FRONTEND_DIR) && pnpm run test:integration

frontend-test-e2e:
	cd $(FRONTEND_DIR) && pnpm exec playwright test

frontend-test: frontend-test-unit frontend-test-integration frontend-test-e2e
```

> **Note:** `frontend-test-terra` is the isolation target — use it when working exclusively on Terra components. `frontend-test-unit` always runs Terra first, then the app. The two Makefile lines in `frontend-test-unit` are both necessary: `pnpm run test:unit` at the workspace root does not recurse into workspace packages. The app's `vitest.config.unit.ts` excludes `src/packages/**` to prevent Terra's tests from running a second time under `jsdom`.

### Glossary

- **BFF (Backend for Frontend)**: The Next.js API routes (`src/app/api/`) that act as a server-side proxy between the browser and the Go control plane. The browser never calls the Go backend directly.
- **Category A E2E**: Playwright tests in `frontend/e2e/` where the Go backend is mocked via `page.route()`. Require only a built Next.js app.
- **Category B E2E**: System-level tests that would exercise the full stack (Next.js + Go backend + Postgres + Redis). Explicitly out of scope for this PRD. No directory, no Playwright config, and no CI step exists for them. A dedicated ADR is required before any work begins.
- **Tier**: A category of test distinguished by its scope, speed, and execution environment (unit, component, integration, E2E).
- **`test-utils/`**: The canonical location for all shared test infrastructure in the app — MSW server, handlers, factory functions, and `renderWithProviders`. Not a workspace package.
- **Workspace package**: A self-contained npm package within the pnpm workspace, declared in `frontend/pnpm-workspace.yaml`. Currently: `@orbit/terra`.
- **`server-only` shim**: An empty module substituted via Vitest's `resolve.alias` to bypass the Next.js `server-only` runtime guard in the test environment. Has no effect on production builds.
- **MSW (Mock Service Worker)**: A library that intercepts HTTP requests at the `fetch` layer. In Node.js (Vitest), it uses HTTP interceptors via `setupServer()`. In the browser (Playwright), it uses a Service Worker. The same handler definitions are reused in both environments.
- **`renderWithProviders()`**: The single render function used in all component tests. Wraps RTL's `render` with all required React contexts and MSW server lifecycle management.
- **State over mocking**: The principle that integration tests should manipulate real application state (via MSW HTTP interception or context/query-cache seeding) rather than replacing modules with `vi.mock()`.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______

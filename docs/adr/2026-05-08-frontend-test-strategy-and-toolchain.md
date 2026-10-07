# Frontend Test Strategy and Toolchain

**Status:** Accepted
**Date:** 2026-05-08
**Deciders:** Engineering Team
**Technical Story:** [Frontend Test Strategy PRD](../prd/frontend-test-strategy.md) -- establishing a layered, package-aware test architecture for the kora frontend.

---

## Context

The kora frontend has zero test infrastructure. No `vitest.config.ts`, `jest.config.ts`, `playwright.config.ts`, or test file exists anywhere in the frontend tree. No `test` script exists in any `package.json`. The CI pipeline has no test step.

The codebase is not a blank slate -- it is a production-intended frontend with two distinct layers of complexity that any test strategy must accommodate.

**The Terra design system (`@orbit/terra`)** is a fully implemented workspace package at `frontend/src/packages/terra/`. It has 21+ component families built on Radix UI primitives, a semantic token system expressed via Tailwind v4 `@theme` blocks, and Storybook stories for every component. It already has `vite@6.3.5` and `@vitejs/plugin-react@4.5.2` installed as dev dependencies. It has no unit or component tests.

**The Next.js 16 App Router application** (`frontend/src/app/`) has a working OIDC auth flow (PKCE, HRD discover, iron-session cookie sealing), a dashboard shell with page-scoped colocated components under `_components/` directories, shared app-level components at `frontend/src/components/`, a Backend-for-Frontend (BFF) layer at `frontend/src/server/` that proxies requests to the Go backend, and BFF route handlers under `frontend/src/app/api/`. All server-side modules are guarded by `import "server-only"`. There are no tests.

The actual directory structure that the test strategy must accommodate:

```
frontend/src/
├── app/
│   ├── (auth)/login/_components/     # page-scoped: EmailSignInForm, AlreadySignedIn
│   ├── (dashboard)/_components/      # page-scoped: DashboardSidebar, DashboardHeader
│   └── api/auth/                     # BFF routes: session, discover, logout
├── components/                       # shared app components: HealthStatus, LogoutButton
├── lib/
│   ├── oidc-client.ts                # buildUserManager, TenantOidcConfig
│   └── routing/                      # ApiRoute, isSafeRedirectPath
├── packages/terra/                   # @orbit/terra workspace package
├── server/                           # server-only: client.ts, auth-config.ts, session.ts
└── shared/                           # HealthResponse and other shared types
```

Note: there is no `_core/` directory. Shared components live in `src/components/`, routing utilities in `src/lib/routing/`, and server-only modules in `src/server/`.

### Background

kora is structured as a pnpm workspace. `frontend/pnpm-workspace.yaml` declares `packages: ['src/packages/*']`, which means Terra is an independent installable package (`@orbit/terra`) consumed by the app via a workspace link. The monorepo's Go backend runs as a separate process; the only interface between the frontend and the backend is the BFF layer at `src/server/client.ts` (`orbitClient`) and the `/api/*` route handlers.

[frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) (frontend scaffolding) established the Next.js 16 App Router application structure. [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) (Terra design system) established the workspace package isolation model, the Tailwind v4 token architecture, and the Storybook-based development workflow. This ADR establishes the test layer that sits alongside those decisions.

### Problem Statement

We need a set of architectural decisions that establish:

1. Which test runner(s) to use across the workspace, and why
2. How Terra's tests are isolated from the Next.js app
3. How app-level tests handle the `server-only` guard and App Router APIs
4. How BFF route handlers are tested without a live Go backend, and which parts of the pipeline are exercised
5. How E2E tests run without a live Go backend or Docker Compose stack
6. Where E2E tests live in a monorepo that contains frontend, BFF, and Go backend
7. What DOM environment each layer uses
8. What the coverage model is, and whether thresholds block CI
9. Where shared mocking utilities live, and how they are kept in sync with real interfaces
10. Whether snapshot testing is adopted

### Goals

- Establish a layered, package-aware test architecture covering unit, component, integration, and E2E tiers
- Give Terra its own isolated test suite that runs with no dependency on the Next.js app
- Enable BFF route handler tests that exercise the full handler and `orbitClient` pipeline without a live backend
- Prioritise real application state over module mocking in integration tests -- intercept at the HTTP boundary, not the module boundary
- Centralise all shared mocking utilities in a single, typed `test-utils/` directory
- Define a CI execution model rooted in a single `make` target at the repo root
- Produce coverage reports for both Terra and the app without blocking CI on threshold failures

### Non-Goals

- Replacing Storybook as the component development and visual documentation environment
- Enforcing coverage thresholds as CI failure conditions (per-route mandatory rules are the enforceable gate)
- Running the Go backend or any Docker Compose service during frontend tests
- Snapshot testing for component output (explicitly rejected -- see Considered Options)
- Full-stack system tests of any kind (explicitly out of scope; require a dedicated ADR before any work begins)

---

## Decision Drivers

* **Vite alignment**: Terra already uses Vite as its build system and Storybook bundler. Adding a test runner that shares Vite's module resolution (aliases, ESM, plugin pipeline) eliminates a second transform stack and makes test configuration predictable.
* **ESM-first codebase**: The entire frontend is written in modern ESM. The codebase includes ESM-only packages (`oidc-client-ts`). Any test runner must support ESM natively without experimental flags.
* **`server-only` boundary**: Modules under `src/server/` and `src/app/api/` import `server-only` at the top level. This package throws at import time in any non-React-Server-Components environment. Tests must neutralise this without altering production code.
* **Package isolation**: Terra must be testable without running Next.js. The workspace model requires each package to carry its own test config and run independently.
* **State over mocking**: Integration tests should exercise the real application code path -- real `fetch` calls, real response parsing, real `useState` transitions -- and intercept only at the HTTP boundary. Module-level mocking of `orbitClient` would leave the most security-relevant behaviors (`ApiError` parsing, `AbortSignal` timeout, URL construction) untested.
* **Centralised mocking**: Ad-hoc inline mocks are not acceptable. All MSW handlers and factory functions must live in one canonical location, typed against real application interfaces, so that interface changes surface stale mocks at compile time.
* **No live backend in tests**: The Go backend is a separate process. Coupling frontend tests to a live backend makes them slow, order-dependent, and fragile on CI.
* **Makefile-rooted execution**: The repo's developer interface is `make`. Adding frontend tests means wiring them into the existing `make check` target, not requiring developers to remember a separate invocation.
* **Developer experience**: Full Terra test suite in under 10 seconds cold start; full app integration suite in under 30 seconds cold start; watch mode re-run on a single file in under 3 seconds.

---

## Considered Options

### Option 1: Jest

**Description:** Use Jest as the test runner for both Terra and the app.

**Pros:**
- Widely known; most React documentation examples use Jest
- Rich ecosystem of Jest-specific matchers and utilities
- Mature snapshot testing support (though snapshot testing is rejected -- see Option 5)

**Cons:**
- Terra already uses Vite as its build system; introducing Jest requires a separate Babel or SWC transform pipeline running alongside the existing Vite pipeline -- two transform stacks for the same code
- Jest's ESM support is still experimental and requires `--experimental-vm-modules`; the codebase is fully ESM and imports ESM-only packages (`oidc-client-ts`)
- Path aliases (`@orbit/terra`, `@orbit/test-utils`, `@/server/`) work automatically in Vitest because it shares Vite's resolver; Jest requires a separate `moduleNameMapper` that must be kept in sync with `tsconfig.json` and `vite.config.ts`
- No benefit from Vite's HMR-aware test reruns; watch mode is slower for a Vite-based project

### Option 2: Storybook interaction tests as the sole component test strategy

**Description:** Use `@storybook/addon-interactions` (already installed in Terra) to write component tests within Storybook stories, relying on `storybook test` in CI rather than a dedicated test runner.

**Pros:**
- No new tooling to install in Terra; `addon-interactions` is already present
- Stories and tests are colocated -- the documentation and the specification are the same file
- Visual feedback in the Storybook UI during development

**Cons:**
- Storybook interaction tests run in a browser-rendered environment; significantly slower than jsdom/happy-dom for large test suites
- Cannot test BFF route handlers, server-only modules, or anything outside the component rendering context
- `storybook test` CI integration is more complex to configure and cache than a standard `vitest run`
- Does not address the app tier, BFF tier, or E2E tier -- a second strategy would still be required

### Option 3: Cypress instead of Playwright for E2E

**Description:** Use Cypress as the E2E test runner instead of Playwright.

**Pros:**
- Mature ecosystem; well-documented
- Component testing mode (though Vitest already covers this)

**Cons:**
- The auth flow crosses origins: `EmailSignInForm` redirects to an external IdP (`issuer_url`), then the IdP redirects back to `/auth/callback`. Cypress's same-origin policy enforcement has historically made testing OIDC redirect flows unreliable, even with `chromeWebSecurity: false`
- Playwright handles cross-origin navigation natively -- a critical requirement for any app with PKCE OIDC
- Playwright is Next.js's official E2E recommendation
- Playwright's `page.route()` API for mocking outbound BFF calls is well-suited to the "mock Go backend at the network level" requirement

### Option 4: Vitest + React Testing Library + Playwright (chosen)

**Description:** Use Vitest as the unified test runner for unit and component tests across Terra and the app, React Testing Library (RTL) for component rendering, and Playwright for E2E tests. Terra carries its own `vitest.config.ts`; the app uses two configs (`vitest.config.unit.ts` and `vitest.config.integration.ts`) for mutually exclusive tier selection. Both app configs include the `server-only` shim alias, the `@orbit/test-utils` path alias, and `setupFiles`. MSW is used from Phase 1 for BFF route and component integration tests. Playwright mocks the Go backend via route interception.

**Pros:**
- Vitest shares Vite's module resolver -- all `tsconfig.json` path aliases work automatically with zero additional configuration in both Terra and the app
- Native ESM support; no experimental flags required; ESM-only dependencies (`oidc-client-ts`) resolve without configuration
- Terra's existing `vite.config.ts` can be extended directly by Vitest
- `@testing-library/react` is the community standard for testing React components; encourages testing from the user's perspective
- MSW from Phase 1 means `orbitClient`'s full pipeline (URL construction, `AbortSignal` timeout, `ApiError` parsing) is exercised in BFF route tests -- not just the handler dispatch logic
- The `server-only` shim pattern is well-documented and requires only a one-line alias in Vitest config
- Playwright handles cross-origin OIDC flows natively; its `page.route()` API directly supports the "mock Go backend at the network level" pattern
- The `test-utils/` directory with typed handlers and factory functions provides a single canonical mocking location; TypeScript surfaces stale mocks when interfaces change

**Cons:**
- Three Vitest config files (Terra: one; app: two split by tier — `vitest.config.unit.ts` and `vitest.config.integration.ts`) — shared setup changes may need to be applied to both app configs; mitigated by extracting shared options into a common base object
- Playwright adds CI overhead (~80 MB browser binaries) and requires dedicated cache configuration
- The `server-only` shim means that in tests, modules guarded by `import "server-only"` behave as if the guard is absent — must be documented clearly

### Option 5: Snapshot testing for component output

**Description:** Use Vitest's snapshot testing (`.toMatchSnapshot()` or `.toMatchInlineSnapshot()`) to pin component rendered output as a regression guard.

**Pros:**
- Low effort to write initial snapshots
- Catches unintentional visual structure changes

**Cons:**
- Terra components are built with CVA. Snapshots capture full resolved className strings (`inline-flex items-center ... bg-accent text-accent-foreground hover:bg-accent-hover`) that are fragile against token renames, Tailwind version upgrades, and CVA internal changes. Every such change triggers a snapshot diff with no behavioral signal.
- In an AI-assisted workflow (Forge), routine component regeneration produces snapshot failures that developers learn to resolve with `--updateSnapshot` without reading -- defeating the purpose entirely.
- Any value worth pinning (e.g., the string returned by `ApiRoute.session.GET()`) is more clearly expressed as an explicit `toEqual` assertion that reads as a specification rather than as a file diff.

**Decision: Snapshot testing is not adopted.** File-based snapshots for component output are explicitly rejected. Inline snapshots for compact, discrete values are permissible but should be rare.

### Option 6: Module-level mocking for all integration tests

**Description:** Use `vi.mock('@/server/client')` to replace `orbitClient` in BFF route tests, and mock `fetch` globally for component integration tests.

**Pros:**
- Simple to set up; no MSW server lifecycle required
- Vitest's `vi.mock()` is familiar to developers coming from Jest

**Cons:**
- Mocking `orbitClient` at the module level means `client.ts`'s request pipeline -- `buildUrl`, `AbortSignal.timeout(10_000)`, `ApiError` parsing -- is never exercised in tests. These are the most security-relevant behaviors in the BFF layer.
- Global `fetch` mocking is unpredictable and doesn't compose well with multiple concurrent requests in complex component tests.
- MSW intercepts at the HTTP boundary, allowing the actual application code to run while serving controlled responses. This is strictly more faithful to production behavior.
- The `test-utils/` typed handler model means interface changes are caught at compile time; inline `vi.mock()` definitions are untyped and diverge silently.

**Decision: Module-level mocking is used only where unavoidable** -- specifically `next/navigation` hooks (`usePathname`, `useRouter`) and `oidc-client-ts` (`signinRedirect()` triggers real browser navigation that cannot run in jsdom). All HTTP-layer concerns use MSW.

---

## Decision

**Chosen Option:** Option 4 -- Vitest + React Testing Library + Playwright

### Rationale

The core constraint driving this decision is that Terra already lives in a Vite-based ecosystem. Adding any test runner other than Vitest would require maintaining a second module resolution configuration in sync with `vite.config.ts` and `tsconfig.json` indefinitely.

MSW is elevated to Phase 1 (not deferred to Phase 2 as initially proposed) because the whole point of BFF route tests is to verify the full handler + `orbitClient` + error-mapping pipeline. Testing only the handler dispatch with a mocked `orbitClient` leaves the most security-relevant logic untested. MSW requires minimal Phase 1 investment and the test-utils infrastructure it requires (the typed handler files) is needed regardless.

Playwright is the natural choice for E2E because it handles cross-origin OIDC flows natively -- a non-negotiable requirement for this codebase -- and its `page.route()` API is designed for the "mock the backend at the network level" pattern.

Snapshot testing is explicitly rejected. The rationale is documented in Option 5 above.

Module-level mocking via `vi.mock()` is restricted to cases where HTTP interception is structurally impossible -- browser navigation APIs and external OIDC SDK internals.

---

## Consequences

### Positive

- Terra gains a fast, isolated test suite with no dependency on Next.js; CI can run Terra tests in parallel with app tests
- BFF route handlers are fully testable in isolation with `orbitClient`'s real pipeline exercised via MSW; error-mapping, header injection, status code handling, and timeout behavior are all covered
- Shared `test-utils/` with typed factory functions means a single interface change in `SessionPayload` or `HealthResponse` immediately surfaces stale test data at compile time
- Playwright's cross-origin support means OIDC auth flow E2E tests are structurally possible without browser security flag workarounds
- The entire frontend test suite integrates into the existing `make check` workflow
- Coverage reports for both packages are produced and uploaded as CI artifacts, making coverage drift visible without blocking the build

### Negative

- Three Vitest config files exist across the workspace (Terra: one; app: two split by tier -- `vitest.config.unit.ts` and `vitest.config.integration.ts`); changes to shared Vitest version, global matchers setup, or alias blocks may need to be applied in multiple places
- Playwright browser binaries add CI download time and must be cached explicitly
- The `server-only` shim is a deliberate deviation from production behavior that must be documented clearly
- MSW in Phase 1 (not Phase 2) adds slightly more initial setup; mitigated by the `test-utils/` infrastructure serving all future tests

### Neutral

- Storybook's role is unchanged; it remains the visual development environment for Terra and is not part of the test execution pipeline in Phase 1
- Coverage thresholds (Terra ≥ 30%, App ≥ 15% for Phase 1) are reported but do not block CI; per-route mandatory test rules are the enforceable gate
- Full-stack system tests (Category B) are explicitly out of scope and will not block Phase 1/2 progress; if ever needed, a dedicated ADR will define them

---

## Implementation

### Four-Tier Test Architecture

| Tier | Scope | Runner | Environment | File suffix | Location |
|------|-------|--------|-------------|-------------|----------|
| Unit | Pure TypeScript logic: `lib/routing/`, `shared/` | Vitest | Node | `*.test.ts` | Colocated with source |
| Component | Terra components | Vitest + RTL | happy-dom | `*.test.tsx` | Colocated within Terra |
| Integration | BFF route handlers, shared components, page-scoped components | Vitest + RTL + MSW | jsdom + `server-only` shim | `*.integration.test.ts(x)` | Colocated with source |
| E2E (Category A) | Critical user flows, Go backend mocked | Playwright | Chromium | `*.spec.ts` | `frontend/e2e/` |
| E2E (Category B) | Full-stack — **explicitly out of scope** | — | — | — | — |

### Test File Placement Rules

These are binding conventions, not preferences. PRs that deviate require explicit justification.

#### Unit tests: colocated, `*.test.ts(x)` suffix

Unit tests live in the same directory as the source file they test and use the `*.test.ts` / `*.test.tsx` suffix. They contain no MSW usage, no imports from `@orbit/test-utils/msw` or `@orbit/test-utils/factories`, and no network activity of any kind.

```
src/lib/routing/api-routes.ts
  └─ src/lib/routing/api-routes.test.ts              ✓

src/components/health-status.tsx
  └─ src/components/health-status.test.tsx           ✓  (renders with prop injection only)

src/app/(dashboard)/_components/dashboard-sidebar.tsx
  └─ src/app/(dashboard)/_components/dashboard-sidebar.test.tsx   ✓  (props only)
```

#### Integration tests: colocated, `*.integration.test.ts(x)` suffix

Integration tests live in the same directory as the source file they test and use the `*.integration.test.ts` / `*.integration.test.tsx` suffix. They must use MSW (`@orbit/test-utils/msw`) and/or typed factories (`@orbit/test-utils/factories`). The suffix makes them mechanically selectable via the `include` glob in separate Vitest config files (`vitest.config.unit.ts` and `vitest.config.integration.ts`), enabling separate `test:unit` and `test:integration` pnpm scripts. Note: Vitest's `--include`/`--exclude` CLI flags are additive against the default glob — they cannot exclusively select one tier. Two config files are required.

```
src/app/api/auth/session/route.ts
  └─ src/app/api/auth/session/route.integration.test.ts      ✓

src/components/health-status.tsx
  └─ src/components/health-status.integration.test.tsx       ✓  (MSW-backed fetch)

src/app/(dashboard)/_components/dashboard-sidebar.tsx
  └─ src/app/(dashboard)/_components/dashboard-sidebar.integration.test.tsx   ✓
```

There are no `__tests__/` subdirectories, no `tests/` directories at any level, and no top-level `integration/` folder. The suffix is the only structural distinction between tiers.

#### Terra component tests: colocated within the package

Terra component tests follow the same colocation rule within the Terra package:

```
src/packages/terra/src/components/button/button.tsx
  └─ src/packages/terra/src/components/button/button.test.tsx    ✓
```

Terra tests never import from `src/test-utils/` and never reference app-level paths.

#### E2E tests: `frontend/e2e/`

Playwright tests are never colocated with source files. All Category A E2E tests (those that require only a running Next.js app, with the Go backend mocked) live under `frontend/e2e/`, organised by the user-facing flow they exercise. The `.spec.ts` extension distinguishes them from Vitest tests at a glance.

```
frontend/e2e/
├── auth/
│   └── login.spec.ts          # HRD form → /api/auth/discover mock → redirect
└── dashboard/
    └── health.spec.ts         # page load, health status display (Go backend mocked)
```

**Rule:** a file in `frontend/e2e/` must never require the Go backend, Postgres, or Redis to be running. If it does, it is a Category B test and does not belong here.

#### Full-stack system tests: explicitly out of scope

Tests that require the Go backend, Postgres, or Redis are not frontend tests and are not addressed by this ADR. No files for them should be created anywhere in the repo as part of this work. They would require a dedicated ADR covering infrastructure, ownership, and CI strategy. The repo-root `e2e/` directory does not exist and must not be created until that ADR is approved.

#### Shared test infrastructure: `src/test-utils/`

Helpers, factories, MSW handlers, and `renderWithProviders` live in `frontend/src/test-utils/`. Test files import from `@orbit/test-utils` (the path alias). No test infrastructure is duplicated inline. No `.test.ts` or `.spec.ts` files live inside `test-utils/`.

#### Summary

| Test type | Suffix | Location | pnpm script |
|-----------|--------|----------|-------------|
| Unit | `.test.ts(x)` | Colocated with source | `test:unit` |
| Component (Terra) | `.test.tsx` | Colocated within Terra | `test:unit` |
| Integration | `.integration.test.ts(x)` | Colocated with source | `test:integration` |
| E2E — Category A | `.spec.ts` | `frontend/e2e/` | `test:e2e` |
| E2E — Category B | — | **out of scope** — requires dedicated ADR | n/a |
| Shared test infrastructure | n/a | `frontend/src/test-utils/` | n/a |

### Package Isolation

Terra's `vitest.config.ts` extends the existing `vite.config.ts` using `mergeConfig` from `vitest/config` and uses `happy-dom` as the DOM environment — lightweight and sufficient for straightforward component rendering without the full browser API surface. Terra has only one test tier (component), so a single config file is sufficient.

The app uses two config files: `vitest.config.unit.ts` and `vitest.config.integration.ts`. Both use `jsdom`, which has better compatibility with Next.js internals and the `NextResponse` API. Both share the same `resolve.alias` block (`server-only` shim + `@orbit/test-utils`) and `setupFiles: ['./src/test-utils/setup.ts']`.

No config is shared between Terra and the app. Terra's config must never reference app-level paths; the app's configs must never reach into Terra source (they consume Terra as a package import via the workspace link).

#### `happy-dom` API gaps and Radix UI

Terra's 21+ component families include several Radix UI primitives (Popover, Tooltip, Dialog, DropdownMenu, etc.) that rely on browser APIs `happy-dom` does not implement:

| Missing API | Affected Radix primitives |
|---|---|
| `ResizeObserver` | Popover, Tooltip, DropdownMenu (position recalculation) |
| `PointerEvent` | All interactive primitives (open/close interaction) |
| `window.matchMedia` | Any component that reads prefers-reduced-motion |
| `DOMRect` / `getBoundingClientRect` | Floating positioning |

For any Terra component test that exercises open/close or positioning behaviour on a Radix primitive, switch the environment per-file using Vitest's docblock annotation:

```ts
// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
```

This should be the exception, not the rule. Phase 1 mandatory components (Button, Badge, Typography) are not Radix-based and run cleanly under `happy-dom`. Document the annotation requirement in the test file with a comment explaining which API gap necessitates it.

### `server-only` Shim

Both app Vitest configs (`vitest.config.unit.ts` and `vitest.config.integration.ts`) must include the following alias block. It must appear in **both** because each is invoked independently via `--config`:

```ts
resolve: {
  alias: {
    'server-only': './src/test-utils/server-only-shim.js',
    '@orbit/test-utils': path.resolve(__dirname, './src/test-utils'),
  },
},
```

Both configs also require:

```ts
setupFiles: ['./src/test-utils/setup.ts'],
```

Without `setupFiles`, `@testing-library/jest-dom` matchers are not registered and the MSW server lifecycle (`beforeAll`/`afterEach`/`afterAll`) never runs.

Where `src/test-utils/server-only-shim.js` is:

```js
// Intentional no-op. The server-only guard is a runtime protection against
// client-bundle inclusion. In the test environment, we control what code runs —
// the shim is safe and does not weaken any production security boundary.
// When a new server-only module is added, this alias handles it automatically
// with no per-module configuration required.
```

These aliases are scoped to the Vitest configs only and have no effect on the Next.js production build.

### Centralised Mocking: `test-utils/`

All shared test infrastructure lives in `frontend/src/test-utils/`. This is **not** a workspace package -- it has no `package.json`, is never published, and is imported exclusively via the `@orbit/test-utils` path alias. It must be excluded from the Next.js production build (add to `tsconfig.json` `exclude` array).

```
frontend/src/test-utils/
├── index.ts                       # public re-exports for renderWithProviders, factories, server
├── render.tsx                     # renderWithProviders() — RTL render + all React contexts
├── setup.ts                       # jest-dom matchers; beforeAll/afterEach/afterAll MSW lifecycle
│                                  #   beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
│                                  #   afterEach(() => server.resetHandlers())
│                                  #   afterAll(() => server.close())
├── server-only-shim.js            # empty module for the server-only alias
├── msw/
│   ├── server.ts                  # setupServer(...defaultHandlers) — no options; options go in listen()
│   └── handlers/
│       ├── index.ts               # aggregates all domain handlers (used as the default handler set)
│       ├── auth.ts                # /api/auth/session (GET + POST), /api/auth/discover, /api/auth/logout
│       └── health.ts              # /api/identity/health
└── factories/
    ├── session.ts                 # createSessionUser(), createSessionPayload(), createBackendSessionResponse()
    └── health.ts                  # createHealthResponse()
```

**Handler typing:** Every handler in `auth.ts` uses the real response interfaces from `src/server/auth-config.ts` and `src/shared/api.ts`. Factory functions have typed parameters with sensible defaults:

```ts
// factories/session.ts
import type { SessionUser, SessionPayload } from '@/server/auth-config';

export function createSessionUser(overrides?: Partial<SessionUser>): SessionUser {
  return { sub: 'usr_test', email: 'test@example.com', name: 'Test User', ...overrides };
}
```

When `SessionUser` gains a new required field, TypeScript immediately surfaces every factory call site that is missing the field. Stale mocks are caught at compile time, not at runtime.

**`renderWithProviders()`:** This is the only render function used in app-level component tests. Individual test files never import `render` directly from `@testing-library/react`.

```tsx
// render.tsx (sketch — add providers as the app acquires them)
import { render, type RenderOptions } from '@testing-library/react';

export function renderWithProviders(ui: React.ReactElement, options?: RenderOptions) {
  // Wrap with future React contexts here (session context, React Query, etc.)
  // as the app architecture evolves. Tests call this function unchanged.
  return render(ui, options);
}
```

**Terra does not use `test-utils/`.** Terra tests are isolated component behavior tests that construct any needed props inline. The `test-utils/` directory is app-level only.

### E2E: Two Categories

**Category A — Frontend-isolated** (`frontend/e2e/`):

Playwright tests where the Go backend is entirely mocked via `page.route()`. The full Next.js routing, middleware, iron-session pipeline, and BFF logic are exercised. Only outbound calls from the BFF to the Go backend are intercepted.

```ts
// frontend/e2e/dashboard/health.spec.ts
test('health status renders as healthy', async ({ page }) => {
  await page.route('**/api/identity/health', route =>
    route.fulfill({ status: 200, json: { status: 'ok', service: 'identity' } })
  );
  await page.goto('/');
  await expect(page.getByText('ok')).toBeVisible();
});
```

The route pattern `**/api/identity/health` matches the BFF route handler at `/api/identity/health` as called from the Next.js server during SSR or from the `HealthStatus` component's client-side fetch. The `**/` prefix matches regardless of origin.

Category A tests require only a built Next.js app (`next build && next start`). No Go backend, no Docker Compose.

**Category B — Full-stack (out of scope):**

Tests that exercise the real Go backend, Postgres, or Redis are not frontend tests and are explicitly out of scope for this ADR. No infrastructure, no directory, and no CI step should be created for them as part of this work. If full-stack system tests are ever warranted, they will be addressed by a dedicated ADR covering ownership, infrastructure, and CI model.

### No Real HTTP Calls in Any Test

This is a hard rule enforced at two levels:

**Vitest (unit + integration):** The MSW server in `test-utils/msw/server.ts` is created with `setupServer(...defaultHandlers)` — no options. The `onUnhandledRequest: 'error'` option is passed to `server.listen()` in `setup.ts`, which Vitest loads via `setupFiles`:

```ts
// test-utils/msw/server.ts
export const server = setupServer(...defaultHandlers);

// test-utils/setup.ts
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
```

`setupServer()` in MSW v2 accepts only `RequestHandler[]` — passing an options object there is silently ignored. The `onUnhandledRequest` option is only honoured when passed to `server.listen()`. Any `fetch` call that is not matched by a registered MSW handler causes the test to throw immediately and fail. There is no "pass silently and hit a real server" failure mode.

**Playwright (Category A E2E):** A global `beforeEach` fixture in `frontend/e2e/fixtures.ts` registers a catch-all `page.route()` handler that returns a `503` response for any URL not explicitly intercepted by the test:

```ts
// e2e/fixtures.ts
import { test as base } from '@playwright/test';

export const test = base.extend({
  page: async ({ page }, use) => {
    // Block any outbound request not explicitly registered in the test.
    // Returns 503 so the test fails fast rather than hanging or hitting a live service.
    await page.route('**/*', route => route.fulfill({ status: 503, body: 'Blocked by test fixture' }));
    await use(page);
  },
});
```

Individual tests register their own `page.route()` handlers before calling `page.goto()`. Because Playwright applies route handlers in registration order and the catch-all is registered first, more-specific handlers registered by the test take precedence. Anything that escapes the test-specific handlers is blocked with a 503. The Go backend, Postgres, Redis, and all external APIs must never be reachable from a test process.

Module-level mocking (`vi.mock()`) is the only permitted alternative and is reserved strictly for cases where HTTP interception is structurally impossible: `next/navigation` hooks (`usePathname`, `useRouter`) and `oidc-client-ts` internals (`signinRedirect()` causes a real browser navigation that cannot run in jsdom).

### Makefile Integration

Four granular targets are added to the root `Makefile`, plus a combined convenience target:

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

frontend-test: frontend-test-unit frontend-test-integration
```

The existing `frontend-check` target gains `frontend-test` as a dependency:

```makefile
frontend-check: frontend-lint frontend-type-check frontend-fmt frontend-test
```

`make check` therefore gates on all frontend test tiers without requiring any additional developer commands. `frontend-test-e2e` is a Phase 2 target — it is not added until Playwright is installed and `frontend/e2e/` is populated.

**pnpm scripts** (in `frontend/package.json`):

```json
"test:unit":        "vitest run --config vitest.config.unit.ts",
"test:integration": "vitest run --config vitest.config.integration.ts",
"test":             "pnpm run test:unit && pnpm run test:integration"
```

> **Why two config files, not `--include`/`--exclude` flags?** Vitest's `--include` CLI flag is additive: it appends to the default glob rather than replacing it. Passing `--include '**/*.integration.test.ts?(x)'` still runs all non-integration tests as well. The only reliable way to select exactly one tier is to define the `include` array inside a dedicated config file. `vitest.config.unit.ts` includes `**/*.test.ts?(x)` and excludes `**/*.integration.test.ts?(x)` and `src/packages/**`; `vitest.config.integration.ts` includes only `**/*.integration.test.ts?(x)` and also excludes `src/packages/**`. The `src/packages/**` exclusion is mandatory in both app configs — without it the bare glob would match Terra's test files and run them a second time under `jsdom` with the wrong Vite config. The `--config` flag accepts either config without ambiguity.

**Terra pnpm scripts** (in `frontend/src/packages/terra/package.json`):

```json
"test:unit": "vitest run",
"test":      "pnpm run test:unit"
```

Terra has no integration tests — it is a pure rendering library with no HTTP calls. Its `test` script is an alias for `test:unit`. CI uses the explicit Makefile targets (`make frontend-test-terra`, `make frontend-test-unit`, `make frontend-test-integration`) — never a single workspace-wide command. `make frontend-test-terra` is also the isolation target for local development when working exclusively on Terra components.

The Makefile targets are separate CI steps. A unit test failure does not suppress integration test results. Each tier's outcome is independently attributable in CI logs.

### Coverage Model

Coverage is collected via `@vitest/coverage-v8` (V8's built-in coverage instrumentation -- no Babel transform required):

```ts
// vitest.config.unit.ts and vitest.config.integration.ts (both app configs)
coverage: {
  provider: 'v8',
  reporter: ['text', 'lcov', 'html'],
  exclude: [
    'src/test-utils/**',
    '**/*.test.{ts,tsx}',
    '**/*.integration.test.{ts,tsx}',
    '**/*.stories.tsx',
  ],
  // No thresholds: coverage is reported, never enforced as a CI failure
}
```

**Phase targets (reporting-only, never CI failure conditions):**

| Phase | Terra | App |
|-------|-------|-----|
| Phase 1 | ≥ 30% statements | ≥ 15% statements |
| Phase 2 | ≥ 50% statements | ≥ 30% statements |
| Phase 3 | ≥ 70% statements | ≥ 45% statements |

**The enforceable gate is per-route, not percentage-based:**
- Every BFF route handler (`src/app/api/**/route.ts`) must have: one happy-path test, one test per mapped error condition.
- Every pure utility (`lib/routing/api-routes.ts`, `lib/routing/routes.ts`) must have 100% path coverage.
- Every new Terra component must ship with at least one rendering test before merge.

These are enforced by PR convention and review, not by a CI threshold.

### Unavoidable Module Mocks

Two module mocks are unavoidable and are permissible:

**`next/navigation`** -- `usePathname()` and `useRouter()` cannot be called outside a Next.js render tree. Mock at the module level in any test for a component that uses them:

```ts
vi.mock('next/navigation', () => ({
  usePathname: vi.fn(() => '/'),
  useRouter: vi.fn(() => ({ replace: vi.fn() })),
}));
```

**`@orbit/lib/oidc-client`** -- `buildUserManager().signinRedirect()` triggers a real browser navigation that `jsdom` cannot execute. Mock the `buildUserManager` factory; test everything around it (the HRD fetch, sessionStorage write, and error branches):

```ts
vi.mock('@orbit/lib/oidc-client', () => ({
  buildUserManager: vi.fn(() => ({
    signinRedirect: vi.fn(),
    signinRedirectCallback: vi.fn(),
  })),
  TENANT_CONFIG_STORAGE_KEY: 'orbit_tenant_config',
}));
```

All other mocking uses MSW handlers from `test-utils/`.

### Migration Path

**Phase 1 (Foundation):**

1. Create `frontend/src/test-utils/` with: `server-only-shim.js`, `setup.ts` (jest-dom matchers + MSW lifecycle via `server.listen({ onUnhandledRequest: 'error' })`), `render.tsx`, `msw/server.ts` (`setupServer(...defaultHandlers)` — no options here), `msw/handlers/` (auth + health), `factories/` (session + health), `index.ts`
2. Add `vitest`, `@vitest/coverage-v8`, `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom`, `happy-dom` to Terra dev dependencies
3. Write `frontend/src/packages/terra/vitest.config.ts`
4. Add `"test:unit": "vitest run"` and `"test": "pnpm run test:unit"` to Terra `package.json`
5. Write at least 3 Terra component tests (`*.test.tsx`): Button, Badge, Typography
6. Add `vitest`, `@vitest/coverage-v8`, `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom`, `jsdom`, `msw` to app dev dependencies
7. Write `frontend/vitest.config.unit.ts` (includes `**/*.test.ts?(x)`, excludes `**/*.integration.test.ts?(x)` and `src/packages/**`) and `frontend/vitest.config.integration.ts` (includes only `**/*.integration.test.ts?(x)`, also excludes `src/packages/**`); both share the `server-only` shim alias, `@orbit/test-utils` alias, and `setupFiles: ['./src/test-utils/setup.ts']`
8. Add `"test:unit": "vitest run --config vitest.config.unit.ts"`, `"test:integration": "vitest run --config vitest.config.integration.ts"`, and `"test": "pnpm run test:unit && pnpm run test:integration"` scripts to `frontend/package.json`
9. Write BFF route integration tests (`route.integration.test.ts`) for `/api/auth/session`: POST happy path, 4xx upstream error, upstream timeout, unexpected error; GET with valid session, GET without session
10. Write `health-status.integration.test.tsx` for `src/components/health-status.tsx` (loading, ok, error states) via MSW
11. Write `dashboard-sidebar.integration.test.tsx` for the dashboard sidebar (nav items, session data) via MSW + `next/navigation` module mock
12. Write unit tests (`*.test.ts`) for `lib/routing/api-routes.ts` and `lib/routing/routes.ts`
13. Add `frontend-test-terra`, `frontend-test-unit`, `frontend-test-integration`, and `frontend-test` Makefile targets; wire `frontend-test` into `frontend-check` (`frontend-test-e2e` is Phase 2 — deferred until Playwright is installed)
14. Add CI step for `make frontend-test`

**Phase 2 (E2E and Coverage):**

1. Install `@playwright/test`; write `frontend/playwright.config.ts`
2. Write baseline Category A E2E test: app loads, health status renders (mocked Go backend)
3. Write BFF handler tests for `/api/auth/discover` and `/api/auth/logout`
4. Upload coverage reports as CI artifacts
5. Write `docs/guides/frontend-testing.md`

**Phase 3 (Maturity):**

1. Expand Terra coverage to all existing component families (target ≥ 70%)
2. Expand app coverage to target ≥ 45%
3. Add E2E flows as product pages are built (auth flow, session expiry)
4. Evaluate cross-workspace coverage aggregation
5. Evaluate session context and React Query adoption to enable query-cache seeding as an alternative to MSW for session-aware component tests; where adopted, migrate relevant integration tests away from MSW-backed HTTP and toward provider-level state injection

---

## Validation

### Success Metrics

- All tests pass green on every PR; no flaky tests in the baseline suite
- Terra achieves ≥ 30% statement coverage by end of Phase 1; ≥ 70% by end of Phase 3
- App achieves ≥ 15% statement coverage by end of Phase 1; ≥ 45% by end of Phase 3
- BFF route handler for `/api/auth/session` has tests covering: POST happy path, 4xx upstream error, upstream timeout, unexpected error, GET with valid session, GET without session
- `orbitClient`'s `ApiError` parsing is exercised in at least one BFF route test via MSW (not mocked out)
- `make frontend-test-unit` and `make frontend-test-integration` together complete in under 60 seconds on a developer machine with a warm cache
- `make frontend-test` (unit + integration + E2E) completes in under 5 minutes in CI

### Monitoring

- Coverage reports uploaded as CI artifacts on every merge to `main`; a meaningful decline in coverage percentage triggers a team review conversation (not an automated block)
- Flaky E2E tests tracked in a dedicated issue label; any test that fails 2+ times in the same week is investigated immediately

---

## Related Decisions

- [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) -- monorepo structure; pnpm workspace conventions this ADR builds on
- [frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) -- Next.js 16 App Router application structure; the BFF layer and server-only boundary this ADR tests
- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) -- Terra design system workspace package; the package isolation model this ADR's test configuration mirrors

---

## References

- [Vitest documentation](https://vitest.dev/)
- [React Testing Library documentation](https://testing-library.com/docs/react-testing-library/intro/)
- [Playwright documentation](https://playwright.dev/)
- [msw v2 documentation](https://mswjs.io/)
- [server-only package](https://www.npmjs.com/package/server-only)
- [Frontend Test Strategy PRD](../prd/frontend-test-strategy.md)

---

## Notes

**The `server-only` shim** is the most operationally sensitive decision in this ADR. It must be documented clearly in the frontend testing guide (Phase 2): (1) why it exists, (2) that it does not weaken any production security boundary, and (3) that when a new server-only module is added, the shim handles it automatically via the alias without any per-module configuration.

**The state-over-mocking principle** reflects the current architecture. Several components (`DashboardSidebar`, `HealthStatus`) use independent `useEffect` + `fetch` patterns for data access. MSW is the right tool for testing these today. As the app matures and adopts a session context or React Query, the corresponding tests should be updated to seed data directly into the query cache or context provider. This eliminates HTTP interception for session-aware components and makes tests faster and more deterministic. The `renderWithProviders()` wrapper in `test-utils/render.tsx` is the extension point for this migration -- add providers to the wrapper, update tests transparently.

**The Category A E2E boundary** is a hard constraint: a file in `frontend/e2e/` must never require the Go backend, Postgres, or Redis to be running. If a test ever needs the real backend, it does not belong in `frontend/` at all — it is a system-level concern requiring a separate ADR. This boundary keeps `make check` fast, infrastructure-free, and executable on every PR without Docker Compose.

**DOM environment rationale:** `happy-dom` for Terra, `jsdom` for the app. `happy-dom` is faster and sufficient for isolated component tests that do not require `NextResponse`, `Headers`, or other Web API surface that Next.js depends on internally. `jsdom` provides the compatibility layer that makes App Router API testing reliable. If Terra ever needs a browser API not supported by `happy-dom`, switch per-file using `@vitest-environment jsdom`.

**Last Updated:** 2026-05-07

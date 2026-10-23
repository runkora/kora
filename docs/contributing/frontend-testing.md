# Frontend Testing Guide

This guide covers how to run tests, how the test tiers work, and how to write new tests that fit the established patterns. See [frontend-test-strategy-and-toolchain](../adr/2026-05-08-frontend-test-strategy-and-toolchain.md) for the full rationale behind these decisions.

---

## Running Tests

```bash
# All frontend tests (unit + integration)
make frontend-test

# Unit tests only (Hades + app — fast, no MSW, no network)
make frontend-test-unit

# Integration tests only (MSW-backed, BFF routes + components)
make frontend-test-integration

# Hades component tests only (fast inner loop when working on Hades)
make frontend-test-hades

# E2E tests (Playwright — spins up the Next.js dev server)
make frontend-test-e2e

# With coverage reports
cd frontend && pnpm run test:coverage:unit
cd frontend && pnpm run test:coverage:integration
```

Coverage HTML reports are written to `frontend/coverage/unit/` and `frontend/coverage/integration/`.

---

## Test Tiers

### Tier 1: Unit Tests (`*.test.ts`, `*.test.tsx`)

Pure-function and isolated-component tests with no network. Run under Vitest + jsdom (app) or happy-dom (Hades).

- **Location**: colocated with the file under test
- **Config**: `vitest.config.unit.ts` (app), `vitest.config.ts` (Hades)
- **No** MSW, no `server` import, no async fetch
- **Target**: pure utilities, routing helpers, Hades components in isolation

```ts
// src/lib/routing/routes.test.ts
import { describe, it, expect } from 'vitest';
import { isSafeRedirectPath } from './routes';

it('rejects an absolute URL', () => {
  expect(isSafeRedirectPath('https://evil.example.com')).toBe(false);
});
```

### Tier 2: Component Tests (Hades, `*.test.tsx`)

Hades components tested with `happy-dom` via their own `vitest.config.ts`. These tests live inside `src/packages/hades/` and run in complete isolation from the app.

```tsx
// src/packages/hades/src/components/button/button.test.tsx
import { render, screen } from '@testing-library/react';
import { Button } from './button';

it('renders with the provided label', () => {
  render(<Button>Save</Button>);
  expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
});
```

### Tier 3: Integration Tests (`*.integration.test.ts`, `*.integration.test.tsx`)

Component and BFF route tests that exercise real fetch/state machines via MSW. Run under jsdom with `vitest.config.integration.ts`.

- **Location**: colocated with the file under test
- MSW server lifecycle is managed by `src/test-utils/setup.ts`
- `onUnhandledRequest: 'error'`, every fetch must be handled by a registered handler
- Use `server.use(...)` inside a test to override the default happy-path handlers

```ts
// Override the default handler for one test
import { server } from '@runkora/test-utils';
import { http, HttpResponse } from 'msw';

it('returns 503 when upstream is unreachable', async () => {
  server.use(http.get(UPSTREAM_URL, () => HttpResponse.error()));
  const res = await GET(makeRequest('alice@acme.com'));
  expect(res.status).toBe(503);
});
```

### Tier 4: E2E Tests (`*.spec.ts` in `frontend/e2e/`)

Playwright tests against the running Next.js dev server. All BFF calls are intercepted with `page.route()`. The Go backend must never be required.

---

## Writing a New Integration Test

### BFF route handler

1. Create `route.integration.test.ts` colocated with `route.ts`
2. `vi.mock('@runkora/server/config', ...)` to set `config.koraApiUrl` without triggering `requireEnv`
3. Use `server.use(...)` to control upstream HTTP responses
4. Import and call the handler directly (`import { GET } from './route'`)

```ts
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraApiUrl: 'http://test-kora-api' },
}));

import { GET } from './route';

it('returns 200 on success', async () => {
  server.use(
    http.get('http://test-kora-api/my/endpoint', () =>
      HttpResponse.json({ ok: true }),
    ),
  );
  const res = await GET(new NextRequest('http://localhost/api/my-route'));
  expect(res.status).toBe(200);
});
```

### Component with HTTP state

1. Create `component.integration.test.tsx` colocated with `component.tsx`
2. Use `renderWithProviders` from `@runkora/test-utils`
3. Default MSW handlers provide happy-path responses; override per-test as needed
4. Use `screen.findByText(...)` (async) for post-fetch assertions

```tsx
import { screen } from '@testing-library/react';
import { renderWithProviders, server } from '@runkora/test-utils';
import { http, HttpResponse } from 'msw';
import { MyComponent } from './my-component';

it('renders data after fetch resolves', async () => {
  server.use(
    http.get('/api/my-data', () => HttpResponse.json({ value: 'hello' })),
  );
  renderWithProviders(<MyComponent />);
  await screen.findByText('hello');
});
```

---

## Test Utilities Reference

All helpers live in `src/test-utils/` and are importable from `@runkora/test-utils`.

| Export | Description |
|--------|-------------|
| `server` | MSW `SetupServer` instance: call `server.use(...)` to add per-test handlers |
| `renderWithProviders` | RTL `render` wrapper: extend with new providers as the app grows |
| `createSessionUser(overrides?)` | Factory for `SessionUser` |
| `createSessionPayload(overrides?)` | Factory for `SessionPayload` (includes a structurally valid fake JWT) |
| `createBackendSessionResponse(overrides?, user?)` | Factory for `BackendSessionResponse` |
| `createHealthResponse(overrides?)` | Factory for `HealthResponse` |

Default MSW handlers in `src/test-utils/msw/handlers/` provide happy-path responses for all auth and health endpoints. They apply to every test automatically.

---

## The `server-only` Shim

`server-only` is an npm package that throws at import time if it's ever bundled into a client-side context. It's used at the top of BFF route files and `@runkora/server/*` modules to enforce the server boundary.

Vitest runs in Node, not in a browser, so importing any module that contains `import 'server-only'` would throw, even though there is no real security boundary in a test environment.

The shim at `src/test-utils/server-only-shim.js` is an empty module. Both `vitest.config.unit.ts` and `vitest.config.integration.ts` alias `server-only` to this shim:

```ts
alias: {
  'server-only': path.resolve(__dirname, './src/test-utils/server-only-shim.js'),
}
```

**This does not weaken any production security boundary.** The production Next.js build is unaffected: the alias only applies inside Vitest. When a new module that imports `server-only` is added to the codebase, the shim handles it automatically with no additional per-module configuration.

---

## E2E Constraints (Category A)

Every file in `frontend/e2e/` must satisfy:

- **No live Go backend**: no requests to the real identity, inference, or policy services
- **No database or Redis**: no infrastructure dependencies whatsoever
- **No real OIDC IdP**: mock OIDC discovery and authorization endpoints with `page.route()`

If a test needs the real backend, it is a **system-level** concern and does not belong in `frontend/e2e/`. It requires a separate test harness with Docker Compose (see [frontend-test-strategy-and-toolchain](../adr/2026-05-08-frontend-test-strategy-and-toolchain.md) Category B).

This boundary keeps `make check` infrastructure-free and executable on every PR.

---

## Coverage Thresholds

Coverage thresholds are **reporting-only**: they surface trends but never block a green build.

| Tier | Lines | Statements | Functions | Branches |
|------|-------|------------|-----------|---------|
| App unit | 15% | 15% | 15% | 10% |
| App integration | 15% | 15% | 15% | 10% |
| Hades (vitest.config.ts) | n/a | n/a | n/a | n/a |

Phase 3 targets: App ≥ 45%, Hades ≥ 70%.

Coverage reports are uploaded as CI artifacts on every PR merge to `main`. A meaningful decline triggers a team review, not an automated block.

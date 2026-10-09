import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:3000';
const port = new URL(baseURL).port || '3000';

/**
 * Playwright E2E configuration.
 *
 * Category A constraint (docs/adr/2026-04-01-browser-auth-flow-and-bff-session-architecture.md): E2E tests in frontend/e2e/ must NEVER
 * require a live Go backend, Postgres, or Redis instance.  All Go API calls
 * are intercepted at the network level using page.route() inside each test.
 *
 * The Next.js dev server is started automatically by the webServer block so
 * tests can run with a single `pnpm test:e2e` command.
 */
export default defineConfig({
  // ── Test discovery ───────────────────────────────────────────────────────
  testDir: './e2e',
  testMatch: '**/*.spec.ts',

  // ── Execution settings ───────────────────────────────────────────────────
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,

  // ── Reporter ─────────────────────────────────────────────────────────────
  reporter: process.env.CI
    ? [['html', { outputFolder: 'playwright-report', open: 'never' }], ['github']]
    : [['html', { outputFolder: 'playwright-report', open: 'on-failure' }]],

  // ── Shared test settings ─────────────────────────────────────────────────
  use: {
    /** All E2E tests target the local Next.js dev server. */
    baseURL,

    /** Capture a trace on first retry to aid CI debugging. */
    trace: 'on-first-retry',

    /** Screenshot on failure. */
    screenshot: 'only-on-failure',
  },

  // ── Projects (browsers) ──────────────────────────────────────────────────
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // ── Dev server ───────────────────────────────────────────────────────────
  webServer: {
    command: `pnpm run dev --port ${port}`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: {
      // Always use the fixed test secret so that injectSessionCookie() in
      // fixtures.ts produces a cookie the dev server can unseal, regardless
      // of whether BFF_SESSION_SECRET is set in the host environment (e.g.
      // when invoked via `make` which exports the root .env).
      BFF_SESSION_SECRET: 'playwright-test-session-secret-32ch',
      ORBIT_API_URL: process.env.ORBIT_API_URL ?? 'http://localhost:8080',
      // These URLs are not called during E2E tests (all BFF fetches are
      // intercepted by page.route()), but config.ts requires them at boot.
      // A new requireEnv() in config.ts must be added here too: without it the
      // dev server never starts and every test fails on a missing element,
      // which reads as a UI regression rather than a missing variable.
      INFERENCE_GATEWAY_URL: process.env.INFERENCE_GATEWAY_URL ?? 'http://localhost:8081/v1',
      POLICY_SERVICE_URL: process.env.POLICY_SERVICE_URL ?? 'http://localhost:8083/api/v1',
      POLICY_GATEWAY_URL: process.env.POLICY_GATEWAY_URL ?? 'http://localhost:8084/v1',
      ORBIT_AGENTS_URL: process.env.ORBIT_AGENTS_URL ?? 'http://localhost:8080',
      ORBIT_TRIGGERS_URL: process.env.ORBIT_TRIGGERS_URL ?? 'http://localhost:8097',
      TOOL_GATEWAY_URL: process.env.TOOL_GATEWAY_URL ?? 'http://localhost:8096',
    },
  },
});

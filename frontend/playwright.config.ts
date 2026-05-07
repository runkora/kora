import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright E2E configuration.
 *
 * Category A constraint (ADR-0030): E2E tests in frontend/e2e/ must NEVER
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
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:3000',

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
    command: 'pnpm run dev',
    url: 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: {
      // Provide a dummy session secret so the Next.js app boots without a
      // real .env file.  Tests mock all auth responses via page.route().
      BFF_SESSION_SECRET: process.env.BFF_SESSION_SECRET ?? 'playwright-test-session-secret-32ch',
      ORBIT_API_URL: process.env.ORBIT_API_URL ?? 'http://localhost:8080',
    },
  },
});

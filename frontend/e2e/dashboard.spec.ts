/**
 * E2E baseline — Dashboard page (/) with HealthStatus component.
 *
 * docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md Category A constraint: no live Go backend is required.
 * All BFF API calls are intercepted via page.route() — the Next.js dev server
 * is the only real process under test.
 *
 * Authentication: the Edge middleware checks the iron-session cookie before
 * rendering any protected page.  page.route() intercepts browser-side requests
 * only and cannot bypass the server-side cookie check.  injectSessionCookie()
 * seals a valid test session with the dev-server secret and sets it in the
 * browser context via context.addCookies() before each navigation.
 *
 * Coverage:
 *   1. Dashboard page renders the main heading
 *   2. HealthStatus renders the "Backend" section label
 *   3. HealthStatus renders the healthy badge and service name from the API
 *   4. HealthStatus renders the error/unreachable state when the API returns 502
 */
import { test, expect, type Route } from '@playwright/test';
import { injectSessionCookie } from './fixtures';

// ── Shared fixture data ────────────────────────────────────────────────────────

const SESSION_RESPONSE = {
  user: { sub: 'user-e2e', email: 'e2e@orbit.local', name: 'E2E User' },
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
};

const HEALTH_OK_RESPONSE = {
  data: { status: 'healthy', service: 'identity', timestamp: '2026-05-07T00:00:00Z' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

async function mockSession(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/auth/session', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(SESSION_RESPONSE),
    });
  });
}

async function mockHealth(
  page: import('@playwright/test').Page,
  status: number,
  body: object,
): Promise<void> {
  await page.route('**/api/identity/health', (route: Route) => {
    void route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify(body),
    });
  });
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('/ — Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    // Inject a valid iron-session cookie so the middleware auth guard passes.
    // mockSession mocks the client-side BFF fetch made by PermissionsProvider.
    await injectSessionCookie(page.context());
    await mockSession(page);
  });

  test('renders the Dashboard page heading', async ({ page }) => {
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  });

  test('HealthStatus renders the Backend section label', async ({ page }) => {
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    // The "Backend" label is always visible — it renders regardless of health state.
    await expect(page.getByText('Backend')).toBeVisible();
  });

  test('HealthStatus renders healthy badge and service name from the mocked API', async ({
    page,
  }) => {
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    // exact: true — case-sensitive to avoid matching "Healthy" agent status badges in the table.
    await expect(page.getByText('healthy', { exact: true })).toBeVisible();
    await expect(page.getByText('identity', { exact: true })).toBeVisible();
  });

  test('HealthStatus renders the error state when the API returns 502', async ({ page }) => {
    await mockHealth(page, 502, {
      error: { code: 'BACKEND_UNREACHABLE', message: 'Backend unreachable' },
    });
    await page.goto('/');

    await expect(page.getByText('Unreachable')).toBeVisible();
    await expect(page.getByText('make dev')).toBeVisible();
  });
});

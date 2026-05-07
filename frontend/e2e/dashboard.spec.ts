/**
 * E2E baseline — Dashboard page (/) with HealthStatus component.
 *
 * ADR-0049 Category A constraint: no live Go backend is required.
 * All BFF API calls are intercepted via page.route() — the Next.js dev server
 * is the only real process under test.
 *
 * Coverage:
 *   1. Dashboard page renders the main heading
 *   2. HealthStatus renders the "Backend" section label
 *   3. HealthStatus renders the healthy badge and service name from the API
 *   4. HealthStatus renders the error/unreachable state when the API returns 502
 */
import { test, expect, type Route } from '@playwright/test';

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
  test('renders the Dashboard page heading', async ({ page }) => {
    await mockSession(page);
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  });

  test('HealthStatus renders the Backend section label', async ({ page }) => {
    await mockSession(page);
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    // The "Backend" label is always visible — it renders regardless of health state.
    await expect(page.getByText('Backend')).toBeVisible();
  });

  test('HealthStatus renders healthy badge and service name from the mocked API', async ({
    page,
  }) => {
    await mockSession(page);
    await mockHealth(page, 200, HEALTH_OK_RESPONSE);
    await page.goto('/');

    // Badge text matches the status field in the API response.
    await expect(page.getByText('healthy')).toBeVisible();
    // Service name rendered alongside the badge.
    await expect(page.getByText('identity')).toBeVisible();
  });

  test('HealthStatus renders the error state when the API returns 502', async ({ page }) => {
    await mockSession(page);
    await mockHealth(page, 502, {
      error: { code: 'BACKEND_UNREACHABLE', message: 'Backend unreachable' },
    });
    await page.goto('/');

    await expect(page.getByText('Unreachable')).toBeVisible();
    await expect(page.getByText('make dev')).toBeVisible();
  });
});

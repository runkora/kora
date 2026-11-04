/**
 * E2E baseline — Dashboard page (/).
 *
 * docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md Category A constraint: no live Go backend is required.
 * All BFF API calls are intercepted via page.route() — the Next.js dev server
 * is the only real process under test.  Server-side (RSC) data fetches have no
 * backend to reach, so each dashboard section degrades to its empty state; the
 * page still renders its headings and the always-present stat cards.
 *
 * Authentication: the Edge middleware checks the iron-session cookie before
 * rendering any protected page.  page.route() intercepts browser-side requests
 * only and cannot bypass the server-side cookie check.  injectSessionCookie()
 * seals a valid test session with the dev-server secret and sets it in the
 * browser context via context.addCookies() before each navigation.
 *
 * Coverage:
 *   1. Dashboard page renders the main heading
 *   2. Dashboard renders the always-present stat cards and section cards
 */
import { test, expect, type Route } from '@playwright/test';
import { injectSessionCookie } from './fixtures';

// ── Shared fixture data ────────────────────────────────────────────────────────

const SESSION_RESPONSE = {
  user: { sub: 'user-e2e', email: 'e2e@runkora.test', name: 'E2E User' },
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
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

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('/ — Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    // Inject a valid iron-session cookie so the middleware auth guard passes.
    // mockSession mocks the client-side BFF fetch made by PermissionsProvider.
    await injectSessionCookie(page.context());
    await mockSession(page);
  });

  test('renders the Dashboard page heading', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  });

  test('renders the always-present stat cards and section cards', async ({ page }) => {
    await page.goto('/');

    // Text unique to the dashboard body (avoids the sidebar nav items, which
    // also contain "Connections"/"Environments" and would trip strict mode).
    // The "Active Runs" stat card renders regardless of backend data.
    await expect(page.getByText('Active Runs')).toBeVisible();

    // Section cards that always render (the Inference section is hidden when
    // analytics is unavailable, which is the case with no backend).
    await expect(page.getByText('Recent Runs')).toBeVisible();
    await expect(page.getByText('Spend vs budget')).toBeVisible();
  });
});

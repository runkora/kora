/**
 * E2E baseline: the Runs tab, which `/` redirects to.
 *
 * docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md Category A constraint: no live Go
 * backend is required. The list and its live stream load in the browser, so page.route() answers
 * both BFF routes; the Next.js dev server is the only real process under test.
 *
 * Authentication: the Edge middleware checks the iron-session cookie before rendering any
 * protected page, and page.route() cannot bypass that server-side check. injectSessionCookie()
 * seals a valid test session with the dev-server secret and sets it before each navigation.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import { injectSessionCookie } from './fixtures';

const SESSION_RESPONSE = {
  user: { sub: 'user-e2e', email: 'e2e@runkora.test', name: 'E2E User' },
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
};

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

async function mockApi(page: Page): Promise<void> {
  await page.route('**/api/auth/session', (route) => json(route, SESSION_RESPONSE));
  await page.route('**/api/agents/runs/stream?*', (route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: ': open\n\n' }),
  );
  await page.route('**/api/agents/runs?*', (route) => json(route, { data: [], next_cursor: null }));
}

test.describe('/runs: Runs', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await mockApi(page);
  });

  test('/ redirects to the Runs heading', async ({ page }) => {
    await page.goto('/');

    await expect(page).toHaveURL(/\/runs$/);
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible();
  });

  test('shows the empty state when no runs exist', async ({ page }) => {
    await page.goto('/runs');

    await expect(page.getByText('No runs yet')).toBeVisible();
  });
});

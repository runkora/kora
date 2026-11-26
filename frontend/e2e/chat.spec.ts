/**
 * E2E baseline: the chat page, where every start action leads.
 *
 * No live Go backend: page.route() answers the BFF routes the page reads, and the Next.js dev
 * server is the only real process under test. injectSessionCookie() passes the Edge middleware.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import { injectSessionCookie } from './fixtures';

const SESSION_RESPONSE = {
  user: { sub: 'user-e2e', email: 'e2e@runkora.test', name: 'Sam Lee' },
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
};

const agent = (slug: string, name: string) => ({
  id: `at-${slug}`,
  slug,
  name,
  description: `${name} does things.`,
  identity_mode: 'autonomous',
  state: 'active',
  can_run: true,
});

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

async function mockApi(page: Page): Promise<void> {
  await page.route('**/api/auth/session', (route) => json(route, SESSION_RESPONSE));
  await page.route('**/api/agents/types', (route) =>
    json(route, { data: [agent('coder', 'Coder'), agent('reviewer', 'Reviewer')] }),
  );
  await page.route('**/api/agents/runs/stream?*', (route) =>
    route.fulfill({ status: 200, contentType: 'text/event-stream', body: ': open\n\n' }),
  );
  await page.route('**/api/agents/runs?*', (route) => json(route, { data: [], next_cursor: null }));
}

test.describe('/chat: Chat', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await mockApi(page);
  });

  test('Start work on the Runs tab opens the chat page', async ({ page }) => {
    await page.goto('/runs');
    await page.getByRole('link', { name: 'Start work' }).click();

    await expect(page).toHaveURL(/\/chat$/);
    await expect(
      page.getByRole('heading', { name: 'What should we work on next, Sam?' }),
    ).toBeVisible();
  });

  test('selects the agent in the address', async ({ page }) => {
    await page.goto('/chat?agent=reviewer');

    await expect(page.getByRole('textbox', { name: 'Describe the work' })).toHaveAttribute(
      'placeholder',
      'What should Reviewer work on?',
    );
  });
});

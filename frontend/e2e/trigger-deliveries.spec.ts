/**
 * E2E — Triggers / Deliveries paging (#412).
 *
 * Category A per docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md:
 * no live backend, so every BFF call is intercepted with page.route() and the
 * server render (RSC) starts the page empty with its error banner.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import { injectSessionCookie } from './fixtures';

const SESSION_RESPONSE = {
  user: { sub: 'user-e2e', email: 'e2e@orbit.local', name: 'E2E User' },
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
};

/** One delivery on the wire, in the snake_case shape toDeliveryRow reads. */
function delivery(id: string, receivedAt: string, status = 'failed') {
  return {
    id,
    kind: 'webhook',
    source_id: 'src-1',
    event_type: 'push',
    status,
    received_at: receivedAt,
    trigger_id: 't1',
    trigger_name: 'gh-push',
    run_id: `run-${id}`,
    error: null,
    payload_excerpt: null,
    runs: [
      {
        trigger_id: 't1',
        trigger_name: 'gh-push',
        run_id: `run-${id}`,
        status,
        error: null,
        created_at: receivedAt,
      },
    ],
  };
}

/** Records every deliveries request the browser makes, and answers in order. */
async function stubDeliveries(
  page: Page,
  pages: Array<{ data: unknown[]; next_cursor: string | null } | { status: number }>,
) {
  const seen: URL[] = [];
  let i = 0;
  // Awaited rather than fire-and-forget: page.route() is asynchronous and
  // openDeliveries() navigates immediately after, so an unawaited registration
  // lets the first deliveries request escape to the real BFF.
  await page.route('**/api/trigger-deliveries*', (route: Route) => {
    seen.push(new URL(route.request().url()));
    const body = pages[Math.min(i, pages.length - 1)];
    i += 1;
    if (body !== undefined && 'status' in body) {
      void route.fulfill({
        status: body.status,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'deliveries backend is unavailable' }),
      });
      return;
    }
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    });
  });
  return seen;
}

async function openDeliveries(page: Page) {
  await page.goto('/triggers');
  await page.getByRole('tab', { name: 'Deliveries' }).click();
}

test.describe('/triggers — Deliveries paging', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await page.route(
      '**/api/auth/session',
      (route: Route) =>
        void route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(SESSION_RESPONSE),
        }),
    );
  });

  test('filters on the server and pages with the cursor', async ({ page }) => {
    const seen = await stubDeliveries(page, [
      {
        data: [delivery('d1', '2026-10-07T12:00:02Z'), delivery('d2', '2026-10-07T12:00:01Z')],
        next_cursor: '2026-10-07T12:00:01Z|d2',
      },
      { data: [delivery('d3', '2026-10-07T12:00:00Z')], next_cursor: null },
    ]);

    await openDeliveries(page);
    await page.getByRole('combobox').first().selectOption('failed');

    // The filter went to the server, not to rows already on screen.
    await expect.poll(() => seen.length).toBeGreaterThan(0);
    expect(seen[0]?.searchParams.getAll('status')).toEqual(['failed']);
    expect(seen[0]?.searchParams.get('cursor')).toBeNull();

    await expect(page.getByText('d1')).toBeVisible();
    const loadMore = page.getByRole('button', { name: 'Load more' });
    await expect(loadMore).toBeVisible();

    await loadMore.click();

    // The second request carried the cursor from the first page.
    await expect.poll(() => seen.length).toBe(2);
    expect(seen[1]?.searchParams.get('cursor')).toBe('2026-10-07T12:00:01Z|d2');
    expect(seen[1]?.searchParams.getAll('status')).toEqual(['failed']);

    // Appended, not replaced: the first page is still on screen.
    await expect(page.getByText('d3')).toBeVisible();
    await expect(page.getByText('d1')).toBeVisible();

    // No further page, so the affordance goes away rather than lying.
    await expect(loadMore).toBeHidden();
  });

  // The slug resolver is built from the triggers the SERVER render supplied,
  // and this harness has no backend for that fetch, so no slug resolves and the
  // id must degrade to text. The linked case is in delivery-detail.test.tsx.
  test('renders the run id as text when no agent slug can be resolved', async ({ page }) => {
    await stubDeliveries(page, [
      { data: [delivery('d1', '2026-10-07T12:00:00Z')], next_cursor: null },
    ]);

    await openDeliveries(page);
    await page.getByRole('combobox').first().selectOption('failed');
    await expect(page.getByText('d1')).toBeVisible();

    await page
      .getByRole('row', { name: /View payload and outcome/ })
      .first()
      .click();

    await expect(page.getByText('run-d1', { exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'run-d1' })).toHaveCount(0);
  });

  test('a failed page says so and offers Retry', async ({ page }) => {
    const seen = await stubDeliveries(page, [
      { status: 502 },
      { data: [delivery('d1', '2026-10-07T12:00:00Z')], next_cursor: null },
    ]);

    await openDeliveries(page);
    await page.getByRole('combobox').first().selectOption('failed');

    const retry = page.getByRole('button', { name: 'Retry' });
    await expect(retry).toBeVisible();

    await retry.click();
    await expect.poll(() => seen.length).toBe(2);
    await expect(page.getByText('d1')).toBeVisible();
  });
});

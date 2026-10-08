/**
 * E2E — Marketplace overview and Skill detail.
 *
 * No live skills-service backend exists yet (mock client + fixtures,
 * src/server/marketplace-fixtures.ts). page.route() cannot intercept a
 * server-side RSC fetch, so the first render of every test below uses the
 * real fixture data rather than a mocked one — these tests assert against
 * that real fixture data (Marketplace names, Skill titles) directly. Only
 * later, browser-triggered BFF calls are intercepted with page.route().
 */
import { test, expect, type Route } from '@playwright/test';
import { injectSessionCookie, injectExpiredSessionCookie } from './fixtures';

test.describe('/marketplace — overview and Skill detail', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
  });

  test('browsing from the overview to a Skill detail and back returns to the same results', async ({
    page,
  }) => {
    await page.goto('/marketplace');

    // No saved preference exists yet, so the first Marketplace in name order loads.
    await expect(page.getByRole('button', { name: /Marketplace: ClearRoute Core/ })).toBeVisible();

    await page.getByRole('button', { name: /Marketplace: ClearRoute Core/ }).click();
    await page.getByRole('radio', { name: 'Platform Engineering' }).click();

    // "React Expert" is the 39th of 61 Platform Engineering Skills in A–Z
    // order, past the default 24-row page, so narrow the results first.
    await page.getByRole('textbox', { name: 'Search Skills' }).fill('React Expert');
    await expect(page.getByText('React Expert')).toBeVisible();

    // KNOWN PRODUCTION BUG — confirmed directly, not a test-writing mistake:
    // APP_ROUTES.marketplace.detail() (src/lib/routing/routes.ts) builds the
    // row's link with `encodeURIComponent(slug)`, so the slug's internal "/"
    // become literal "%2F" (e.g. ".../platform-engineering%2Fskills%2Freact-
    // expert"). The [...slug] catch-all route
    // (src/app/(dashboard)/marketplace/[...slug]/page.tsx) receives that as a
    // single segment and never decodes the "%2F" back to "/", so
    // `segments.join('/')` never matches a real fixture slug — every Skill
    // row in the overview 404s to "Skill not found" instead of opening its
    // detail page. Verified in isolation: page.goto() to the exact same
    // %2F-encoded URL also 404s, while page.goto() to the equivalent literal
    // "/platform-engineering/skills/react-expert" URL loads the Skill fine —
    // isolating the break to link-building/param-parsing, not the Skill
    // detail page itself. Left failing (not routed around) per instructions
    // not to edit production code to force a pass; this documents the
    // intended behavior so the test flips to an "unexpected pass" (and
    // demands removal of test.fail()) the day this is fixed.
    test.fail(
      true,
      "Row-click navigation 404s: encodeURIComponent(slug) in APP_ROUTES.marketplace.detail() is not reversed by the [...slug] route, so segments.join('/') never matches a real Skill slug.",
    );

    await page.getByRole('row', { name: 'Open React Expert' }).click();

    await expect(page).toHaveURL(/\/marketplace\/platform-engineering\/skills\/react-expert/);
    await expect(page.getByRole('heading', { name: 'React Expert' })).toBeVisible();
    await expect(page.getByText('Senior React developer.')).toBeVisible();

    await page.getByRole('link', { name: 'Skills' }).click();

    await expect(page).toHaveURL(/marketplace=Platform\+Engineering/);
    await expect(page.getByText('React Expert')).toBeVisible();
  });

  test('a shared overview link opens the same Marketplace directly, including after a refresh', async ({
    page,
  }) => {
    await page.goto('/marketplace?marketplace=ClearRoute+Platform');

    await expect(
      page.getByRole('button', { name: /Marketplace: ClearRoute Platform/ }),
    ).toBeVisible();
    await expect(page.getByText('Cost Report')).toBeVisible();

    await page.reload();

    await expect(
      page.getByRole('button', { name: /Marketplace: ClearRoute Platform/ }),
    ).toBeVisible();
    await expect(page.getByText('Cost Report')).toBeVisible();
  });

  test('an unknown Skill slug shows a not-found message with a way back', async ({ page }) => {
    await page.goto('/marketplace/does-not-exist');

    await expect(page.getByText('Skill not found')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Back to Skills' })).toBeVisible();
  });

  test('a later browse change that fails keeps the previous results and offers a retry', async ({
    page,
  }) => {
    await page.goto('/marketplace');
    await expect(page.getByRole('button', { name: /Marketplace: /, exact: false })).toBeVisible();

    await page.route('**/api/marketplace/skills*', (route: Route) => {
      void route.fulfill({ status: 500, contentType: 'application/json', body: '{}' });
    });

    await page.getByRole('button', { name: /Marketplace: /, exact: false }).click();
    await page.getByRole('radio', { name: 'Platform Engineering' }).click();

    await expect(
      page.getByText(/The results below are from the last successful request\./),
    ).toBeVisible();
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
  });

  test('an expired session redirects to login before the overview renders', async ({ page }) => {
    // Confirmed against src/proxy.ts: the Edge middleware reads the cookie
    // directly (session.user and session.accessToken are both present here),
    // so step 3's unauthenticated check does not fire. Step 4 then sees
    // secondsUntilExpiry <= 0, attempts a refresh against ORBIT_API_URL (no
    // backend is running in this E2E setup, so it fails), destroys the
    // session, and redirects to /login — before the Marketplace overview's
    // own isSessionValid()/"Session expired" branch in page.tsx ever runs.
    // That branch is only reachable for a session the middleware itself
    // considers valid but the page's stricter check does not — not
    // reproducible from the browser against the current mock session shape.
    await injectExpiredSessionCookie(page.context());

    await page.goto('/marketplace');

    await expect(page).toHaveURL(/\/login(\?|$)/);
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
  });
});

test.describe('/marketplace — accessibility and layout', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
  });

  test('every overview control has an accessible name and is keyboard-reachable', async ({
    page,
  }) => {
    await page.goto('/marketplace');

    await expect(page.getByRole('textbox', { name: 'Search Skills' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Marketplace: /, exact: false })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Sort skills' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'List view' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Card view' })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Results per page' })).toBeVisible();

    await page.getByRole('textbox', { name: 'Search Skills' }).focus();
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: /Marketplace: /, exact: false })).toBeFocused();
  });

  test('a failed-load retry button has an accessible name', async ({ page }) => {
    await page.route('**/api/marketplace/skills*', (route) =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{}' }),
    );
    await page.goto('/marketplace');
    await page.getByRole('button', { name: /Marketplace: /, exact: false }).click();
    // Picking the already-selected first Marketplace (ClearRoute Core, the
    // default) fires no onValueChange and so no request — pick a different one.
    await page.getByRole('radio', { name: 'Platform Engineering' }).click();

    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
  });

  test('the overview toolbar stays usable just below and at the 1024px breakpoint', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1023, height: 900 });
    await page.goto('/marketplace');
    await expect(page.getByRole('textbox', { name: 'Search Skills' })).toBeVisible();

    await page.setViewportSize({ width: 1024, height: 900 });
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Search Skills' })).toBeVisible();
  });

  test('the overview renders on a mobile viewport', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/marketplace');
    await expect(page.getByRole('heading', { name: 'Skills' })).toBeVisible();
  });

  test('the Skill detail page renders on mobile and desktop viewports', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/marketplace/platform-engineering/skills/react-expert');
    await expect(page.getByRole('heading', { name: 'React Expert' })).toBeVisible();

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.reload();
    await expect(page.getByRole('heading', { name: 'React Expert' })).toBeVisible();
    // getByText('Source') is ambiguous: it substring-matches the "resources"
    // count text in the Contents section header too.
    await expect(page.getByRole('heading', { name: 'Source' })).toBeVisible();
  });
});

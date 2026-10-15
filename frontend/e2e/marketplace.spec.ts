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
import { test, expect, type Locator, type Page, type Route } from '@playwright/test';
import { injectSessionCookie, injectExpiredSessionCookie } from './fixtures';

const restoredBrowse = {
  marketplace: 'Platform Engineering',
  query: 'ter',
  page: 2,
  pageSize: 12,
  sort: 'name-desc',
};

async function expectBrowseUrl(page: Page, pathname = '/marketplace') {
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === pathname &&
      Object.entries(restoredBrowse).every(
        ([key, value]) => url.searchParams.get(key) === String(value),
      ),
  );
}

async function expectRestoredOverview(page: Page) {
  await expectBrowseUrl(page);
  await expect(
    page.getByRole('button', { name: `Marketplace: ${restoredBrowse.marketplace}`, exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Search Skills' })).toHaveValue(
    restoredBrowse.query,
  );
  await expect(page.getByRole('combobox', { name: 'Sort skills' })).toHaveValue(
    restoredBrowse.sort,
  );
  await expect(page.getByRole('combobox', { name: 'Results per page' })).toHaveValue('12');
  await expect(page.getByRole('button', { name: 'Page 2', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await expect(page.getByText('13–14 of 14', { exact: true })).toBeVisible();
  const results = page.getByRole('row', { name: /^Open / });
  await expect(results).toHaveCount(2);
  await expect(results.nth(0)).toHaveAccessibleName('Open Budget Forecaster');
  await expect(results.nth(1)).toHaveAccessibleName('Open Bicep Linter');
}

test.describe('/marketplace — overview and Skill detail', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await page.route('**/api/preferences', (route) => route.fulfill({ json: {} }));
  });

  test('browsing from the overview to a Skill detail and back returns to the same results', async ({
    page,
  }) => {
    await page.goto('/marketplace');

    // No saved preference exists yet, so the first Marketplace in name order loads.
    await expect(page.getByRole('button', { name: /Marketplace: ClearRoute Core/ })).toBeVisible();

    await page.getByRole('button', { name: /Marketplace: ClearRoute Core/ }).click();
    await page.getByRole('radio', { name: 'Platform Engineering' }).click();
    await expect(page.getByRole('row', { name: 'Open Airflow Access Review' })).toBeVisible();

    await page.getByRole('combobox', { name: 'Results per page' }).selectOption('12');
    await expect(page.getByText('1–12 of 61', { exact: true })).toBeVisible();
    // This search matches fourteen Skills, so all five browse values can be non-default.
    await page.getByRole('textbox', { name: 'Search Skills' }).fill(restoredBrowse.query);
    await expect(page.getByText('1–12 of 14', { exact: true })).toBeVisible();
    await page.getByRole('combobox', { name: 'Sort skills' }).selectOption(restoredBrowse.sort);
    await expect(page.getByRole('row', { name: /^Open / }).first()).toHaveAccessibleName(
      'Open Terraform Module Reviewer',
    );
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expectRestoredOverview(page);
    const originalResults = await page.getByRole('row', { name: /^Open / }).allTextContents();

    await page.getByRole('row', { name: 'Open Budget Forecaster' }).click();

    const detailPath = '/marketplace/platform-engineering%2Fskills%2Fbudget-forecaster';
    await expectBrowseUrl(page, detailPath);
    await expect(page.getByRole('heading', { name: 'Budget Forecaster' })).toBeVisible();
    await expect(
      page.getByText('Projects month-end cloud spend from the current burn rate.', { exact: true }),
    ).toBeVisible();
    const backLink = page.getByRole('link', { name: 'Skills', exact: true });
    const browseQuery = new URLSearchParams(
      Object.fromEntries(
        Object.entries(restoredBrowse).map(([key, value]) => [key, String(value)]),
      ),
    ).toString();
    await expect(backLink).toHaveAttribute('href', `/marketplace?${browseQuery}`);

    // A shared encoded detail URL must also survive a full RSC reload.
    await page.reload();
    await expectBrowseUrl(page, detailPath);
    await expect(page.getByRole('heading', { name: 'Budget Forecaster' })).toBeVisible();
    await backLink.click();

    await expectRestoredOverview(page);
    await expect(page.getByRole('row', { name: /^Open / })).toHaveText(originalResults);
  });

  test('a shared overview link restores all browse values and results, including after a refresh', async ({
    page,
  }) => {
    await page.goto(
      '/marketplace?marketplace=Platform+Engineering&query=ter&page=2&pageSize=12&sort=name-desc',
    );
    await expectRestoredOverview(page);
    const originalResults = await page.getByRole('row', { name: /^Open / }).allTextContents();

    await page.reload();

    await expectRestoredOverview(page);
    await expect(page.getByRole('row', { name: /^Open / })).toHaveText(originalResults);
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

/** Reach controls through the dashboard's real Tab order, including result rows. */
async function tabTo(page: Page, control: Locator) {
  await expect(control).toBeVisible();
  for (let step = 0; step < 80; step += 1) {
    await page.keyboard.press('Tab');
    if (await control.evaluate((element) => element === document.activeElement)) {
      await expect(control).toBeFocused();
      return;
    }
  }
  await expect(control).toBeFocused();
}

async function bounds(control: Locator) {
  const box = await control.boundingBox();
  expect(box).not.toBeNull();
  if (!box) throw new Error('Expected a rendered layout element');
  return box;
}

function overviewControls(page: Page) {
  return [
    page.getByRole('textbox', { name: 'Search Skills' }),
    page.getByRole('button', { name: /^Marketplace:/ }),
    page.getByRole('combobox', { name: 'Sort skills' }),
    page.getByRole('button', { name: 'Card view' }),
    page.getByRole('button', { name: 'List view' }),
    page.getByRole('combobox', { name: 'Results per page' }),
  ];
}

async function expectControlsFit(page: Page, controls: Locator[]) {
  const viewport = page.viewportSize();
  if (!viewport) throw new Error('Expected a configured browser viewport');
  const boxes = await Promise.all(controls.map(bounds));
  for (const box of boxes) {
    expect(box.width).toBeGreaterThan(0);
    expect(box.height).toBeGreaterThan(0);
    expect(box.x).toBeGreaterThanOrEqual(-1);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1);
  }
  for (let first = 0; first < boxes.length; first += 1) {
    for (let second = first + 1; second < boxes.length; second += 1) {
      const a = boxes[first];
      const b = boxes[second];
      const overlapX = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x);
      const overlapY = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y);
      expect(overlapX > 1 && overlapY > 1, 'Controls must not overlap').toBe(false);
    }
  }
}

async function expectNoPageOverflow(page: Page) {
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) -
          document.documentElement.clientWidth,
      ),
    )
    .toBeLessThanOrEqual(1);
}

test.describe('/marketplace — accessibility and layout', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await page.route('**/api/preferences', (route) => route.fulfill({ json: {} }));
  });

  test('search, Marketplace selection, sort and both views work through the keyboard', async ({
    page,
  }) => {
    await page.goto('/marketplace');
    const search = page.getByRole('textbox', { name: 'Search Skills' });
    const picker = page.getByRole('button', { name: /^Marketplace:/ });
    const sort = page.getByRole('combobox', { name: 'Sort skills' });
    const cards = page.getByRole('button', { name: 'Card view' });
    const list = page.getByRole('button', { name: 'List view' });

    await tabTo(page, search);
    await search.pressSequentially('no-such-skill');
    await expect(page.getByText('No Skills match your search', { exact: true })).toBeVisible();
    await tabTo(page, page.getByRole('button', { name: 'Clear search' }));
    await page.keyboard.press('Enter');
    await expect(search).toHaveValue('');
    await expect(page.getByRole('row', { name: 'Open Incident Triage' })).toBeVisible();

    await tabTo(page, picker);
    await page.keyboard.press('Enter');
    await expect(page.getByRole('radio', { name: 'ClearRoute Core' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(picker).toBeFocused();
    await expect(picker).toHaveAttribute('aria-expanded', 'false');
    await page.keyboard.press('Space');
    await expect(page.getByRole('radio', { name: 'ClearRoute Core' })).toBeFocused();
    await page.keyboard.down('ArrowUp');
    try {
      await expect(picker).toHaveAccessibleName('Marketplace: Platform Engineering');
    } finally {
      await page.keyboard.up('ArrowUp');
    }
    await expect(picker).toBeFocused();
    await expect(page.getByRole('row', { name: 'Open Airflow Access Review' })).toBeVisible();

    await tabTo(page, search);
    await search.pressSequentially('React');
    const rows = page.getByRole('row', { name: /^Open / });
    await expect(rows).toHaveCount(2);
    await expect(rows.first()).toHaveAccessibleName('Open React Expert');

    await tabTo(page, sort);
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');
    await expect(sort).toHaveValue('name-desc');
    await expect(rows.first()).toHaveAccessibleName('Open React Reviewer');

    await page.keyboard.press('Escape');
    await tabTo(page, cards);
    await page.keyboard.press('Space');
    await expect(cards).toHaveAttribute('aria-pressed', 'true');
    await expect(list).toHaveAttribute('aria-pressed', 'false');
    await expect(page.getByRole('table', { name: 'skills' })).toHaveCount(0);
    await expect(page.locator('a[href^="/marketplace/"]')).toHaveCount(2);

    await page.keyboard.press('Tab');
    await expect(list).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(list).toHaveAttribute('aria-pressed', 'true');
    await expect(cards).toHaveAttribute('aria-pressed', 'false');
    await expect(rows).toHaveCount(2);
    await page.keyboard.press('Shift+Tab');
    await expect(cards).toBeFocused();
  });

  test('page controls and results per page operate through the keyboard', async ({ page }) => {
    await page.goto('/marketplace?marketplace=Platform+Engineering&pageSize=12');
    const size = page.getByRole('combobox', { name: 'Results per page' });
    const previous = page.getByRole('button', { name: 'Previous', exact: true });
    const next = page.getByRole('button', { name: 'Next', exact: true });

    await tabTo(page, size);
    await expect(previous).toBeDisabled();
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: 'Page 1', exact: true })).toBeFocused();
    await expect(page.getByRole('button', { name: 'Page 1', exact: true })).toHaveAttribute(
      'aria-current',
      'page',
    );

    await tabTo(page, next);
    await page.keyboard.press('Enter');
    await expect(page.getByText('13–24 of 61', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Page 2', exact: true })).toHaveAttribute(
      'aria-current',
      'page',
    );
    await tabTo(page, previous);
    await page.keyboard.press('Space');
    await expect(page.getByText('1–12 of 61', { exact: true })).toBeVisible();

    await tabTo(page, page.getByRole('button', { name: 'Page 6', exact: true }));
    await page.keyboard.press('Enter');
    await expect(page.getByText('61–61 of 61', { exact: true })).toBeVisible();
    await expect(next).toBeDisabled();

    await tabTo(page, size);
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');
    await expect(size).toHaveValue('24');
    await expect(page.getByText('1–24 of 61', { exact: true })).toBeVisible();
    await expect(previous).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Page 1', exact: true })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  test('a named retry can be reached and recover a failed request through the keyboard', async ({
    page,
  }) => {
    let requests = 0;
    await page.route('**/api/marketplace/skills*', async (route) => {
      requests += 1;
      if (requests === 1) {
        await route.fulfill({ status: 503, json: { message: 'Temporary catalogue failure.' } });
      } else {
        await route.continue();
      }
    });
    await page.goto('/marketplace');
    const picker = page.getByRole('button', { name: /^Marketplace:/ });
    await tabTo(page, picker);
    await page.keyboard.press('Enter');
    await expect(page.getByRole('radio', { name: 'ClearRoute Core' })).toBeFocused();

    const notice = page.getByRole('alert').filter({ hasText: 'Temporary catalogue failure.' });
    await page.keyboard.down('ArrowUp');
    try {
      await expect(notice).toBeVisible();
    } finally {
      await page.keyboard.up('ArrowUp');
    }
    await expect(page.getByRole('row', { name: 'Open Incident Triage' })).toBeVisible();
    const retry = page.getByRole('button', { name: 'Try again' });
    await tabTo(page, retry);
    await page.keyboard.press('Enter');

    await expect(picker).toHaveAccessibleName('Marketplace: Platform Engineering');
    await expect(page.getByRole('row', { name: 'Open Airflow Access Review' })).toBeVisible();
    await expect(notice).toHaveCount(0);
    expect(requests).toBe(2);
  });

  for (const width of [1023, 1024]) {
    test(`toolbar fits and uses the expected arrangement at ${width}px`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/marketplace?marketplace=Platform+Engineering');
      await expect(page.getByRole('table', { name: 'skills' })).toBeVisible();
      await page.evaluate(async () => {
        await document.fonts.ready;
      });
      const controls = overviewControls(page);
      await expectControlsFit(page, controls);
      const search = await bounds(controls[0]);
      const picker = await bounds(controls[1]);
      const sort = await bounds(controls[2]);
      if (width === 1023) {
        expect(picker.y).toBeGreaterThanOrEqual(search.y + search.height);
        expect(sort.y).toBeGreaterThanOrEqual(picker.y + picker.height);
      } else {
        expect(picker.x).toBeGreaterThanOrEqual(search.x + search.width);
        expect(sort.x).toBeGreaterThanOrEqual(picker.x + picker.width);
        expect(Math.abs(search.y + search.height / 2 - picker.y - picker.height / 2)).toBeLessThan(
          1,
        );
      }
      await tabTo(page, page.getByRole('button', { name: 'Card view' }));
      await page.keyboard.press('Space');
      await expect(page.locator('a[href^="/marketplace/"]')).toHaveCount(24);
      await expectNoPageOverflow(page);
      await testInfo.attach(`toolbar-${width}px`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }

  for (const viewport of [
    { width: 390, height: 844 },
    { width: 1440, height: 900 },
  ]) {
    test(`overview list and cards fit at ${viewport.width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize(viewport);
      await page.goto('/marketplace?marketplace=Platform+Engineering&pageSize=12&page=3');
      await expect(page.getByRole('row', { name: /^Open / })).toHaveCount(12);
      const controls = [
        ...overviewControls(page),
        page.getByRole('button', { name: 'Previous', exact: true }),
        page.getByRole('button', { name: 'Next', exact: true }),
        ...(await page.getByRole('button', { name: /^Page \d+$/ }).all()),
      ];
      await expectControlsFit(page, controls);
      await expectNoPageOverflow(page);
      await testInfo.attach(`list-${viewport.width}px`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });

      await tabTo(page, page.getByRole('button', { name: 'Card view' }));
      await page.keyboard.press('Enter');
      const cards = page.locator('a[href^="/marketplace/"]');
      await expect(cards).toHaveCount(12);
      const first = await bounds(cards.nth(0));
      const second = await bounds(cards.nth(1));
      if (viewport.width === 390) {
        expect(second.y).toBeGreaterThanOrEqual(first.y + first.height);
      } else {
        expect(second.x).toBeGreaterThanOrEqual(first.x + first.width);
        expect(Math.abs(second.y - first.y)).toBeLessThan(1);
      }
      await expectControlsFit(page, [...controls, ...(await cards.all())]);
      await expectNoPageOverflow(page);
      await testInfo.attach(`cards-${viewport.width}px`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });

    test(`detail Contents and Source fit at ${viewport.width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize(viewport);
      await page.goto('/marketplace/platform-engineering/skills/react-expert');
      await expect(page.getByRole('heading', { name: 'React Expert', exact: true })).toBeVisible();
      const contents = page
        .getByRole('heading', { name: 'Contents', exact: true })
        .locator('../..');
      const source = page
        .locator('div')
        .filter({ has: page.getByRole('heading', { name: 'Source', exact: true }) })
        .filter({ has: page.getByText('Path', { exact: true }) })
        .last();
      await expect(contents).toContainText('# React Expert');
      await expect(source).toContainText('Platform Engineering');
      await expect(source).toContainText('skills/react-expert/SKILL.md');
      await expectControlsFit(page, [contents, source]);
      const main = await bounds(contents);
      const sidebar = await bounds(source);
      if (viewport.width === 390) {
        expect(sidebar.y).toBeGreaterThanOrEqual(main.y + main.height);
      } else {
        expect(sidebar.x).toBeGreaterThanOrEqual(main.x + main.width);
        expect(Math.abs(sidebar.y - main.y)).toBeLessThan(1);
      }
      await expectNoPageOverflow(page);
      await testInfo.attach(`detail-${viewport.width}px`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: 'image/png',
      });
    });
  }
});

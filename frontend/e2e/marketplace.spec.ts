/**
 * E2E — Marketplace overview and Skill detail.
 *
 * The skills-service is a live Go backend, so Category A
 * (docs/adr/2026-05-08-frontend-test-strategy-and-toolchain.md) forbids these
 * tests from reaching it. page.route() cannot intercept the server-side RSC
 * fetch either, so the very first render of every test always shows the
 * overview's load-error state, the same way dashboard.spec.ts lets an
 * unreachable RSC fetch degrade to an empty state. load() below drives past
 * that error with the real "Try again" retry, which runs client-side through
 * the BFF and is what page.route() mocks from here on: /api/marketplace/skills
 * and /api/marketplace/skill. Those mocked responses are built from
 * marketplace-fixtures.ts with resolveOverview(), the same sort/page/total
 * rules marketplace-server.ts applies to a real skills-service response.
 */
import { test, expect, type Locator, type Page, type Route } from '@playwright/test';
import { injectSessionCookie, injectExpiredSessionCookie } from './fixtures';
import {
  MARKETPLACE_FIXTURES,
  SKILL_DETAIL_FIXTURES,
  SKILL_FIXTURES_BY_MARKETPLACE,
} from './marketplace-fixtures';

/** The fixture Marketplace "Platform Engineering" and its skills-service slug. */
const chosenMarketplace = { name: 'Platform Engineering', slug: 'platform-engineering__skills' };

const chosenBrowse = {
  marketplace: chosenMarketplace.slug,
  query: 'ter',
  page: 2,
  pageSize: 12,
  sort: 'name-desc',
};

// ── Marketplace route mocks ──────────────────────────────────────────────────
//
// Mirrors marketplace-server.ts's loadMarketplaceOverview: pick the requested
// Marketplace, falling back to the first one; filter by search; sort by name;
// cut the page; count the total.

function resolveOverview(query: {
  marketplace: string;
  query: string;
  page: number;
  pageSize: number;
  sort: string;
}) {
  const known = MARKETPLACE_FIXTURES.some((m) => m.marketplaceSlug === query.marketplace);
  const marketplaceSlug = known ? query.marketplace : MARKETPLACE_FIXTURES[0].marketplaceSlug;
  const all = SKILL_FIXTURES_BY_MARKETPLACE[marketplaceSlug] ?? [];
  const needle = query.query.toLowerCase();
  const matched = needle ? all.filter((skill) => skill.name.toLowerCase().includes(needle)) : all;
  const ordered = [...matched].sort((a, b) =>
    query.sort === 'name-desc' ? b.name.localeCompare(a.name) : a.name.localeCompare(b.name),
  );
  const lastPage = Math.max(1, Math.ceil(ordered.length / query.pageSize));
  const page = Math.min(Math.max(1, query.page), lastPage);
  const start = (page - 1) * query.pageSize;

  return {
    marketplaces: MARKETPLACE_FIXTURES,
    requested: {
      marketplace: query.marketplace,
      query: query.query,
      page: query.page,
      pageSize: query.pageSize,
      sort: query.sort,
    },
    browse: {
      marketplace: marketplaceSlug,
      query: query.query,
      page,
      pageSize: query.pageSize,
      sort: query.sort,
    },
    skills: ordered.slice(start, start + query.pageSize),
    total: ordered.length,
  };
}

/** Installs the default, always-succeeding Marketplace route mocks. */
async function mockMarketplaceRoutes(page: Page) {
  await page.route('**/api/marketplace/skills*', (route: Route) => {
    const url = new URL(route.request().url());
    const overview = resolveOverview({
      marketplace: url.searchParams.get('marketplace') ?? '',
      query: url.searchParams.get('query') ?? '',
      page: Number(url.searchParams.get('page') ?? '1'),
      pageSize: Number(url.searchParams.get('pageSize') ?? '24'),
      sort: url.searchParams.get('sort') ?? 'name-asc',
    });
    return route.fulfill({ json: { data: overview } });
  });

  // The bare "?" is a glob wildcard for one character, not the query-string
  // mark, so it must be escaped: unescaped, this pattern also matches
  // /api/marketplace/skills?... (the plural "s" satisfies the wildcard).
  await page.route('**/api/marketplace/skill\\?*', (route: Route) => {
    const url = new URL(route.request().url());
    const marketplaceSlug = url.searchParams.get('marketplace') ?? '';
    const skillSlug = url.searchParams.get('skill') ?? '';
    const skill = SKILL_DETAIL_FIXTURES.find(
      (candidate) =>
        candidate.skillSlug === skillSlug && skillSlug.startsWith(`${marketplaceSlug}__`),
    );
    if (!skill) {
      return route.fulfill({
        status: 404,
        json: { code: 'not_found', message: 'Skill not found' },
      });
    }
    return route.fulfill({ json: { data: skill } });
  });
}

/**
 * Clicks "Try again" and waits for the overview it loads.
 *
 * The dev server's hydration occasionally cancels this first client-side
 * fetch outside the app's own retry path (not an app bug: a cancelled
 * request is deliberately left for a newer request to replace, but no newer
 * request exists here). That leaves the overview stuck loading, with no
 * "Try again" to click. A fresh reload gives hydration a clean second try.
 */
async function retryLoad(page: Page) {
  const picker = page.getByRole('button', { name: /^Marketplace:/ });
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await page.getByRole('button', { name: 'Try again' }).click();
    try {
      await expect(picker).toBeVisible({ timeout: 5_000 });
      return;
    } catch (err) {
      if (attempt === 2) throw err;
      await page.reload();
    }
  }
}

/** Navigates to a Marketplace page and resolves its forced initial load error. */
async function load(page: Page, path = '/marketplace') {
  await page.goto(path);
  await retryLoad(page);
}

/** Reloads the page and resolves its forced initial load error, like load(). */
async function reload(page: Page) {
  await page.reload();
  await retryLoad(page);
}

async function expectCleanUrl(page: Page, pathname = '/marketplace') {
  await expect(page).toHaveURL((url) => url.pathname === pathname && url.search === '');
}

async function expectChosenOverview(page: Page) {
  await expectCleanUrl(page);
  await expect(
    page.getByRole('button', { name: `Marketplace: ${chosenMarketplace.name}`, exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Search Skills' })).toHaveValue(
    chosenBrowse.query,
  );
  await expect(page.getByRole('combobox', { name: 'Sort skills' })).toHaveValue(chosenBrowse.sort);
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

async function expectDefaultOverview(page: Page) {
  await expectCleanUrl(page);
  await expect(
    page.getByRole('button', { name: 'Marketplace: ClearRoute Core', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Search Skills' })).toHaveValue('');
  await expect(page.getByRole('combobox', { name: 'Sort skills' })).toHaveValue('name-asc');
  await expect(page.getByRole('combobox', { name: 'Results per page' })).toHaveValue('24');
  await expect(page.getByRole('button', { name: 'Page 1', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await expect(page.getByText('1–6 of 6', { exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /^Open / })).toHaveCount(6);
  await expect(page.getByRole('row', { name: /^Open / }).first()).toHaveAccessibleName(
    'Open Change Freeze Checker',
  );
}

async function choosePlatformEngineering(page: Page) {
  await page.getByRole('button', { name: 'Marketplace: ClearRoute Core', exact: true }).click();
  await page.getByRole('radio', { name: 'Platform Engineering' }).click();
  await expect(page.getByRole('row', { name: 'Open Airflow Access Review' })).toBeVisible();
  await expectCleanUrl(page);
}

async function chooseOverviewState(page: Page) {
  await choosePlatformEngineering(page);
  await page.getByRole('combobox', { name: 'Results per page' }).selectOption('12');
  await expect(page.getByText('1–12 of 61', { exact: true })).toBeVisible();
  // This search matches fourteen Skills, so all five browse values can be non-default.
  await page.getByRole('textbox', { name: 'Search Skills' }).fill(chosenBrowse.query);
  await expect(page.getByText('1–12 of 14', { exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Sort skills' }).selectOption(chosenBrowse.sort);
  await expect(page.getByRole('row', { name: /^Open / }).first()).toHaveAccessibleName(
    'Open Terraform Module Reviewer',
  );
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expectChosenOverview(page);
}

test.describe('/marketplace — overview and Skill detail', () => {
  test.beforeEach(async ({ page }) => {
    await injectSessionCookie(page.context());
    await page.route('**/api/preferences', (route) => route.fulfill({ json: {} }));
    await mockMarketplaceRoutes(page);
  });

  test('browsing keeps a clean address, opens a slug-only Skill and returns to defaults', async ({
    page,
  }) => {
    const browseRequests: URL[] = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/marketplace/skills') browseRequests.push(url);
    });
    await load(page);
    // No saved preference exists yet, so the first Marketplace in name order loads.
    await expectDefaultOverview(page);
    const initialHistoryLength = await page.evaluate(() => window.history.length);
    await chooseOverviewState(page);
    expect(await page.evaluate(() => window.history.length)).toBe(initialHistoryLength);
    const finalRequest = browseRequests.at(-1);
    expect(finalRequest).toBeDefined();
    expect(finalRequest && Object.fromEntries(finalRequest.searchParams)).toEqual({
      marketplace: chosenBrowse.marketplace,
      query: chosenBrowse.query,
      page: '2',
      pageSize: '12',
      sort: chosenBrowse.sort,
    });

    const skillRequest = page.waitForRequest(
      (request) => new URL(request.url()).pathname === '/api/marketplace/skill',
    );
    await page.getByRole('row', { name: 'Open Budget Forecaster' }).click();

    // The Skill opens in place: the address and the history do not change.
    const skillParams = new URL((await skillRequest).url()).searchParams;
    expect(skillParams.get('marketplace')).toBe(chosenMarketplace.slug);
    expect(skillParams.get('skill')).toBe('platform-engineering__skills__budget-forecaster');
    await expect(page.getByRole('heading', { name: 'Budget Forecaster' })).toBeVisible();
    await expect(
      page.getByText('Projects month-end cloud spend from the current burn rate.', { exact: true }),
    ).toBeVisible();
    await expectCleanUrl(page);
    expect(await page.evaluate(() => window.history.length)).toBe(initialHistoryLength);
    const skillsButton = page.getByRole('button', { name: 'Skills', exact: true });
    await expect(skillsButton).toBeFocused();

    // Closing the Skill shows the same results, from memory.
    await skillsButton.click();
    await expectChosenOverview(page);

    // A reload forgets the open Skill and the browse values.
    await page.getByRole('row', { name: 'Open Budget Forecaster' }).click();
    await expect(page.getByRole('heading', { name: 'Budget Forecaster' })).toBeVisible();
    await reload(page);
    await expectDefaultOverview(page);
  });

  test('an old Skill address opens the default overview', async ({ page }) => {
    await load(page, '/marketplace/platform-engineering%2Fskills%2Fbudget-forecaster');

    await expectDefaultOverview(page);
  });

  test('old browse queries are ignored and a refresh resets in-memory browse values', async ({
    page,
  }) => {
    await load(
      page,
      '/marketplace?marketplace=Platform+Engineering&query=ter&page=2&pageSize=12&sort=name-desc',
    );
    await expectDefaultOverview(page);
    await chooseOverviewState(page);

    await reload(page);

    await expectDefaultOverview(page);
  });

  test('a Skill that no longer exists shows a not-found message with a way back', async ({
    page,
  }) => {
    await load(page);
    await page.route('**/api/marketplace/skill\\?*', (route: Route) =>
      route.fulfill({ status: 404, json: { code: 'not_found', message: 'Skill not found' } }),
    );
    await page.getByRole('row', { name: 'Open Change Freeze Checker' }).click();

    await expect(page.getByText('Skill not found')).toBeVisible();
    await page.getByRole('button', { name: 'Back to Skills' }).click();
    await expectDefaultOverview(page);
  });

  test('a later browse change that fails keeps the previous results and offers a retry', async ({
    page,
  }) => {
    await load(page);
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
    await mockMarketplaceRoutes(page);
  });

  test('search, Marketplace selection, sort and both views work through the keyboard', async ({
    page,
  }) => {
    await load(page);
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
    await expect(page.getByRole('main').getByRole('button', { name: /^Open / })).toHaveCount(2);

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
    await load(page);
    await choosePlatformEngineering(page);
    await page.getByRole('combobox', { name: 'Results per page' }).selectOption('12');
    await expect(page.getByText('1–12 of 61', { exact: true })).toBeVisible();
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
    await load(page);
    // Counts only from here: load() may itself need more than one attempt
    // (see retryLoad's comment), so counting from test start would be
    // brittle. The next request is the ArrowUp Marketplace switch below.
    let requests = 0;
    await page.route('**/api/marketplace/skills*', async (route) => {
      requests += 1;
      if (requests === 1) {
        await route.fulfill({ status: 503, json: { message: 'Temporary catalogue failure.' } });
      } else {
        // The manual retry below falls back to the good-data mock.
        await route.fallback();
      }
    });
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
      await load(page);
      await choosePlatformEngineering(page);
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
      await expect(page.getByRole('main').getByRole('button', { name: /^Open / })).toHaveCount(24);
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
      await load(page);
      await choosePlatformEngineering(page);
      await page.getByRole('combobox', { name: 'Results per page' }).selectOption('12');
      await expect(page.getByText('1–12 of 61', { exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Page 3', exact: true }).click();
      await expect(page.getByText('25–36 of 61', { exact: true })).toBeVisible();
      await expectCleanUrl(page);
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
      const cards = page.getByRole('main').getByRole('button', { name: /^Open / });
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
      await load(page);
      await choosePlatformEngineering(page);
      await page.getByRole('textbox', { name: 'Search Skills' }).fill('React Expert');
      await page.getByRole('row', { name: 'Open React Expert' }).click();
      await expect(page.getByRole('heading', { name: 'React Expert', exact: true })).toBeVisible();
      await expectCleanUrl(page);
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

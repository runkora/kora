/**
 * E2E: the Skills overview and skill page against skills-upstream.mjs, which stands in for
 * skills-service behind the real Server Components and the real /api/skills BFF routes.
 */
import { test, expect, type Page } from '@playwright/test';
import { injectSessionCookie } from './fixtures';
import { BROKEN, PARTNER, TEAM, UNAVAILABLE_MESSAGE } from './skills-fixtures';

const rows = (page: Page) => page.getByRole('row', { name: /^Open / });

/** Cards are the default view; these tests read the table. */
const showList = (page: Page) => page.getByRole('button', { name: 'List view' }).click();
const chip = (page: Page, label: string | RegExp) =>
  page.getByRole('button', { name: label }).first();

test.beforeEach(async ({ context }) => {
  await injectSessionCookie(context);
});

test('/skills lists every Marketplace in title order, ten a page, and names the one it left out', async ({
  page,
}) => {
  await page.goto('/skills');
  await showList(page);

  await expect(page.getByText('32 skills', { exact: true })).toBeVisible();
  await expect(rows(page)).toHaveCount(10);
  await expect(rows(page).first()).toHaveAccessibleName('Open Café notes');
  await expect(page.getByText('1–10 of 32', { exact: true })).toBeVisible();
  await expect(page.getByText(`${BROKEN.repository}: ${UNAVAILABLE_MESSAGE}`)).toBeVisible();
});

test('the URL holds the Marketplace, search and page, and Back from a skill restores them', async ({
  page,
}) => {
  await page.goto('/skills');
  await showList(page);

  await chip(page, new RegExp(`^${TEAM.repository}`)).click();
  await expect(page).toHaveURL(`/skills?marketplace=${TEAM.slug}`);
  await page.getByRole('textbox', { name: 'Search skills' }).fill('review');
  await expect(page).toHaveURL(`/skills?marketplace=${TEAM.slug}&search=review`);
  await expect(page.getByText('1–10 of 28', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(page).toHaveURL(`/skills?marketplace=${TEAM.slug}&search=review&page=2`);
  await expect(page.getByText('11–20 of 28', { exact: true })).toBeVisible();

  await rows(page).first().click();
  await expect(page).toHaveURL(
    `/skills/${TEAM.slug}/skills/review-11?marketplace=${TEAM.slug}&search=review&page=2`,
  );
  await expect(page.getByRole('link', { name: 'Skills', exact: true })).toHaveAttribute(
    'href',
    `/skills?marketplace=${TEAM.slug}&search=review&page=2`,
  );

  await page.goBack();
  await expect(page).toHaveURL(`/skills?marketplace=${TEAM.slug}&search=review&page=2`);
  await expect(page.getByText('11–20 of 28', { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByText('11–20 of 28', { exact: true })).toBeVisible();
});

test('a favourited skill shows in the Favourites view', async ({ page, context }) => {
  // Its own session token gives it a preferences document no other test writes.
  await context.clearCookies();
  await injectSessionCookie(context, `favourites-${Date.now()}`);
  await page.goto(`/skills?marketplace=${PARTNER.slug}`);

  await page.getByRole('button', { name: 'Add to favourites' }).last().click();
  const favourites = page.waitForResponse((res) => res.url().includes('/api/skills?'));
  await chip(page, /^Favourites/).click();
  expect((await (await favourites).json()).data.total).toBe(1);
  await expect(page).toHaveURL('/skills?favourites=1');

  // The server render reads the stored favourites: a reload proves they were saved.
  await page.reload();
  await expect(page.getByText('1 skill', { exact: true })).toBeVisible();
  await expect(page.getByText('Linter', { exact: true })).toBeVisible();
});

test('an unavailable Marketplace says so and offers a retry', async ({ page }) => {
  await page.goto(`/skills?marketplace=${BROKEN.slug}`);

  await expect(page.getByText('This Marketplace is unavailable.', { exact: true })).toBeVisible();
  await expect(page.getByRole('main').getByText(UNAVAILABLE_MESSAGE)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
});

test('a skill page renders SKILL.md, loads another file on click and names its source', async ({
  page,
}) => {
  await page.goto(`/skills/${TEAM.slug}/skills/pdf`);

  await expect(page.getByRole('heading', { name: 'PDF Processing Guide' })).toBeVisible();
  await expect(page.getByText('Use pdf when the job needs it.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'skills/pdf' })).toHaveAttribute(
    'href',
    `https://github.com/${TEAM.repository}/tree/${TEAM.commit}/skills/pdf`,
  );

  await page.getByRole('button', { name: 'reference folder' }).click();
  await page.getByRole('button', { name: /notes\.md/ }).click();
  await expect(page.getByText('Notes for pdf.')).toBeVisible();

  await page.getByRole('link', { name: 'Skills', exact: true }).click();
  await expect(page).toHaveURL('/skills');
});

test('a skill whose folder needs encoding opens from its row', async ({ page }) => {
  await page.goto(`/skills?marketplace=${PARTNER.slug}`);
  await showList(page);

  await rows(page).filter({ hasText: 'Café notes' }).click();

  await expect(page).toHaveURL(
    `/skills/${PARTNER.slug}/tools/caf%C3%A9%20notes?marketplace=${PARTNER.slug}`,
  );
  // The first visit compiles the skill route in the dev server, which can take a while.
  await expect(page.getByRole('heading', { name: 'Café notes' })).toBeVisible({ timeout: 20_000 });
});

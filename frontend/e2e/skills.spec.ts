/**
 * E2E: the Skills overview and skill page against skills-upstream.mjs, which stands in for
 * skills-service behind the real Server Components and the real /api/skills BFF route.
 */
import { test, expect, type Page } from '@playwright/test';
import { injectSessionCookie } from './fixtures';
import { BROKEN, PARTNER, TEAM, UNAVAILABLE_MESSAGE } from './skills-fixtures';

const rows = (page: Page) => page.getByRole('row', { name: /^Open / });

/** Cards are the default view; these tests read the table. */
const showList = (page: Page) => page.getByRole('button', { name: 'List view' }).click();

test.beforeEach(async ({ context }) => {
  await injectSessionCookie(context);
});

test('/skills opens the first Marketplace and lists its skills A to Z', async ({ page }) => {
  await page.goto('/skills');
  await showList(page);

  await expect(page).toHaveURL(`/skills/${TEAM.slug}`);
  await expect(page.getByRole('button', { name: `Marketplace: ${TEAM.name}` })).toBeVisible();
  await expect(page.getByText('1–25 of 30', { exact: true })).toBeVisible();
  await expect(rows(page)).toHaveCount(25);
  await expect(rows(page).first()).toHaveAccessibleName('Open docx');
});

test('the URL holds search and page, and Back from a skill restores them', async ({ page }) => {
  await page.goto(`/skills/${TEAM.slug}`);
  await showList(page);

  await page.getByRole('textbox', { name: 'Search skills' }).fill('review');
  await expect(page).toHaveURL(`/skills/${TEAM.slug}?search=review`);
  await expect(page.getByText('1–25 of 28', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Page 2', exact: true }).click();
  await expect(page).toHaveURL(`/skills/${TEAM.slug}?search=review&page=2`);
  await expect(rows(page)).toHaveCount(3);

  await rows(page).first().click();
  await expect(page).toHaveURL(`/skills/${TEAM.slug}/skills/review-26?search=review&page=2`);
  await expect(page.getByRole('link', { name: 'Skills', exact: true })).toHaveAttribute(
    'href',
    `/skills/${TEAM.slug}?search=review&page=2`,
  );
  await expect(page.getByRole('heading', { name: 'File Explorer', exact: true })).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(`/skills/${TEAM.slug}?search=review&page=2`);
  await expect(page.getByRole('textbox', { name: 'Search skills' })).toHaveValue('review');
  await expect(page.getByText('26–28 of 28', { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByText('26–28 of 28', { exact: true })).toBeVisible();
});

test('choosing a Marketplace changes the path and resets the page', async ({ page }) => {
  await page.goto(`/skills/${TEAM.slug}?page=2`);
  await showList(page);

  await page.getByRole('button', { name: `Marketplace: ${TEAM.name}` }).click();
  await page.getByRole('radio', { name: new RegExp(PARTNER.name) }).click();

  await expect(page).toHaveURL(`/skills/${PARTNER.slug}`);
  await expect(rows(page)).toHaveCount(2);
  await expect(rows(page).last()).toHaveAccessibleName('Open linter');
});

test('an unavailable Marketplace says so and offers a retry', async ({ page }) => {
  await page.goto(`/skills/${BROKEN.slug}`);

  await expect(page.getByText('This Marketplace is unavailable.', { exact: true })).toBeVisible();
  await expect(page.getByText(UNAVAILABLE_MESSAGE)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
  await expect(page.getByRole('button', { name: `Marketplace: ${BROKEN.name}` })).toBeVisible();
});

test('a skill page shows SKILL.md first, its other files and the repository', async ({ page }) => {
  await page.goto(`/skills/${TEAM.slug}/skills/pdf`);

  await expect(page.getByRole('figure')).toContainText('# pdf');
  await expect(page.getByText(TEAM.repository, { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'reference', exact: false }).click();
  await page.getByRole('button', { name: /notes\.md/ }).click();
  await expect(page.getByRole('figure')).toContainText('Notes for pdf.');

  await page.getByRole('link', { name: 'Skills', exact: true }).click();
  await expect(page).toHaveURL(`/skills/${TEAM.slug}`);
});

test('a skill whose folder needs encoding opens from its row', async ({ page }) => {
  await page.goto(`/skills/${PARTNER.slug}`);
  await showList(page);

  await rows(page).filter({ hasText: 'café notes' }).click();

  await expect(page).toHaveURL(`/skills/${PARTNER.slug}/tools/caf%C3%A9%20notes`);
  await expect(page.getByRole('figure')).toContainText('# café notes');
});

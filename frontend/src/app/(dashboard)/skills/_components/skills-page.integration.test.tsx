/** Integration tests for the Skills overview states, rendered through SkillsShell with MSW. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@runkora/test-utils';
import { SkillsShell } from './skills-shell';
import { MARKETPLACE_FIXTURES, storyOverview } from './skills-story-fixtures';
import { browseQuery, type SkillsLoadError, type SkillsOverview } from '../_types';

const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push, refresh: vi.fn() }) }));

/** The address matches the browse values, as after a server render, so nothing is restored. */
function renderPage(overview: SkillsOverview, error: SkillsLoadError | null = null) {
  window.history.replaceState(null, '', `/skills${browseQuery(overview.browse)}`);
  return renderWithProviders(<SkillsShell initialOverview={overview} initialError={error} />);
}

function desktop() {
  vi.spyOn(window, 'matchMedia').mockReturnValue({
    matches: true,
    addEventListener: () => {},
    removeEventListener: () => {},
  } as unknown as MediaQueryList);
}

beforeEach(() => {
  push.mockReset();
  window.history.replaceState(null, '', '/skills');
});
afterEach(() => vi.restoreAllMocks());

describe('SkillsPage', () => {
  it('shows a chip per Marketplace with its count, and none while a count is unknown', () => {
    const unread = { ...MARKETPLACE_FIXTURES[2], skillCount: null };
    renderPage({
      ...storyOverview(),
      marketplaces: [MARKETPLACE_FIXTURES[0], MARKETPLACE_FIXTURES[1], unread],
    });

    expect(screen.getByRole('button', { name: /^anthropics\/skills\s*12$/ })).toBeVisible();
    expect(screen.getByRole('button', { name: /^mattpocock\/skills$/ })).toBeVisible();
    expect(screen.getByRole('button', { name: /^All$/ })).toBeVisible();
    expect(screen.getByText('3 Marketplaces')).toBeVisible();
  });

  it('asks for three characters, counted in code points, before it searches', async () => {
    renderPage(storyOverview());

    await userEvent.type(screen.getByRole('textbox', { name: 'Search skills' }), '👍👍');

    expect(screen.getByText('Type at least 3 characters to search.')).toBeVisible();
  });

  it('opens cards on a phone, ten a page, each linking to its skill with the way back', () => {
    renderPage(storyOverview({ search: 'ing' }));

    expect(screen.queryByRole('button', { name: 'List view' })).not.toBeInTheDocument();
    expect(screen.getByText(/^1–\d+ of \d+$/)).toBeVisible();
    const card = screen.getByText('PDF Processing Guide').closest('[data-interactive]');
    expect(within(card as HTMLElement).getByRole('link', { name: /Details/ })).toHaveAttribute(
      'href',
      '/skills/anthropic-skills/skills/pdf?search=ing',
    );
  });

  it('opens a table row on a desktop after switching to the list view', async () => {
    desktop();
    renderPage(storyOverview());

    await userEvent.click(screen.getByRole('button', { name: 'List view' }));
    await userEvent.click(screen.getByRole('row', { name: 'Open Anthropic Brand Styling' }));

    expect(push).toHaveBeenCalledWith('/skills/anthropic-skills/skills/brand-guidelines');
  });

  it('names a Marketplace the all-Marketplaces page left out', () => {
    renderPage({
      ...storyOverview(),
      failures: [
        { marketplace: 'mattpocock-skills', message: 'This Marketplace could not be loaded.' },
      ],
    });

    expect(
      screen.getByText('mattpocock/skills: This Marketplace could not be loaded.'),
    ).toBeVisible();
  });

  it('offers no view toggle in an error state', () => {
    desktop();
    renderPage(
      { ...storyOverview({ marketplace: 'anthropic-skills' }), skills: [], total: 0 },
      { status: 502, message: 'This Marketplace could not be loaded. Try again later.' },
    );

    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Card view' })).not.toBeInTheDocument();
  });

  it('says how to add a favourite when the Favourites view is empty', () => {
    renderPage(storyOverview({ favourites: true }));

    expect(screen.getByText('No favourite skills')).toBeVisible();
  });

  it('says so when the address names no known Marketplace', () => {
    renderPage(storyOverview({ marketplace: 'gone' }));

    expect(screen.getByText('Marketplace not found')).toBeVisible();
  });

  it('says so when the tenant has no Marketplaces', () => {
    renderPage({ ...storyOverview(), marketplaces: [], skills: [], total: 0 });

    expect(screen.getByText('No Marketplaces available')).toBeVisible();
  });
});

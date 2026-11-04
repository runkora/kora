/** Integration tests for the Skills overview states, rendered through SkillsShell with MSW. */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@runkora/test-utils';
import { SkillsShell } from './skills-shell';
import { storyOverview, storyPage, TEAM_MARKETPLACE } from './skills-story-fixtures';

const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push, refresh: vi.fn() }) }));

const OVERVIEW = `/skills/${TEAM_MARKETPLACE.slug}`;

beforeEach(() => {
  push.mockReset();
  window.history.replaceState(null, '', OVERVIEW);
});

describe('SkillsPage', () => {
  it('opens cards by default and, on a phone, offers no table', () => {
    renderWithProviders(<SkillsShell initialOverview={storyOverview()} initialError={null} />);

    expect(screen.getByText('1–3 of 30')).toBeVisible();
    expect(screen.getByRole('link', { name: /pdf/ })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'List view' })).not.toBeInTheDocument();
  });

  describe('on a desktop', () => {
    beforeEach(() => {
      vi.spyOn(window, 'matchMedia').mockReturnValue({
        matches: true,
        addEventListener: () => {},
        removeEventListener: () => {},
      } as unknown as MediaQueryList);
      window.history.replaceState(null, '', `${OVERVIEW}?search=pdf&page=2`);
    });
    afterEach(() => vi.restoreAllMocks());

    const searched = () =>
      storyOverview({ browse: { marketplace: TEAM_MARKETPLACE.slug, search: 'pdf', page: 2 } });

    it('links each card to its skill with the search and page to return to', () => {
      renderWithProviders(<SkillsShell initialOverview={searched()} initialError={null} />);

      expect(screen.getByRole('link', { name: /pdf/ })).toHaveAttribute(
        'href',
        `${OVERVIEW}/skills/pdf?search=pdf&page=2`,
      );
    });

    it('opens a table row the same way after switching to the list view', async () => {
      renderWithProviders(<SkillsShell initialOverview={searched()} initialError={null} />);

      await userEvent.click(screen.getByRole('button', { name: 'List view' }));
      await userEvent.click(screen.getByRole('row', { name: 'Open pdf' }));

      expect(push).toHaveBeenCalledWith(`${OVERVIEW}/skills/pdf?search=pdf&page=2`);
    });

    it('offers no view toggle in an error state', () => {
      renderWithProviders(
        <SkillsShell
          initialOverview={searched()}
          initialError={{ status: 502, message: 'No credential.' }}
        />,
      );

      expect(screen.queryByRole('button', { name: 'Card view' })).not.toBeInTheDocument();
    });
  });

  it('shows an unavailable Marketplace with its reason, and Try again fetches the page again', async () => {
    server.use(http.get('/api/skills', () => HttpResponse.json({ data: storyPage() })));
    renderWithProviders(
      <SkillsShell
        initialOverview={storyOverview({ skills: [], total: 0 })}
        initialError={{
          status: 502,
          message: 'This Marketplace could not be loaded. Try again later.',
        }}
      />,
    );

    expect(screen.getByText('This Marketplace is unavailable.')).toBeVisible();
    expect(
      screen.getByText('This Marketplace could not be loaded. Try again later.'),
    ).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));

    await waitFor(() => expect(screen.getByText('1–3 of 30')).toBeVisible());
  });

  it('offers to clear a search that matches nothing', async () => {
    server.use(http.get('/api/skills', () => HttpResponse.json({ data: storyPage() })));
    window.history.replaceState(null, '', `${OVERVIEW}?search=nothing`);
    renderWithProviders(
      <SkillsShell
        initialOverview={storyOverview({
          browse: { marketplace: TEAM_MARKETPLACE.slug, search: 'nothing', page: 1 },
          skills: [],
          total: 0,
        })}
        initialError={null}
      />,
    );

    await userEvent.click(screen.getByRole('button', { name: 'Clear search' }));

    expect(window.location.search).toBe('');
    await waitFor(() => expect(screen.getByText('1–3 of 30')).toBeVisible());
  });

  it('says so when the address names no known Marketplace', () => {
    window.history.replaceState(null, '', '/skills/gone');
    renderWithProviders(
      <SkillsShell
        initialOverview={storyOverview({
          browse: { marketplace: 'gone', search: '', page: 1 },
          skills: [],
          total: 0,
        })}
        initialError={null}
      />,
    );

    expect(screen.getByText('Marketplace not found')).toBeVisible();
  });

  it('says so when the tenant has no Marketplaces', () => {
    renderWithProviders(
      <SkillsShell
        initialOverview={storyOverview({ marketplaces: [], skills: [], total: 0 })}
        initialError={null}
      />,
    );

    expect(screen.getByText('No Marketplaces available')).toBeVisible();
  });
});

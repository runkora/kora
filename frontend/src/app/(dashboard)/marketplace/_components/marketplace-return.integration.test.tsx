import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { Text } from '@orbit/hades';
import { renderWithProviders, server } from '@orbit/test-utils';
import { MarketplaceBrowseStateProvider } from '../_context/marketplace-browse-state';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { parseBrowseParams } from '../_utils/browse-params';
import { MarketplaceShell } from './marketplace-shell';

vi.mock('next/navigation', () => ({
  usePathname: () => window.location.pathname,
  useRouter: () => ({ push: vi.fn() }),
}));

function overview(overrides: Partial<MarketplaceBrowseParams> = {}): MarketplaceOverviewData {
  const browse: MarketplaceBrowseParams = {
    marketplace: 'core__skills',
    query: '',
    page: 1,
    pageSize: 24,
    sort: 'name-asc',
    ...overrides,
  };
  return {
    marketplaces: [
      { name: 'Core', marketplaceSlug: 'core__skills', link: 'https://github.com/core/skills' },
    ],
    requested: browse,
    browse,
    total: 192,
    skills: Array.from({ length: browse.pageSize }, (_, offset) => {
      const index = (browse.page - 1) * browse.pageSize + offset;
      const number = browse.sort === 'name-desc' ? 192 - index : index + 1;
      return {
        name: `Skill ${number}`,
        skillSlug: `core__skills__skill-${number}`,
        skillName: `skill-${number}`,
        description: '',
      };
    }),
  };
}

function route(
  showOverview: boolean,
  initialOverview = overview(),
  initialError: string | null = null,
) {
  return (
    <MarketplaceBrowseStateProvider>
      {showOverview ? (
        <MarketplaceShell initialOverview={initialOverview} initialError={initialError} />
      ) : (
        <Text>Skill detail</Text>
      )}
    </MarketplaceBrowseStateProvider>
  );
}

afterEach(() => window.history.replaceState(null, '', '/'));

describe('returning from a Skill', () => {
  const browseRequests: URL[] = [];

  beforeEach(() => {
    browseRequests.length = 0;
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        const url = new URL(request.url);
        browseRequests.push(url);
        return HttpResponse.json({
          data: overview(parseBrowseParams(url.searchParams)),
        });
      }),
    );
    window.history.replaceState(null, '', '/marketplace/core__skills');
  });

  it('keeps page 5 of 8 when the overview remounts with fresh server defaults', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));

    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');
    await user.click(screen.getByRole('button', { name: 'Page 5' }));
    await screen.findByText('97–120 of 192');
    await user.click(screen.getByRole('row', { name: 'Open Skill 97' }));

    window.history.replaceState(null, '', '/marketplace/core__skills/skill-97');
    rerender(route(false));
    const refreshedPage = overview({ page: 5 });
    refreshedPage.skills[0] = { ...refreshedPage.skills[0], name: 'Updated Skill 97' };
    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        browseRequests.push(new URL(request.url));
        return HttpResponse.json({ data: refreshedPage });
      }),
    );
    window.history.replaceState(null, '', '/marketplace/core__skills');
    rerender(route(true));

    await screen.findByRole('row', { name: 'Open Updated Skill 97' });
    expect(screen.getByText('97–120 of 192')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Page 5' })).toHaveAttribute('aria-current', 'page');
    expect(browseRequests.at(-1)?.searchParams.get('page')).toBe('5');
  });

  it('restores search and sort and keeps 24 rows for the next page request', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));

    await user.type(screen.getByRole('textbox', { name: 'Search Skills' }), 'Skill');
    await waitFor(() => expect(browseRequests.at(-1)?.searchParams.get('query')).toBe('Skill'));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort skills' }), 'name-desc');
    await screen.findByRole('row', { name: 'Open Skill 192' });
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    window.history.replaceState(null, '', '/marketplace/core__skills/skill-144');
    rerender(route(false));
    window.history.replaceState(null, '', '/marketplace/core__skills');
    rerender(route(true));

    expect(screen.getByRole('textbox', { name: 'Search Skills' })).toHaveValue('Skill');
    expect(screen.queryByRole('combobox', { name: 'Results per page' })).not.toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Sort skills' })).toHaveValue('name-desc');
    await screen.findByText('49–72 of 192');
    await user.click(screen.getByRole('button', { name: 'Next' }));
    await screen.findByText('73–96 of 192');
    expect(screen.getByRole('row', { name: 'Open Skill 120' })).toBeVisible();
    expect(Object.fromEntries(browseRequests.at(-1)?.searchParams ?? [])).toEqual({
      marketplace: 'core__skills',
      query: 'Skill',
      page: '4',
      pageSize: '24',
      sort: 'name-desc',
    });
    expect(window.location.pathname + window.location.search).toBe('/marketplace/core__skills');
  });

  it('restores relevance after a Skill visit and the previous alphabetical sort when search clears', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));
    const sort = () => screen.getByRole('combobox', { name: 'Sort skills' });

    await user.selectOptions(sort(), 'name-desc');
    await screen.findByRole('row', { name: 'Open Skill 192' });
    await user.type(screen.getByRole('textbox', { name: 'Search Skills' }), 'Skill');
    await waitFor(() => expect(sort()).toHaveValue('relevance'));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    window.history.replaceState(null, '', '/marketplace/core__skills/skill-49');
    rerender(route(false));
    window.history.replaceState(null, '', '/marketplace/core__skills');
    rerender(route(true));

    await screen.findByText('49–72 of 192');
    expect(sort()).toHaveValue('relevance');
    expect(screen.getByRole('textbox', { name: 'Search Skills' })).toHaveValue('Skill');
    await user.clear(screen.getByRole('textbox', { name: 'Search Skills' }));
    await waitFor(() => expect(sort()).toHaveValue('name-desc'));
    expect(screen.getByRole('row', { name: 'Open Skill 192' })).toBeVisible();
    expect(browseRequests.at(-1)?.searchParams.has('query')).toBe(false);
    expect(browseRequests.at(-1)?.searchParams.get('pageSize')).toBe('24');
  });

  it('retries the remembered page if reloading it on return fails', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    window.history.replaceState(null, '', '/marketplace/core__skills/skill-49');
    rerender(route(false));
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({ message: 'Return failed' }, { status: 503 }),
      ),
    );
    window.history.replaceState(null, '', '/marketplace/core__skills');
    rerender(route(true));
    await screen.findByText('Return failed');

    server.use(
      http.get('/api/marketplace/skills', ({ request }) => {
        const url = new URL(request.url);
        browseRequests.push(url);
        return HttpResponse.json({
          data: overview({ page: Number(url.searchParams.get('page')) }),
        });
      }),
    );
    await user.click(screen.getByRole('button', { name: 'Try again' }));

    await screen.findByText('49–72 of 192');
    expect(browseRequests.at(-1)?.searchParams.get('page')).toBe('3');
  });

  it('starts from the defaults when the dashboard provider is mounted again', async () => {
    const user = userEvent.setup();
    const { unmount } = renderWithProviders(route(true));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    unmount();
    renderWithProviders(route(true));

    expect(screen.getByText('1–24 of 192')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute('aria-current', 'page');
  });

  it('clears retained browse state when navigating to another dashboard feature', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    window.history.replaceState(null, '', '/agents');
    rerender(route(false));
    window.history.replaceState(null, '', '/marketplace/core__skills');
    rerender(route(true));

    expect(screen.getByText('1–24 of 192')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Page 1' })).toHaveAttribute('aria-current', 'page');
  });

  it('shows a new server error instead of restoring the remembered browse settings', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    rerender(route(false));
    const unavailable = { ...overview(), skills: [], total: 0 };
    rerender(route(true, unavailable, 'Failed to load Skills'));

    expect(screen.getByRole('alert')).toHaveTextContent('Failed to load Skills');
    expect(screen.queryByRole('row', { name: 'Open Skill 49' })).not.toBeInTheDocument();
  });

  it('does not restore browse settings for a Marketplace missing from the current source list', async () => {
    const user = userEvent.setup();
    const { rerender } = renderWithProviders(route(true));
    await user.click(screen.getByRole('button', { name: 'Page 3' }));
    await screen.findByText('49–72 of 192');

    rerender(route(false));
    rerender(route(true, { ...overview(), marketplaces: [], skills: [], total: 0 }));

    expect(screen.getByText('No Marketplaces available')).toBeVisible();
    expect(screen.queryByRole('row', { name: 'Open Skill 49' })).not.toBeInTheDocument();
  });
});

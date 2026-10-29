import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { MarketplaceProvider } from '../_context/marketplace-context';
import { MarketplaceOverview } from './marketplace-overview';
import type { MarketplaceOverviewData } from '../_types';

const { push } = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
  usePathname: () => window.location.pathname,
}));

const browse = {
  marketplace: 'team__repo',
  query: 'review',
  page: 3,
  pageSize: 12,
  sort: 'name-desc' as const,
};
const overview: MarketplaceOverviewData = {
  marketplaces: [
    { name: 'Platform', marketplaceSlug: browse.marketplace, link: 'https://example.com' },
  ],
  requested: browse,
  browse,
  skills: [
    {
      name: 'Reviewer',
      description: 'Reviews code.',
      skillSlug: 'team__repo__reviewer',
      skillName: 'reviewer',
    },
  ],
  total: 30,
};
const detailHref = '/marketplace/team__repo/reviewer';

beforeEach(() => {
  push.mockReset();
  window.history.replaceState(null, '', '/marketplace');
  renderWithProviders(
    <MarketplaceProvider initialData={overview} initialError={null}>
      <MarketplaceOverview />
    </MarketplaceProvider>,
  );
});

describe('Skill detail navigation', () => {
  it('opens the detail path from a list row without copying browse state to the URL', async () => {
    await userEvent.setup().click(screen.getByRole('row', { name: 'Open Reviewer' }));
    expect(push).toHaveBeenCalledExactlyOnceWith(detailHref);
  });

  it.each(['{Enter}', ' '])('opens a focused list row with %s', async (key) => {
    screen.getByRole('row', { name: 'Open Reviewer' }).focus();
    await userEvent.setup().keyboard(key);
    expect(push).toHaveBeenCalledExactlyOnceWith(detailHref);
  });

  it('gives each card a native link to the same detail route', async () => {
    await userEvent.setup().click(screen.getByRole('button', { name: 'Card view' }));
    expect(screen.getByRole('link', { name: 'Open Reviewer' })).toHaveAttribute('href', detailHref);
  });
});

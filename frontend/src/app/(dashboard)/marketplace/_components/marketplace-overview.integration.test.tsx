import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceShell } from './marketplace-shell';
import { MarketplaceOverview } from './marketplace-overview';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceOverviewData } from '../_types';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const browse = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };
const partial: MarketplaceOverviewData = {
  marketplaces: [
    { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
    { name: 'Other', marketplaceSlug: 'Other', link: 'https://github.com/example/other' },
  ],
  requested: browse,
  browse,
  skills: [],
  total: 0,
};

afterEach(() => window.history.replaceState(null, '', '/'));

function renderFailure(data: MarketplaceOverviewData) {
  window.history.replaceState(null, '', '/marketplace');
  return renderWithProviders(
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS} loaded={false}>
      <MarketplaceShell initialOverview={data} initialError="Failed to load Skills" />
    </PreferencesProvider>,
  );
}

describe('Marketplace initial failure recovery', () => {
  it('does not claim the Marketplace is empty while retrying a failed first Skills load', async () => {
    let finishRequest!: () => void;
    const pending = new Promise<void>((resolve) => {
      finishRequest = resolve;
    });
    server.use(
      http.get('/api/marketplace/skills', async () => {
        await pending;
        return HttpResponse.json({ data: partial });
      }),
    );
    renderFailure(partial);
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(screen.getByText('Loading Skills…')).toBeVisible();
    expect(screen.queryByText('No Skills in this Marketplace')).not.toBeInTheDocument();
    finishRequest();
    expect(await screen.findByText('No Skills in this Marketplace')).toBeVisible();
  });

  it('keeps the selector and allows a healthy Marketplace to load after a Skills failure', async () => {
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json({ data: { ...partial, browse: { ...browse, marketplace: 'Other' } } }),
      ),
    );
    renderFailure(partial);
    expect(screen.getByText('Failed to load Skills')).toBeVisible();
    expect(screen.queryByText('No Skills in this Marketplace')).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Marketplace: Core' }));
    await user.click(screen.getByRole('radio', { name: 'Other' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Marketplace: Other' })).toBeVisible(),
    );
    expect(screen.queryByText('Failed to load Skills')).not.toBeInTheDocument();
  });

  it('keeps a newly loaded Marketplace list when a retry loads the list but still fails to load Skills', async () => {
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json(
          { code: 'upstream_error', message: 'Failed to load Skills', data: partial },
          { status: 502 },
        ),
      ),
    );
    renderFailure({ ...partial, marketplaces: [] });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Marketplace: Core' })).toBeVisible(),
    );
    expect(screen.getByText('Failed to load Skills')).toBeVisible();
    expect(screen.queryByText('No Skills in this Marketplace')).not.toBeInTheDocument();
  });
});

// ── MarketplaceOverview (rendered directly via MarketplaceProvider) ────────────

function browseWith(overrides: Partial<MarketplaceOverviewData['browse']> = {}) {
  return { ...browse, ...overrides };
}

function overviewWith(overrides: Partial<MarketplaceOverviewData> = {}): MarketplaceOverviewData {
  const b = browseWith();
  return {
    marketplaces: [
      { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
    ],
    requested: b,
    browse: b,
    skills: [],
    total: 0,
    ...overrides,
  };
}

const SKILL = {
  skillSlug: 'core/skills/react-expert',
  skillName: 'react-expert',
  name: 'React Expert',
  description: 'Senior React developer.',
};

function renderOverview(data: MarketplaceOverviewData, initialError: string | null = null) {
  window.history.replaceState(null, '', '/marketplace');
  return renderWithProviders(
    <MarketplaceProvider initialData={data} initialError={initialError}>
      <MarketplaceOverview />
    </MarketplaceProvider>,
  );
}

describe('result states', () => {
  it('renders populated results in the list view by default', () => {
    renderOverview(overviewWith({ skills: [SKILL], total: 1 }));

    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(screen.getByText('React Expert')).toBeInTheDocument();
  });

  it('shows the empty-Marketplace message when the Marketplace has no Skills', () => {
    renderOverview(overviewWith());

    expect(screen.getByText('No Skills in this Marketplace')).toBeInTheDocument();
  });

  it('shows the no-matches message and a Clear search action for an empty search result', () => {
    renderOverview(
      overviewWith({
        requested: browseWith({ query: 'react' }),
        browse: browseWith({ query: 'react' }),
      }),
    );

    expect(screen.getByText('No Skills match your search')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Clear search' })).toBeInTheDocument();
  });

  it('shows the no-Marketplaces empty state when there are no Marketplaces', () => {
    renderOverview(
      overviewWith({
        marketplaces: [],
        requested: browseWith({ marketplace: '' }),
        browse: browseWith({ marketplace: '' }),
      }),
    );

    expect(screen.getByText('No Marketplaces available')).toBeInTheDocument();
  });

  it('shows an error notice with Try again when the first load failed entirely', () => {
    renderOverview(
      overviewWith({
        marketplaces: [],
        requested: browseWith({ marketplace: '' }),
        browse: browseWith({ marketplace: '' }),
      }),
      'Failed to load Marketplaces',
    );

    expect(screen.getByText(/Could not load the Marketplace/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });

  it('shows a loading skeleton while retrying after a failed first load', async () => {
    let finishRequest!: () => void;
    const pending = new Promise<void>((resolve) => {
      finishRequest = resolve;
    });
    server.use(
      http.get('/api/marketplace/skills', async () => {
        await pending;
        return HttpResponse.json({ data: overviewWith({ skills: [SKILL], total: 1 }) });
      }),
    );
    renderOverview(
      overviewWith({
        marketplaces: [],
        requested: browseWith({ marketplace: '' }),
        browse: browseWith({ marketplace: '' }),
      }),
      'Failed to load Marketplaces',
    );
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: 'Try again' }));

    expect(screen.getByRole('status', { name: 'Loading the Marketplace' })).toBeInTheDocument();
    finishRequest();
    expect(await screen.findByText('React Expert')).toBeVisible();
  });
});

describe('later browse changes', () => {
  function populatedOverview() {
    return overviewWith({
      marketplaces: [
        { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
        { name: 'Other', marketplaceSlug: 'Other', link: 'https://github.com/example/other' },
      ],
      skills: [SKILL],
      total: 1,
    });
  }

  it('keeps the current results visible and shows a loading notice during a later refresh', async () => {
    let finishRequest!: () => void;
    const pending = new Promise<void>((resolve) => {
      finishRequest = resolve;
    });
    server.use(
      http.get('/api/marketplace/skills', async () => {
        await pending;
        return HttpResponse.json({
          data: overviewWith({
            marketplaces: [
              { name: 'Core', marketplaceSlug: 'Core', link: 'https://github.com/example/core' },
              { name: 'Other', marketplaceSlug: 'Other', link: 'https://github.com/example/other' },
            ],
            skills: [SKILL],
            total: 1,
            requested: browseWith({ sort: 'name-desc' }),
            browse: browseWith({ sort: 'name-desc' }),
          }),
        });
      }),
    );
    renderOverview(populatedOverview());
    const user = userEvent.setup();

    await user.selectOptions(screen.getByLabelText('Sort skills'), 'name-desc');

    expect(screen.getByText('Loading Skills…')).toBeVisible();
    expect(screen.getByText('React Expert')).toBeVisible();

    finishRequest();
    await waitFor(() => expect(screen.queryByText('Loading Skills…')).not.toBeInTheDocument());
  });

  it('shows an error notice above the existing results when a later refresh fails, with a working retry', async () => {
    server.use(
      http.get('/api/marketplace/skills', () =>
        HttpResponse.json(
          { code: 'upstream_error', message: 'Failed to load Skills' },
          { status: 500 },
        ),
      ),
    );
    renderOverview(populatedOverview());
    const user = userEvent.setup();

    await user.selectOptions(screen.getByLabelText('Sort skills'), 'name-desc');

    expect(await screen.findByText(/last successful request/)).toBeVisible();
    expect(screen.getByText('React Expert')).toBeVisible();

    server.use(
      http.get('/api/marketplace/skills', () => HttpResponse.json({ data: populatedOverview() })),
    );
    await user.click(screen.getByRole('button', { name: 'Try again' }));

    await waitFor(() =>
      expect(screen.queryByText(/last successful request/)).not.toBeInTheDocument(),
    );
  });

  it('preserves the Marketplace, page size, and chosen view when Clear search runs', async () => {
    const searching = overviewWith({
      requested: browseWith({ query: 'react', pageSize: 12 }),
      browse: browseWith({ query: 'react', pageSize: 12 }),
      skills: [],
      total: 0,
    });
    renderOverview(searching);
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: 'Card view' }));

    const cleared = overviewWith({
      skills: [SKILL],
      total: 1,
      requested: browseWith({ pageSize: 12 }),
      browse: browseWith({ pageSize: 12 }),
    });
    server.use(http.get('/api/marketplace/skills', () => HttpResponse.json({ data: cleared })));

    await user.click(screen.getByRole('button', { name: 'Clear search' }));

    expect(await screen.findByRole('link', { name: 'Open React Expert' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Marketplace: Core' })).toBeInTheDocument();
    expect(screen.getByLabelText('Results per page')).toHaveValue('12');
  });
});

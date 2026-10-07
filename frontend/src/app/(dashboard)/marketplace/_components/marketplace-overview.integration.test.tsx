import { afterEach, describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { MarketplaceShell } from './marketplace-shell';
import type { MarketplaceOverviewData } from '../_types';

const browse = { marketplace: 'Core', query: '', page: 1, pageSize: 24, sort: 'name-asc' as const };
const partial: MarketplaceOverviewData = {
  marketplaces: [{ name: 'Core' }, { name: 'Other' }],
  requested: browse,
  browse,
  skills: [],
  total: 0,
};

afterEach(() => window.history.replaceState(null, '', '/'));

function renderFailure(data: MarketplaceOverviewData) {
  window.history.replaceState(null, '', '/marketplace?marketplace=Core');
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

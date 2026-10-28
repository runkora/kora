/**
 * Integration tests for the saved Marketplace in MarketplaceProvider.
 *
 * The provider saves the Marketplace that the user picks in the picker in the
 * user preferences (`marketplace.lastSelected`), so that /marketplace without a
 * `marketplace` key opens it again, for example after a click on the sidebar entry.
 *
 * A preferences PUT replaces the whole document. So the provider must never
 * save without a pick, and never after a failed preferences read, because then
 * the client holds only the defaults.
 *
 * Strategy:
 * - A real PreferencesProvider sends the debounced PUT /api/preferences.
 * - MSW records the body of each PUT, and serves GET /api/marketplace/skills.
 * - The jsdom URL matches the server render, so the mount sends no BFF request.
 */
import { useEffect } from 'react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { act, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS, type UserPreferences } from '../../_preferences/preferences.schema';
import type { MarketplaceStateShape } from '../_hooks/use-marketplace';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { MarketplaceProvider, useMarketplaceContext } from './marketplace-context';

// ── Fixtures ──────────────────────────────────────────────────────────────────

const MARKETPLACES = [
  {
    name: 'ClearRoute Core',
    marketplaceSlug: 'ClearRoute Core',
    link: 'https://github.com/example/clearroute-core',
  },
  {
    name: 'Platform Engineering',
    marketplaceSlug: 'Platform Engineering',
    link: 'https://github.com/example/platform-engineering',
  },
];

const CORE_BROWSE: MarketplaceBrowseParams = {
  marketplace: 'ClearRoute Core',
  query: '',
  page: 1,
  pageSize: 24,
  sort: 'name-asc',
};

/** A server render of "ClearRoute Core". */
const CORE_RENDER: MarketplaceOverviewData = {
  marketplaces: MARKETPLACES,
  requested: CORE_BROWSE,
  browse: CORE_BROWSE,
  skills: [],
  total: 0,
};

const CORE_URL = '/marketplace?marketplace=ClearRoute+Core';

const PLATFORM_BROWSE: MarketplaceBrowseParams = {
  ...CORE_BROWSE,
  marketplace: 'Platform Engineering',
};

/** The BFF result after the user picks "Platform Engineering". */
const PLATFORM_RESULT: MarketplaceOverviewData = {
  marketplaces: MARKETPLACES,
  requested: PLATFORM_BROWSE,
  browse: PLATFORM_BROWSE,
  skills: [],
  total: 0,
};

/** Stored preferences of other features, which a write must keep. */
const STORED_PREFERENCES: UserPreferences = {
  ...PREFERENCE_DEFAULTS,
  analytics: { defaultPeriod: '30d' },
  modelGateway: { favouriteModels: ['bedrock/claude-sonnet'] },
};

/** Longer than the 500 ms debounce of PreferencesProvider. */
const DEBOUNCE_WAIT_MS = 700;

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Records the body of each PUT /api/preferences. */
function recordPreferenceWrites(): unknown[] {
  const bodies: unknown[] = [];
  server.use(
    http.put('/api/preferences', async ({ request }) => {
      bodies.push(await request.json());
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return bodies;
}

function serveBrowseResult(response: () => Response) {
  server.use(http.get('/api/marketplace/skills', response));
}

/** The context of the rendered provider, so that a test can pick a Marketplace. */
let captured: MarketplaceStateShape | null = null;

function CaptureContext() {
  const context = useMarketplaceContext();
  useEffect(() => {
    captured = context;
  }, [context]);
  return null;
}

function currentContext(): MarketplaceStateShape {
  if (!captured) {
    throw new Error('The provider did not render');
  }
  return captured;
}

function providerTree(preferences: UserPreferences, loaded: boolean) {
  return (
    <PreferencesProvider initialPreferences={preferences} loaded={loaded}>
      <MarketplaceProvider initialData={CORE_RENDER} initialError={null}>
        <CaptureContext />
      </MarketplaceProvider>
    </PreferencesProvider>
  );
}

function renderProvider(preferences: UserPreferences, loaded: boolean) {
  return renderWithProviders(providerTree(preferences, loaded));
}

function pickMarketplace(name: string) {
  const context = currentContext();
  act(() => {
    context.selectMarketplace(name);
  });
}

function waitLongerThanDebounce() {
  return new Promise((resolve) => setTimeout(resolve, DEBOUNCE_WAIT_MS));
}

beforeEach(() => {
  window.history.replaceState(null, '', CORE_URL);
});

afterEach(() => {
  captured = null;
  window.history.replaceState(null, '', '/');
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('MarketplaceProvider saved Marketplace', () => {
  it('sends no write on a visit without a pick', async () => {
    const writes = recordPreferenceWrites();

    renderProvider(STORED_PREFERENCES, true);

    await waitLongerThanDebounce();
    expect(writes).toHaveLength(0);
  });

  it('saves a picked Marketplace and keeps the other preferences', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() => HttpResponse.json({ data: PLATFORM_RESULT }));

    renderProvider(STORED_PREFERENCES, true);
    pickMarketplace('Platform Engineering');

    await waitFor(
      () =>
        expect(writes).toEqual([
          {
            analytics: { defaultPeriod: '30d' },
            modelGateway: { favouriteModels: ['bedrock/claude-sonnet'] },
            marketplace: { lastSelected: 'Platform Engineering' },
          },
        ]),
      { timeout: 2000 },
    );
  });

  it('sends no write after a failed preferences read', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() => HttpResponse.json({ data: PLATFORM_RESULT }));

    // After a failed read, the client holds only the defaults. A write would
    // replace the stored preferences of other features with them.
    renderProvider(PREFERENCE_DEFAULTS, false);
    pickMarketplace('Platform Engineering');

    await waitFor(() => expect(currentContext().browse.marketplace).toBe('Platform Engineering'));
    await waitLongerThanDebounce();
    expect(writes).toHaveLength(0);
  });

  it('does not make fallback preferences writable after a successful server refresh', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() => HttpResponse.json({ data: PLATFORM_RESULT }));

    const { rerender } = renderProvider(PREFERENCE_DEFAULTS, false);
    // router.refresh() supplies new props while preserving the provider's state.
    rerender(providerTree(STORED_PREFERENCES, true));
    pickMarketplace('Platform Engineering');

    await waitFor(() => expect(currentContext().browse.marketplace).toBe('Platform Engineering'));
    await waitLongerThanDebounce();
    expect(writes).toHaveLength(0);
  });

  it('keeps a successfully loaded preference snapshot writable after a failed server refresh', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() => HttpResponse.json({ data: PLATFORM_RESULT }));

    const { rerender } = renderProvider(STORED_PREFERENCES, true);
    rerender(providerTree(PREFERENCE_DEFAULTS, false));
    pickMarketplace('Platform Engineering');

    await waitFor(
      () =>
        expect(writes).toEqual([
          {
            analytics: { defaultPeriod: '30d' },
            modelGateway: { favouriteModels: ['bedrock/claude-sonnet'] },
            marketplace: { lastSelected: 'Platform Engineering' },
          },
        ]),
      { timeout: 2000 },
    );
  });

  it('sends no write when the picked Marketplace fails to load', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() =>
      HttpResponse.json(
        { code: 'upstream_error', message: 'Failed to load Skills' },
        { status: 502 },
      ),
    );

    renderProvider(STORED_PREFERENCES, true);
    pickMarketplace('Platform Engineering');

    await waitFor(() => expect(currentContext().status).toBe('error'));
    await waitLongerThanDebounce();
    expect(writes).toHaveLength(0);
  });

  it('sends no write when the picked Marketplace is already saved', async () => {
    const writes = recordPreferenceWrites();
    serveBrowseResult(() => HttpResponse.json({ data: PLATFORM_RESULT }));
    const saved: UserPreferences = {
      ...STORED_PREFERENCES,
      marketplace: { lastSelected: 'Platform Engineering' },
    };

    renderProvider(saved, true);
    pickMarketplace('Platform Engineering');

    await waitFor(() => expect(currentContext().browse.marketplace).toBe('Platform Engineering'));
    await waitLongerThanDebounce();
    expect(writes).toHaveLength(0);
  });
});

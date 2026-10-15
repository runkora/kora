/**
 * Integration tests for opening a Skill from the overview.
 *
 * A Skill opens in place of the results. The address stays `/marketplace`
 * and no history entry is added. Closing the Skill shows the same results.
 *
 * Strategy:
 * - A real MarketplaceProvider serves the context. The saved Marketplace in
 *   the preferences equals the Marketplace on screen, so no PUT occurs.
 * - MSW serves the Skill detail BFF route, so the real client request runs.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type { Skill } from '@orbit/shared/api';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS, type UserPreferences } from '../../_preferences/preferences.schema';
import { MarketplaceProvider } from '../_context/marketplace-context';
import type { MarketplaceBrowseParams, MarketplaceOverviewData } from '../_types';
import { MarketplaceOverview } from './marketplace-overview';

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** Page 3 of a search in "Platform Engineering", with other values that are not defaults. */
const PAGE_3_BROWSE: MarketplaceBrowseParams = {
  marketplace: 'Platform Engineering',
  query: 'review',
  page: 3,
  pageSize: 12,
  sort: 'name-desc',
};

const REACT_SLUG = 'platform-engineering__skills__react-reviewer';
const GO_SLUG = 'platform-engineering__skills__go-reviewer';

const PAGE_3_RENDER: MarketplaceOverviewData = {
  marketplaces: [
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
  ],
  requested: PAGE_3_BROWSE,
  browse: PAGE_3_BROWSE,
  skills: [
    { skillSlug: REACT_SLUG, name: 'React Reviewer', description: 'Reviews React.' },
    { skillSlug: GO_SLUG, name: 'Go Reviewer', description: 'Reviews Go.' },
  ],
  total: 30,
};

function detail(skillSlug: string, title: string): Skill {
  const folder = skillSlug.split('__').pop() ?? skillSlug;
  return {
    name: title,
    description: `${title} details.`,
    skillSlug,
    skill: { path: `skills/${folder}/SKILL.md`, name: 'SKILL.md', content: `# ${title}` },
    documents: [],
  };
}

/** The saved Marketplace equals the Marketplace on screen, so the provider sends no PUT. */
const SAVED_PREFERENCES: UserPreferences = {
  ...PREFERENCE_DEFAULTS,
  marketplace: { lastSelected: 'Platform Engineering' },
};

// ── Helpers ───────────────────────────────────────────────────────────────────

/**
 * Serves each Skill by its skillSlug, and records the Marketplace and the
 * Skill that the browser requested, as `marketplace|skill`.
 */
function serveSkills(requested: string[] = []) {
  server.use(
    http.get('/api/marketplace/skill', ({ request }) => {
      const params = new URL(request.url).searchParams;
      const slug = params.get('skill') ?? '';
      requested.push(`${params.get('marketplace')}|${slug}`);
      const title = slug === REACT_SLUG ? 'React Reviewer' : 'Go Reviewer';
      return HttpResponse.json({ data: detail(slug, title) });
    }),
  );
  return requested;
}

function renderOverview() {
  return renderWithProviders(
    <PreferencesProvider initialPreferences={SAVED_PREFERENCES}>
      <MarketplaceProvider initialData={PAGE_3_RENDER} initialError={null}>
        <MarketplaceOverview />
      </MarketplaceProvider>
    </PreferencesProvider>,
  );
}

function expectAddress(path: string) {
  expect(window.location.pathname + window.location.search + window.location.hash).toBe(path);
}

beforeEach(() => {
  window.history.replaceState(null, '', '/marketplace');
});

afterEach(() => {
  vi.restoreAllMocks();
  window.history.replaceState(null, '', '/');
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('Opening a Skill from the overview', () => {
  it('list view: a row click opens the Skill in place without changing the address', async () => {
    const requested = serveSkills();
    const pushState = vi.spyOn(window.history, 'pushState');
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('row', { name: 'Open React Reviewer' }));

    expect(await screen.findByRole('heading', { name: 'React Reviewer' })).toBeVisible();
    expect(requested).toEqual([`Platform Engineering|${REACT_SLUG}`]);
    expectAddress('/marketplace');
    expect(pushState).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: 'Search Skills' })).not.toBeInTheDocument();
  });

  it('moves keyboard focus to the Skills control when a Skill opens', async () => {
    serveSkills();
    renderOverview();

    screen.getByRole('row', { name: 'Open React Reviewer' }).focus();
    const user = userEvent.setup();
    await user.keyboard('{Enter}');

    expect(screen.getByRole('button', { name: 'Skills' })).toHaveFocus();
  });

  it.each(['{Enter}', ' '])('card view: %s on a focused card opens the Skill', async (key) => {
    serveSkills();
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Card view' }));
    screen.getByRole('button', { name: 'Open Go Reviewer' }).focus();
    await user.keyboard(key);

    expect(await screen.findByRole('heading', { name: 'Go Reviewer' })).toBeVisible();
    expectAddress('/marketplace');
  });

  it('Skills closes the Skill and shows the same results, page, and view', async () => {
    serveSkills();
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Card view' }));
    await user.click(screen.getByRole('button', { name: 'Open React Reviewer' }));
    await screen.findByRole('heading', { name: 'React Reviewer' });

    await user.click(screen.getByRole('button', { name: 'Skills' }));

    expect(screen.getByRole('button', { name: 'Open React Reviewer' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Card view' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('textbox', { name: 'Search Skills' })).toHaveValue('review');
    expect(screen.getByText(/25.+30 of 30/)).toBeInTheDocument();
    expectAddress('/marketplace');
  });

  it('shows "Skill not found" for a 404, with a way back to the results', async () => {
    server.use(
      http.get('/api/marketplace/skill', () =>
        HttpResponse.json({ code: 'not_found', message: 'Skill not found' }, { status: 404 }),
      ),
    );
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('row', { name: 'Open React Reviewer' }));

    expect(await screen.findByText('Skill not found')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Back to Skills' }));
    expect(screen.getByRole('row', { name: 'Open React Reviewer' })).toBeVisible();
  });

  it('shows a failed load with a retry that requests the same Skill again', async () => {
    let calls = 0;
    server.use(
      http.get('/api/marketplace/skill', ({ request }) => {
        calls += 1;
        if (calls === 1) {
          return HttpResponse.json(
            { code: 'upstream_error', message: 'Failed to load the Skill' },
            { status: 502 },
          );
        }
        const slug = new URL(request.url).searchParams.get('skill') ?? '';
        return HttpResponse.json({ data: detail(slug, 'React Reviewer') });
      }),
    );
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('row', { name: 'Open React Reviewer' }));
    expect(
      await screen.findByText('Orbit could not load this Skill from the Marketplace.'),
    ).toBeVisible();

    await user.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('heading', { name: 'React Reviewer' })).toBeVisible();
    expect(calls).toBe(2);
  });

  it('ignores a late response for a Skill that the user closed', async () => {
    let finishReact!: () => void;
    const reactPending = new Promise<void>((resolve) => {
      finishReact = resolve;
    });
    server.use(
      http.get('/api/marketplace/skill', async ({ request }) => {
        const slug = new URL(request.url).searchParams.get('skill') ?? '';
        if (slug === REACT_SLUG) {
          await reactPending;
          return HttpResponse.json({ data: detail(slug, 'React Reviewer') });
        }
        return HttpResponse.json({ data: detail(slug, 'Go Reviewer') });
      }),
    );
    renderOverview();

    const user = userEvent.setup();
    await user.click(screen.getByRole('row', { name: 'Open React Reviewer' }));
    expect(screen.getByRole('status', { name: 'Loading the Skill' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Skills' }));
    await user.click(screen.getByRole('row', { name: 'Open Go Reviewer' }));
    expect(await screen.findByRole('heading', { name: 'Go Reviewer' })).toBeVisible();

    finishReact();

    await waitFor(() => expect(screen.getByRole('heading', { name: 'Go Reviewer' })).toBeVisible());
    expect(screen.queryByRole('heading', { name: 'React Reviewer' })).not.toBeInTheDocument();
  });
});

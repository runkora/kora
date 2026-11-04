/**
 * Integration tests for useSkills: each browse change writes the URL and sends one BFF
 * request, served here by MSW.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { delay, http, HttpResponse } from 'msw';
import { server } from '@runkora/test-utils';
import { PreferencesProvider } from '../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../_preferences/preferences.schema';
import { skillsPage, storyOverview } from '../_components/skills-story-fixtures';
import { toBrowse } from '../_types';
import { useSkills } from './use-skills';

const FAVOURITES = ['anthropic-skills:skills/pdf'];

/** Records each BFF request's query and answers with the fixture page. */
function serveSkills(
  respond: (query: Record<string, string>) => Response | Promise<Response> = (query) =>
    HttpResponse.json({ data: skillsPage(toBrowse(query), (query.keys ?? '').split(',')) }),
) {
  const requests: Record<string, string>[] = [];
  server.use(
    http.get('/api/skills', ({ request }) => {
      const query = Object.fromEntries(new URL(request.url).searchParams);
      requests.push(query);
      return respond(query);
    }),
    http.put('/api/preferences', () => HttpResponse.json({})),
  );
  return requests;
}

/** useSkills reads the favourites from the preferences, so it renders inside their provider. */
function renderSkills(overview = storyOverview()) {
  return renderHook(() => useSkills(overview, null), {
    wrapper: ({ children }) => (
      <PreferencesProvider
        initialPreferences={{ ...PREFERENCE_DEFAULTS, skills: { favouriteSkills: FAVOURITES } }}
      >
        {children}
      </PreferencesProvider>
    ),
  });
}

beforeEach(() => window.history.replaceState(null, '', '/skills?agent=draft-1'));
afterEach(() => vi.useRealTimers());

describe('useSkills', () => {
  it('goes to a page with one request and writes it to the URL, keeping other query keys', async () => {
    const requests = serveSkills();
    const { result } = renderSkills();

    act(() => result.current.goToPage(2));

    expect(window.location.search).toBe('?agent=draft-1&page=2');
    await waitFor(() => expect(result.current.status).toBe('success'));
    expect(requests).toEqual([{ page: '2' }]);
  });

  it('filters by a chip back on page 1, and the Favourites chip sends the favourite keys', async () => {
    const requests = serveSkills();
    window.history.replaceState(null, '', '/skills?page=2');
    const { result } = renderSkills(storyOverview({ page: 2 }));

    act(() => result.current.selectChip('partner-skills'));
    expect(window.location.search).toBe('?marketplace=partner-skills');
    act(() => result.current.selectChip(null));
    expect(window.location.search).toBe('?favourites=1');

    await waitFor(() => expect(result.current.total).toBe(1));
    expect(requests.at(-1)).toEqual({ page: '1', favourites: '1', keys: FAVOURITES.join(',') });
  });

  it('searches 300 ms after typing stops, from three characters, back on page 1', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    window.history.replaceState(null, '', '/skills?page=2');
    const requests = serveSkills();
    const { result } = renderSkills(storyOverview({ page: 2 }));

    act(() => result.current.setSearchText('pd'));
    await act(() => vi.advanceTimersByTimeAsync(300));
    expect(requests).toHaveLength(0);

    act(() => result.current.setSearchText('pdf'));
    await act(() => vi.advanceTimersByTimeAsync(300));
    await waitFor(() => expect(requests).toEqual([{ page: '1', search: 'pdf' }]));
    expect(window.location.search).toBe('?search=pdf');
  });

  it('shows only the latest page when an earlier response arrives last', async () => {
    let calls = 0;
    serveSkills(() => {
      calls += 1;
      const page = { skills: [], total: calls, offset: 0, limit: 10 };
      return calls === 1
        ? delay(50).then(() => HttpResponse.json({ data: page }))
        : HttpResponse.json({ data: page });
    });
    const { result } = renderSkills();

    act(() => result.current.goToPage(2));
    act(() => result.current.goToPage(3));

    await waitFor(() => expect(result.current.total).toBe(2));
    await act(() => delay(80));
    expect(result.current.total).toBe(2);
  });

  it('loads the page in the address when Back restores stale first-render props', async () => {
    window.history.replaceState(null, '', '/skills?marketplace=anthropic-skills&page=2');
    const requests = serveSkills();
    const { result } = renderSkills();

    await waitFor(() => expect(result.current.status).toBe('success'));
    expect(requests).toEqual([{ page: '2', marketplace: 'anthropic-skills' }]);
    expect(result.current.browse.marketplace).toBe('anthropic-skills');
  });

  it('keeps an unavailable Marketplace error and retries the same page', async () => {
    let fail = true;
    const requests = serveSkills((query) =>
      fail
        ? HttpResponse.json({ message: 'Credential unavailable.' }, { status: 502 })
        : HttpResponse.json({ data: skillsPage(toBrowse(query)) }),
    );
    const { result } = renderSkills();

    act(() => result.current.goToPage(2));
    await waitFor(() =>
      expect(result.current.error).toEqual({ status: 502, message: 'Credential unavailable.' }),
    );
    fail = false;
    act(() => result.current.retry());

    await waitFor(() => expect(result.current.error).toBeNull());
    expect(requests.map((query) => query.page)).toEqual(['2', '2']);
  });

  it('adds and removes a favourite in the preferences', () => {
    serveSkills();
    const { result } = renderSkills();

    act(() => result.current.toggleFavourite('partner-skills:skills/tdd'));
    expect([...result.current.favourites]).toEqual([...FAVOURITES, 'partner-skills:skills/tdd']);
    act(() => result.current.toggleFavourite(FAVOURITES[0]));
    expect([...result.current.favourites]).toEqual(['partner-skills:skills/tdd']);
  });
});

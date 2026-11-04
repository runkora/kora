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
import {
  STORY_SKILLS,
  storyOverview,
  storyPage,
  TEAM_MARKETPLACE,
} from '../_components/skills-story-fixtures';
import { useSkills } from './use-skills';

const OVERVIEW = `/skills/${TEAM_MARKETPLACE.slug}`;

/** Records each BFF request's query and answers with the fixture page. */
function serveSkills(
  respond: () => Response | Promise<Response> = () => HttpResponse.json({ data: storyPage() }),
) {
  const requests: URLSearchParams[] = [];
  server.use(
    http.get('/api/skills', ({ request }) => {
      requests.push(new URL(request.url).searchParams);
      return respond();
    }),
  );
  return requests;
}

/** useSkills reads the page size from the preferences, so it renders inside their provider. */
function renderSkills(overview = storyOverview()) {
  return renderHook(() => useSkills(overview, null), {
    wrapper: ({ children }) => (
      <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>{children}</PreferencesProvider>
    ),
  });
}

beforeEach(() => window.history.replaceState(null, '', `${OVERVIEW}?agent=draft-1`));
afterEach(() => vi.useRealTimers());

describe('useSkills', () => {
  it('goes to a page with one request and writes it to the URL, keeping other query keys', async () => {
    const requests = serveSkills();
    const { result } = renderSkills();

    act(() => result.current.goToPage(2));

    expect(window.location.search).toBe('?agent=draft-1&page=2');
    await waitFor(() => expect(result.current.status).toBe('success'));
    expect(requests).toHaveLength(1);
    expect(Object.fromEntries(requests[0])).toEqual({
      marketplace: TEAM_MARKETPLACE.slug,
      page: '2',
      pageSize: '25',
    });
  });

  it('searches 300 ms after typing stops, from three characters, back on page 1', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    window.history.replaceState(null, '', `${OVERVIEW}?agent=draft-1&page=4`);
    const requests = serveSkills();
    const { result } = renderSkills(
      storyOverview({ browse: { marketplace: TEAM_MARKETPLACE.slug, search: '', page: 4 } }),
    );

    act(() => result.current.setSearchText('pd'));
    await act(() => vi.advanceTimersByTimeAsync(300));
    expect(requests).toHaveLength(0);

    act(() => result.current.setSearchText('pdf'));
    await act(() => vi.advanceTimersByTimeAsync(300));
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].get('search')).toBe('pdf');
    expect(requests[0].get('page')).toBe('1');
    expect(window.location.search).toBe('?agent=draft-1&search=pdf');
  });

  it('shows only the latest page when an earlier response arrives last', async () => {
    let calls = 0;
    serveSkills(() => {
      calls += 1;
      const page = calls === 1 ? storyPage({ total: 1 }) : storyPage({ total: 2 });
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
    window.history.replaceState(null, '', `${OVERVIEW}?search=docx&page=2`);
    const requests = serveSkills();
    const { result } = renderSkills();

    expect(result.current.searchText).toBe('docx');
    await waitFor(() => expect(result.current.status).toBe('success'));
    expect(requests).toHaveLength(1);
    expect(requests[0].get('page')).toBe('2');
    expect(result.current.skills).toEqual(STORY_SKILLS);
  });

  it('keeps an unavailable Marketplace error and retries the same page', async () => {
    let fail = true;
    const requests = serveSkills(() =>
      fail
        ? HttpResponse.json({ message: 'Credential unavailable.' }, { status: 502 })
        : HttpResponse.json({ data: storyPage() }),
    );
    const { result } = renderSkills();

    act(() => result.current.goToPage(2));
    await waitFor(() =>
      expect(result.current.error).toEqual({ status: 502, message: 'Credential unavailable.' }),
    );
    fail = false;
    act(() => result.current.retry());

    await waitFor(() => expect(result.current.error).toBeNull());
    expect(requests.map((query) => query.get('page'))).toEqual(['2', '2']);
  });
});

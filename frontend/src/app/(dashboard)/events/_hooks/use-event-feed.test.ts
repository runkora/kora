import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

/** A stream the test drives by hand. */
class FakeSource {
  static CLOSED = 2;
  readyState = 1;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((m: MessageEvent<string>) => void) | null = null;
  constructor(public url: string) {}
  close() {
    this.closed = true;
  }
  send(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>);
  }
}

const api = vi.hoisted(() => ({
  fetchEventPage: vi.fn(),
  openEventStream: vi.fn(),
  sources: [] as FakeSource[],
}));
vi.mock('../_services/events-api', () => ({
  fetchEventPage: api.fetchEventPage,
  openEventStream: api.openEventStream,
}));
vi.stubGlobal('EventSource', FakeSource);

import { NO_FILTERS, filterParams } from '../_types';
import type { EventFilters, EventRecord } from '../_types';
import { useEventFeed } from './use-event-feed';

const record = (seq: number): EventRecord => ({
  seq,
  recorded_at: '2026-11-06T00:00:00Z',
  event: {
    specversion: '1.0',
    id: `e${seq}`,
    source: 'kora-agents',
    type: 'agents.run.started.v1',
    subject: `runs/r${seq}`,
    time: '2026-11-06T00:00:00Z',
    datacontenttype: 'application/json',
    koratenant: 't',
    koraprincipal: `p${seq}`,
    koraprincipalname: `Principal ${seq}`,
    koraprincipaltype: 'USER',
    koraagent: 'triage',
    korachaindepth: 0,
    data: { entity: {}, occurred_at: '2026-11-06T00:00:00Z' },
  },
});

const initial = {
  key: filterParams(NO_FILTERS).toString(),
  page: { events: [record(2), record(1)], next_cursor: null },
  error: null,
};

beforeEach(() => {
  vi.clearAllMocks();
  api.sources = [];
  api.openEventStream.mockImplementation((params: URLSearchParams) => {
    const source = new FakeSource(`/api/events/stream?${params.toString()}`);
    api.sources.push(source);
    return source;
  });
});

const latest = () => api.sources[api.sources.length - 1] as FakeSource;

describe('useEventFeed', () => {
  it('starts from the server page and streams from its newest seq', async () => {
    const { result } = renderHook(() => useEventFeed(NO_FILTERS, initial));
    expect(api.fetchEventPage).not.toHaveBeenCalled();
    await waitFor(() => expect(api.sources).toHaveLength(1));
    expect(latest().url).toBe('/api/events/stream?after=2');

    act(() => latest().send(record(3)));
    expect(result.current.events.map((e) => e.id)).toEqual(['e3', 'e2', 'e1']);
    expect(result.current.freshIds.has('e3')).toBe(true);
    expect(result.current.principals.map((p) => p.name)).toContain('Principal 3');
  });

  it('holds live events while paused and shows them on resume', async () => {
    const { result } = renderHook(() => useEventFeed(NO_FILTERS, initial));
    await waitFor(() => expect(api.sources).toHaveLength(1));
    act(() => result.current.setPaused(true));
    act(() => latest().send(record(3)));
    expect(result.current.stream).toBe('paused');
    expect(result.current.pending).toBe(1);
    expect(result.current.events).toHaveLength(2);

    act(() => result.current.setPaused(false));
    expect(result.current.pending).toBe(0);
    expect(result.current.events.map((e) => e.id)).toEqual(['e3', 'e2', 'e1']);
  });

  it('fetches the filtered page and closes the stream while a search is active', async () => {
    api.fetchEventPage.mockResolvedValue({
      page: { events: [record(1)], next_cursor: null },
      error: null,
    });
    const { result, rerender } = renderHook((f: EventFilters) => useEventFeed(f, initial), {
      initialProps: NO_FILTERS,
    });
    await waitFor(() => expect(api.sources).toHaveLength(1));

    rerender({ ...NO_FILTERS, tab: 'runs', query: 'r1' });
    await waitFor(() => expect(result.current.loading).toBe(false));
    const params = api.fetchEventPage.mock.calls[0]?.[0] as URLSearchParams;
    expect(params.get('category')).toBe('agents.run');
    expect(params.get('q')).toBe('r1');
    expect(result.current.stream).toBe('search');
    expect(api.sources[0]?.closed).toBe(true);
    expect(api.sources).toHaveLength(1);
  });

  it('shows a failed page as the error and retries it', async () => {
    api.fetchEventPage
      .mockResolvedValueOnce({ page: null, error: 'Could not load the events.' })
      .mockResolvedValueOnce({ page: { events: [record(5)], next_cursor: 'c' }, error: null });
    const { result } = renderHook(() =>
      useEventFeed(NO_FILTERS, { key: initial.key, page: null, error: null }),
    );
    await waitFor(() => expect(result.current.error).toBe('Could not load the events.'));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.events.map((e) => e.id)).toEqual(['e5']));
    expect(result.current.hasMore).toBe(true);
  });

  it('keeps the stream open when an older page fails', async () => {
    api.fetchEventPage.mockResolvedValue({ page: null, error: 'Could not load the events.' });
    const { result } = renderHook(() =>
      useEventFeed(NO_FILTERS, { ...initial, page: { ...initial.page, next_cursor: 'c' } }),
    );
    await waitFor(() => expect(api.sources).toHaveLength(1));
    act(() => result.current.loadOlder());
    await waitFor(() => expect(result.current.olderError).toBe('Could not load the events.'));
    expect(result.current.error).toBeNull();
    expect(api.sources[0]?.closed).toBe(false);
    expect(result.current.stream).toBe('live');
  });

  it('drops a streamed record it cannot draw', async () => {
    const { result } = renderHook(() => useEventFeed(NO_FILTERS, initial));
    await waitFor(() => expect(api.sources).toHaveLength(1));
    act(() => latest().send({ seq: 9, event: { id: 'bad' } }));
    expect(result.current.events.map((e) => e.id)).toEqual(['e2', 'e1']);
  });
});

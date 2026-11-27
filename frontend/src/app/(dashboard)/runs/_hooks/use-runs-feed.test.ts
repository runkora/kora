import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { RunWithAttribution } from '@runkora/shared/api';

/** A stream the test drives by hand. */
class FakeSource {
  static CLOSED = 2;
  readyState = 1;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners: Record<string, ((e: MessageEvent<string>) => void)[]> = {};
  constructor(public url: string) {}
  addEventListener(type: string, fn: (e: MessageEvent<string>) => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data: unknown, id = '') {
    const event = { data: JSON.stringify(data), lastEventId: id } as MessageEvent<string>;
    this.listeners[type]?.forEach((fn) => fn(event));
  }
}

const api = vi.hoisted(() => ({
  fetchRunsPage: vi.fn(),
  openRunsStream: vi.fn(),
  calls: [] as string[],
  sources: [] as FakeSource[],
}));
vi.mock('../_services/runs-api', () => ({
  fetchRunsPage: api.fetchRunsPage,
  openRunsStream: api.openRunsStream,
}));
vi.stubGlobal('EventSource', FakeSource);

import { DEFAULT_RUN_FILTERS } from '../_types';
import { useRunsFeed } from './use-runs-feed';

const run = (id: string): RunWithAttribution => ({
  id,
  tenant_id: 't',
  agent_type_id: 'agt',
  state: 'active',
  pod_name: '',
  created_by: 'usr',
  started_at: null,
  completed_at: null,
  created_at: '2026-11-25T10:00:00Z',
  updated_at: '2026-11-25T10:00:00Z',
});

beforeEach(() => {
  vi.clearAllMocks();
  api.calls = [];
  api.sources = [];
  api.openRunsStream.mockImplementation((params: URLSearchParams) => {
    api.calls.push('stream');
    const source = new FakeSource(`?${params.toString()}`);
    api.sources.push(source);
    return source;
  });
  api.fetchRunsPage.mockImplementation((params: URLSearchParams) => {
    api.calls.push(`page?${params.toString()}`);
    return Promise.resolve({ page: { runs: [run('b'), run('a')], nextCursor: null }, error: null });
  });
});

const latest = () => api.sources[api.sources.length - 1]!;

describe('useRunsFeed', () => {
  it('draws a page read while the stream subscribes, then the page read once it is subscribed', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    expect(api.calls).toEqual(['stream', 'page?limit=50']);
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => latest().onopen?.());
    await waitFor(() => expect(api.calls).toEqual(['stream', 'page?limit=50', 'page?limit=50']));
  });

  it('draws the server page on the first render and reads only the subscribed page', async () => {
    const initial = { id: 'read-1', key: '', page: { runs: [run('s')], nextCursor: null } };
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS, initial));
    expect(result.current.runs.map((r) => r.id)).toEqual(['s']);
    expect(result.current.loading).toBe(false);
    expect(api.calls).toEqual(['stream']);
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs.map((r) => r.id)).toEqual(['b', 'a']));
    expect(api.calls).toEqual(['stream', 'page?limit=50']);
  });

  it('draws a server page once, not again when back or forward restores it', () => {
    const initial = { id: 'read-2', key: '', page: { runs: [run('s')], nextCursor: null } };
    renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS, initial)).unmount();
    api.calls = [];
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS, initial));
    expect(result.current.runs).toHaveLength(0);
    expect(api.calls).toEqual(['stream', 'page?limit=50']);
  });

  it('reads a page alongside the stream when the server page is for other filters', () => {
    const initial = {
      id: 'read-3',
      key: 'state=failed',
      page: { runs: [run('s')], nextCursor: null },
    };
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS, initial));
    expect(result.current.runs).toHaveLength(0);
    expect(api.calls).toEqual(['stream', 'page?limit=50']);
  });

  it('keeps a change that arrives before the subscribed page, and replays it on that page', async () => {
    let answer: (value: unknown) => void = () => {};
    let reads = 0;
    api.fetchRunsPage.mockImplementation(() => {
      reads += 1;
      // The page read alongside the stream answers at once; the subscribed one waits.
      if (reads === 1) {
        return Promise.resolve({
          page: { runs: [run('b'), run('a')], nextCursor: null },
          error: null,
        });
      }
      return new Promise((resolve) => (answer = resolve));
    });
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    await waitFor(() => expect(result.current.runs.map((r) => r.id)).toEqual(['b', 'a']));
    act(() => latest().onopen?.());
    const created = { ...run('c'), updated_at: '2026-11-25T10:05:00Z' };
    act(() => latest().emit('run', { type: 'agents.run.created.v1', matches: true, run: created }));
    // The subscribed page was read before c was created, so only the replayed change adds it.
    await act(async () =>
      answer({ page: { runs: [run('b'), run('a')], nextCursor: null }, error: null }),
    );
    expect(result.current.runs.map((r) => r.id)).toEqual(['c', 'b', 'a']);
  });

  it('ignores the page read alongside the stream when the subscribed page answered first', async () => {
    let early: (value: unknown) => void = () => {};
    let reads = 0;
    api.fetchRunsPage.mockImplementation(() => {
      reads += 1;
      if (reads === 1) return new Promise((resolve) => (early = resolve));
      return Promise.resolve({ page: { runs: [run('b')], nextCursor: null }, error: null });
    });
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs.map((r) => r.id)).toEqual(['b']));
    await act(async () => early({ page: { runs: [run('stale')], nextCursor: null }, error: null }));
    expect(result.current.runs.map((r) => r.id)).toEqual(['b']);
  });

  it('still loads the list when the stream is refused', async () => {
    vi.useFakeTimers();
    try {
      const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
      latest().readyState = FakeSource.CLOSED;
      await act(async () => latest().onerror?.());
      expect(result.current.runs).toHaveLength(2);
      // The reopened stream has no position, so its opening reads the first page again.
      await act(() => vi.advanceTimersByTimeAsync(5_000));
      expect(latest().url).toBe('?');
      await act(async () => latest().onopen?.());
      expect(api.fetchRunsPage).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it('still loads the list when the stream fails while the browser retries it', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    latest().readyState = 0;
    await act(async () => latest().onerror?.());
    expect(result.current.runs).toHaveLength(2);
    expect(result.current.reconnecting).toBe(true);
    // The retried stream reads the first page again once it opens, as the page was read unsubscribed.
    await act(async () => latest().onopen?.());
    expect(api.sources).toHaveLength(1);
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(3);
  });

  it('replaces the stream on a reset and reads the first page again once it opens', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    const first = latest();
    act(() => first.onopen?.());
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => first.emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') }, '7'));
    act(() => first.emit('reset', {}));
    expect(first.closed).toBe(true);
    expect(api.sources).toHaveLength(2);
    expect(latest().url).toBe('?');
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(2);
    act(() => latest().onopen?.());
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(3));
  });

  it('reopens a closed stream past the last event it read', async () => {
    vi.useFakeTimers();
    try {
      renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
      const first = latest();
      await act(async () => first.onopen?.());
      act(() =>
        first.emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') }, '48213'),
      );
      first.readyState = FakeSource.CLOSED;
      act(() => first.onerror?.());
      await act(() => vi.advanceTimersByTimeAsync(5_000));
      expect(api.sources).toHaveLength(2);
      expect(latest().url).toBe('?after=48213');
      await act(async () => latest().onopen?.());
      expect(api.fetchRunsPage).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it('drops a stream message it cannot read', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => {
      latest().listeners.run?.forEach((fn) =>
        fn({ data: 'not json', lastEventId: '' } as MessageEvent<string>),
      );
      latest().emit('run', { type: 'agents.run.created.v1', matches: true, run: { id: 'z' } });
    });
    expect(result.current.runs.map((r) => r.id)).toEqual(['b', 'a']);
  });

  it('reads an older page with the cursor and steps back without a fetch', async () => {
    api.fetchRunsPage.mockImplementation((params: URLSearchParams) => {
      api.calls.push(`page?${params.toString()}`);
      const older = params.has('cursor');
      return Promise.resolve({
        page: { runs: older ? [run('a')] : [run('b')], nextCursor: older ? null : 'cur1' },
        error: null,
      });
    });
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.hasOlder).toBe(true));
    act(() => result.current.older());
    await waitFor(() => expect(result.current.pageIndex).toBe(1));
    expect(api.calls).toContain('page?limit=50&cursor=cur1');
    expect(result.current.runs.map((r) => r.id)).toEqual(['a']);
    act(() => result.current.newer());
    expect(result.current.runs.map((r) => r.id)).toEqual(['b']);
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(3);
  });

  it('drops an older page that answers after the filters changed', async () => {
    let answer: (value: unknown) => void = () => {};
    api.fetchRunsPage.mockImplementation((params: URLSearchParams) =>
      params.has('cursor')
        ? new Promise((resolve) => (answer = resolve))
        : Promise.resolve({ page: { runs: [run('b')], nextCursor: 'cur1' }, error: null }),
    );
    const { result, rerender } = renderHook((filters) => useRunsFeed(filters), {
      initialProps: DEFAULT_RUN_FILTERS,
    });
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.hasOlder).toBe(true));
    act(() => result.current.older());
    rerender({ ...DEFAULT_RUN_FILTERS, quick: 'failed' });
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs.map((r) => r.id)).toEqual(['b']));
    await act(async () =>
      answer({ page: { runs: [run('stale')], nextCursor: null }, error: null }),
    );
    expect(result.current.pageIndex).toBe(0);
    expect(result.current.runs.map((r) => r.id)).toEqual(['b']);
  });

  it('reloads the first page on the way back when the bar holds new runs', async () => {
    api.fetchRunsPage.mockImplementation((params: URLSearchParams) =>
      Promise.resolve({
        page: params.has('cursor')
          ? { runs: [run('a')], nextCursor: null }
          : { runs: [run('b')], nextCursor: 'cur1' },
        error: null,
      }),
    );
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.hasOlder).toBe(true));
    act(() => result.current.older());
    await waitFor(() => expect(result.current.pageIndex).toBe(1));
    act(() =>
      latest().emit('run', { type: 'agents.run.created.v1', matches: true, run: run('c') }),
    );
    expect(result.current.freshCount).toBe(1);
    act(() => result.current.newer());
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(4));
    await waitFor(() => expect(result.current.freshCount).toBe(0));
    expect(result.current.pageIndex).toBe(0);
  });

  it('leaves a stream the server ended to the browser, which reconnects with Last-Event-ID', async () => {
    renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    const first = latest();
    act(() => first.onopen?.());
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(2));
    act(() => first.emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') }, '9'));
    // An ended response leaves the source connecting, not closed.
    first.readyState = 0;
    act(() => first.onerror?.());
    expect(first.closed).toBe(false);
    expect(api.sources).toHaveLength(1);
    act(() => first.onopen?.());
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(2);
  });

  it('reports a dropped stream until it opens again', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    expect(result.current.reconnecting).toBe(false);
    act(() => latest().onerror?.());
    expect(result.current.reconnecting).toBe(true);
    act(() => latest().onopen?.());
    expect(result.current.reconnecting).toBe(false);
  });

  it('marks only matching runs created while the page is open as live', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => {
      latest().emit('run', { type: 'agents.run.created.v1', matches: true, run: run('c') });
      latest().emit('run', { type: 'agents.run.created.v1', matches: false, run: run('d') });
      latest().emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') });
    });
    expect([...result.current.liveIds]).toEqual(['c']);
  });

  it('reopens the stream with the new filters when they change', async () => {
    const { rerender } = renderHook((filters) => useRunsFeed(filters), {
      initialProps: DEFAULT_RUN_FILTERS,
    });
    expect(api.sources).toHaveLength(1);
    rerender({ ...DEFAULT_RUN_FILTERS, quick: 'failed' });
    await waitFor(() => expect(api.sources).toHaveLength(2));
    expect(api.sources[0]!.closed).toBe(true);
    expect(latest().url).toBe('?state=failed%2Cbudget_exhausted');
  });
});

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
  it('fetches the first page only once the stream is subscribed', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    expect(api.calls).toEqual(['stream']);
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    expect(api.calls).toEqual(['stream', 'page?limit=50']);
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
      expect(api.fetchRunsPage).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
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
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(1);
    act(() => latest().onopen?.());
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(2));
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
      expect(api.fetchRunsPage).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('updates a shown row in place from a message that no longer matches, and never inserts one', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    act(() => latest().onopen?.());
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => {
      latest().emit('run', {
        type: 'agents.run.failed.v1',
        matches: false,
        run: { ...run('a'), state: 'failed' },
      });
      latest().emit('run', { type: 'agents.run.created.v1', matches: false, run: run('z') });
    });
    expect(result.current.runs.map((r) => [r.id, r.state])).toEqual([
      ['b', 'active'],
      ['a', 'failed'],
    ]);
  });

  it('leaves a stream the server ended to the browser, which reconnects with Last-Event-ID', async () => {
    renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    const first = latest();
    act(() => first.onopen?.());
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(1));
    act(() => first.emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') }, '9'));
    // An ended response leaves the source connecting, not closed.
    first.readyState = 0;
    act(() => first.onerror?.());
    expect(first.closed).toBe(false);
    expect(api.sources).toHaveLength(1);
    act(() => first.onopen?.());
    expect(api.fetchRunsPage).toHaveBeenCalledTimes(1);
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

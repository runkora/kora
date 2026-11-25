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
  it('opens the stream before it fetches the first page', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    expect(api.calls[0]).toBe('stream');
    expect(api.calls[1]).toBe('page?limit=50');
  });

  it('reloads the first page on a reset and keeps the stream open', async () => {
    const { result } = renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
    await waitFor(() => expect(result.current.runs).toHaveLength(2));
    act(() => latest().emit('reset', {}));
    await waitFor(() => expect(api.fetchRunsPage).toHaveBeenCalledTimes(2));
    expect(latest().closed).toBe(false);
  });

  it('reopens a closed stream past the last event it read', async () => {
    vi.useFakeTimers();
    try {
      renderHook(() => useRunsFeed(DEFAULT_RUN_FILTERS));
      const first = latest();
      act(() =>
        first.emit('run', { type: 'agents.run.idle.v1', matches: true, run: run('a') }, '48213'),
      );
      first.readyState = FakeSource.CLOSED;
      act(() => first.onerror?.());
      await act(() => vi.advanceTimersByTimeAsync(5_000));
      expect(api.sources).toHaveLength(2);
      expect(latest().url).toBe('?after=48213');
    } finally {
      vi.useRealTimers();
    }
  });

  it('reopens the stream with the new filters when they change', async () => {
    const { rerender } = renderHook((filters) => useRunsFeed(filters), {
      initialProps: DEFAULT_RUN_FILTERS,
    });
    await waitFor(() => expect(api.sources).toHaveLength(1));
    rerender({ ...DEFAULT_RUN_FILTERS, quick: 'failed' });
    await waitFor(() => expect(api.sources).toHaveLength(2));
    expect(api.sources[0]!.closed).toBe(true);
    expect(latest().url).toBe('?state=failed%2Cbudget_exhausted');
  });
});

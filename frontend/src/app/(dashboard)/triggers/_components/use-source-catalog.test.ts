import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
}));
vi.mock('../_services/triggers-api', () => api);

import { useSourceCatalog } from './use-source-catalog';

/** A promise the test resolves by hand, to control response order. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchEventTypes.mockResolvedValue({ eventTypes: null, error: null });
  api.fetchProviderFields.mockResolvedValue({ fields: [], error: null });
});

describe('useSourceCatalog', () => {
  it('ignores a late answer for a provider that is no longer chosen', async () => {
    const github = deferred<unknown>();
    const alertmanager = deferred<unknown>();
    api.fetchProviderEventTypes.mockImplementation((provider: string) =>
      provider === 'github' ? github.promise : alertmanager.promise,
    );

    const { result, rerender } = renderHook(({ provider }) => useSourceCatalog(provider, '', ''), {
      initialProps: { provider: 'github' },
    });
    rerender({ provider: 'alertmanager' });

    await act(async () => {
      alertmanager.resolve({
        eventTypes: { declared: ['alert.firing'], associations: [] },
        error: null,
      });
    });
    await act(async () => {
      github.resolve({ eventTypes: { declared: ['push'], associations: ['OWNER'] }, error: null });
    });

    expect(result.current.declared).toEqual(['alert.firing']);
    expect(result.current.associations).toEqual([]);
  });

  it("ignores a late answer for an event's fields after the event changes", async () => {
    api.fetchProviderEventTypes.mockResolvedValue({ eventTypes: null, error: null });
    const push = deferred<unknown>();
    const comment = deferred<unknown>();
    api.fetchProviderFields.mockImplementation((_p: string, eventType: string) =>
      eventType === 'push' ? push.promise : comment.promise,
    );

    const { result, rerender } = renderHook(
      ({ eventType }) => useSourceCatalog('github', '', eventType),
      { initialProps: { eventType: 'push' } },
    );
    rerender({ eventType: 'issue_comment' });

    await act(async () => {
      comment.resolve({ fields: [{ path: 'comment.body', group: 'comment' }], error: null });
    });
    await act(async () => {
      push.resolve({ fields: [{ path: 'ref', group: 'event' }], error: null });
    });

    expect(result.current.fields.map((f) => f.path)).toEqual(['comment.body']);
  });

  it('asks the source for event types when one exists, else the provider', async () => {
    api.fetchProviderEventTypes.mockResolvedValue({ eventTypes: null, error: null });
    renderHook(() => useSourceCatalog('github', '', ''));
    expect(api.fetchProviderEventTypes).toHaveBeenCalledWith('github');
    expect(api.fetchEventTypes).not.toHaveBeenCalled();

    vi.clearAllMocks();
    renderHook(() => useSourceCatalog('github', 'src-1', ''));
    expect(api.fetchEventTypes).toHaveBeenCalledWith('src-1');
    expect(api.fetchProviderEventTypes).not.toHaveBeenCalled();
  });
});

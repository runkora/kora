import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
}));
vi.mock('../_services/triggers-api', () => api);

import { useProviderCategories, useSourceCatalog } from './use-source-catalog';

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
        eventTypes: { declared: ['alert.firing'], category: 'webhook' },
        error: null,
      });
    });
    await act(async () => {
      github.resolve({
        eventTypes: {
          declared: ['push'],
          category: 'webhook',
          senders: { roles: [{ value: 'OWNER', label: 'Owner', description: '' }] },
        },
        error: null,
      });
    });

    expect(result.current.declared).toEqual(['alert.firing']);
    expect(result.current.roles).toEqual([]);
    expect(result.current.identifiesSenders).toBe(false);
  });

  it('reads the sender roles and whether senders are identified', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: {
        declared: ['mention', 'direct_message'],
        category: 'chat',
        senders: { roles: [] },
      },
      error: null,
    });
    const { result } = renderHook(() => useSourceCatalog('slack', 'src-slack', ''));
    await act(async () => {});
    expect(result.current.identifiesSenders).toBe(true);
    expect(result.current.roles).toEqual([]);
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

describe('useProviderCategories', () => {
  it('maps each provider to the category its event types report', async () => {
    api.fetchProviderEventTypes.mockImplementation(async (provider: string) => ({
      eventTypes: { declared: [], category: provider === 'slack' ? 'chat' : 'webhook' },
      error: null,
    }));
    const { result } = renderHook(() => useProviderCategories(['github', 'slack']));
    expect(result.current.pending).toEqual(['github', 'slack']);
    await act(async () => {});
    expect(result.current).toEqual({
      categories: { github: 'webhook', slack: 'chat' },
      pending: [],
      failed: [],
    });
  });

  it('reports a provider whose request failed', async () => {
    api.fetchProviderEventTypes.mockImplementation(async (provider: string) =>
      provider === 'slack'
        ? { eventTypes: null, error: 'unavailable' }
        : { eventTypes: { declared: [], category: 'webhook' }, error: null },
    );
    const { result } = renderHook(() => useProviderCategories(['github', 'slack']));
    await act(async () => {});
    expect(result.current).toEqual({
      categories: { github: 'webhook' },
      pending: [],
      failed: ['slack'],
    });
  });
});

describe('useSourceCatalog schemaless', () => {
  it('is schemaless for a generic source before any answer, and fetches its fields once', async () => {
    api.fetchEventTypes.mockReturnValue(new Promise(() => undefined));
    const { result, rerender } = renderHook(
      ({ eventType }) => useSourceCatalog('generic', 'src-gen', eventType),
      { initialProps: { eventType: 'i' } },
    );
    expect(result.current.schemaless).toBe(true);
    rerender({ eventType: 'issue' });
    await act(async () => {});
    expect(api.fetchProviderFields).toHaveBeenCalledTimes(1);
    expect(api.fetchProviderFields).toHaveBeenCalledWith('generic', 'webhook');
  });

  it('is schemaless when a source reports a null declared list', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: { declared: null, category: 'webhook' },
      error: null,
    });
    const { result } = renderHook(() => useSourceCatalog('custom', 'src-x', ''));
    await act(async () => {});
    expect(result.current.schemaless).toBe(true);
  });

  it('is not schemaless for a provider with declared types, and fetches per event', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: { declared: ['push'], category: 'webhook' },
      error: null,
    });
    const { result, rerender } = renderHook(
      ({ eventType }) => useSourceCatalog('github', 'src-gh', eventType),
      { initialProps: { eventType: 'push' } },
    );
    expect(result.current.schemaless).toBe(false);
    await act(async () => {});
    expect(result.current.schemaless).toBe(false);
    rerender({ eventType: 'issue_comment' });
    await act(async () => {});
    expect(api.fetchProviderFields.mock.calls).toEqual([
      ['github', 'push'],
      ['github', 'issue_comment'],
    ]);
  });
});

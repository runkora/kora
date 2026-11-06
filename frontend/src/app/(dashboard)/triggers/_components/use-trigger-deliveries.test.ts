import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ listDeliveryPage: vi.fn() }));
vi.mock('../_services/triggers-api', () => api);

import { DELIVERY_PAGE_SIZE, useTriggerDeliveries } from './use-trigger-deliveries';
import type { DeliveryQuery } from './use-trigger-deliveries';

/** The API's delivery shape, as little of it as the row mapper needs. */
const raw = (id: string) => ({
  id,
  kind: 'webhook',
  status: 'failed',
  received_at: '2026-07-29T04:00:00Z',
  runs: [],
});
const page = (ids: string[], nextCursor: string | null, error: string | null = null) => ({
  deliveries: ids.map(raw),
  nextCursor,
  error,
});

beforeEach(() => vi.clearAllMocks());

describe('useTriggerDeliveries', () => {
  it('refetches when the status changes and pages older with the cursor', async () => {
    api.listDeliveryPage
      .mockResolvedValueOnce(page(['all-1'], null))
      .mockResolvedValueOnce(page(['failed-1'], 'c1'))
      .mockResolvedValueOnce(page(['failed-2'], null));
    const { result, rerender } = renderHook((q: DeliveryQuery) => useTriggerDeliveries(q), {
      initialProps: { triggerId: 'trg-1' } as DeliveryQuery,
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.hasMore).toBe(false);

    rerender({ triggerId: 'trg-1', status: 'failed' });
    await waitFor(() => expect(result.current.deliveries.map((d) => d.id)).toEqual(['failed-1']));
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith({
      triggerId: 'trg-1',
      status: 'failed',
      limit: DELIVERY_PAGE_SIZE,
    });
    expect(result.current.hasMore).toBe(true);

    act(() => result.current.loadOlder());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith({
      triggerId: 'trg-1',
      status: 'failed',
      limit: DELIVERY_PAGE_SIZE,
      cursor: 'c1',
    });
    expect(result.current.deliveries.map((d) => d.id)).toEqual(['failed-1', 'failed-2']);
    expect(result.current.hasMore).toBe(false);
  });

  it('keeps "Load older" after an older page fails, so it can be retried', async () => {
    api.listDeliveryPage
      .mockResolvedValueOnce(page(['d1'], 'c1'))
      .mockResolvedValueOnce(page([], null, 'boom'));
    const { result } = renderHook(() => useTriggerDeliveries({}));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.loadOlder());
    await waitFor(() => expect(result.current.error).toBe('boom'));
    expect(result.current.hasMore).toBe(true);
  });

  it('fetches nothing with no query', () => {
    renderHook(() => useTriggerDeliveries(null));
    expect(api.listDeliveryPage).not.toHaveBeenCalled();
  });
});

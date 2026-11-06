import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ listDeliveryPage: vi.fn() }));
vi.mock('../_services/triggers-api', () => api);

import { DELIVERY_PAGE_SIZE, useTriggerDeliveries } from './use-trigger-deliveries';

/** The API's delivery shape, as little of it as the row mapper needs. */
const raw = (
  id: string,
  receivedAt: string,
  triggerId: string | null,
  runTriggers: string[] = [],
) => ({
  id,
  kind: 'webhook',
  status: 'dispatched',
  trigger_id: triggerId,
  received_at: receivedAt,
  runs: runTriggers.map((t) => ({ trigger_id: t, status: 'dispatched' })),
});

beforeEach(() => vi.clearAllMocks());

describe('useTriggerDeliveries', () => {
  it('asks for a scheduled or manual trigger by trigger, and starts loading', async () => {
    api.listDeliveryPage.mockResolvedValue({ deliveries: [], error: null });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', ''));
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(api.listDeliveryPage).toHaveBeenCalledWith({
      triggerId: 'trg-1',
      limit: DELIVERY_PAGE_SIZE,
    });
  });

  it('reads a webhook from its source and keeps only the deliveries that reached it', async () => {
    api.listDeliveryPage.mockResolvedValue({
      deliveries: [
        raw('mine-direct', '2026-07-29T04:00:00Z', 'trg-1'),
        raw('mine-fanout', '2026-07-29T03:00:00Z', 'trg-2', ['trg-2', 'trg-1']),
        raw('other', '2026-07-29T02:00:00Z', 'trg-2', ['trg-2']),
      ],
      error: null,
    });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', 'src-1'));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(api.listDeliveryPage).toHaveBeenCalledWith({
      sourceId: 'src-1',
      limit: DELIVERY_PAGE_SIZE,
    });
    expect(result.current.deliveries.map((d) => d.id)).toEqual(['mine-direct', 'mine-fanout']);
    expect(result.current.hasMore).toBe(false);
  });

  it('pages back from the oldest delivery fetched, shown or not', async () => {
    const full = Array.from({ length: DELIVERY_PAGE_SIZE }, (_, i) =>
      raw(
        `d${i}`,
        `2026-07-29T04:${String(59 - i).padStart(2, '0')}:00Z`,
        i === 0 ? 'trg-1' : 'trg-2',
        [i === 0 ? 'trg-1' : 'trg-2'],
      ),
    );
    api.listDeliveryPage
      .mockResolvedValueOnce({ deliveries: full, error: null })
      .mockResolvedValueOnce({
        deliveries: [raw('older', '2026-07-28T00:00:00Z', 'trg-1')],
        error: null,
      });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', 'src-1'));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.deliveries.map((d) => d.id)).toEqual(['d0']);
    expect(result.current.hasMore).toBe(true);

    act(() => result.current.loadOlder());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(api.listDeliveryPage).toHaveBeenLastCalledWith({
      sourceId: 'src-1',
      limit: DELIVERY_PAGE_SIZE,
      before: full[full.length - 1]!.received_at,
    });
    expect(result.current.deliveries.map((d) => d.id)).toEqual(['d0', 'older']);
    expect(result.current.hasMore).toBe(false);
  });

  it('reports a failed first fetch and tries again on retry', async () => {
    api.listDeliveryPage.mockResolvedValueOnce({ deliveries: [], error: 'boom' });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', ''));
    await waitFor(() => expect(result.current.error).toBe('boom'));
    api.listDeliveryPage.mockResolvedValueOnce({ deliveries: [], error: null });
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.error).toBeNull());
    expect(api.listDeliveryPage).toHaveBeenCalledTimes(2);
  });

  it('fetches nothing with no trigger open', () => {
    renderHook(() => useTriggerDeliveries('', ''));
    expect(api.listDeliveryPage).not.toHaveBeenCalled();
  });

  it('keeps "Load older" available after an older page fails, so it can be retried', async () => {
    const full = Array.from({ length: DELIVERY_PAGE_SIZE }, (_, i) =>
      raw(`d${i}`, `2026-07-29T04:${String(59 - i).padStart(2, '0')}:00Z`, 'trg-1'),
    );
    api.listDeliveryPage
      .mockResolvedValueOnce({ deliveries: full, error: null })
      .mockResolvedValueOnce({ deliveries: [], error: 'boom' });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', ''));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.loadOlder());
    await waitFor(() => expect(result.current.error).toBe('boom'));
    expect(result.current.hasMore).toBe(true);
    expect(result.current.deliveries).toHaveLength(DELIVERY_PAGE_SIZE);
  });

  it('starts only one older fetch however fast it is asked', async () => {
    const full = Array.from({ length: DELIVERY_PAGE_SIZE }, (_, i) =>
      raw(`d${i}`, `2026-07-29T04:${String(59 - i).padStart(2, '0')}:00Z`, 'trg-1'),
    );
    api.listDeliveryPage
      .mockResolvedValueOnce({ deliveries: full, error: null })
      .mockResolvedValue({ deliveries: [], error: null });
    const { result } = renderHook(() => useTriggerDeliveries('trg-1', ''));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => {
      result.current.loadOlder();
      result.current.loadOlder();
    });
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(api.listDeliveryPage).toHaveBeenCalledTimes(2);
  });
});

/**
 * The server render decides where the first cursor starts from, so these cover
 * the two things that can silently cap the console again: asking for a
 * different page size than the client pager uses, and dropping the cursor on
 * the way out of the envelope.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('server-only', () => ({}));
vi.mock('@orbit/server/triggers-client', () => ({
  listSources: vi.fn(),
  listTriggers: vi.fn(),
  listDeliveries: vi.fn(),
}));
vi.mock('@orbit/server/agents-client', () => ({
  agentsClient: { listAgentTypes: vi.fn().mockResolvedValue([]) },
}));
// Mocked for its ApiError alone: the real module reads ORBIT_API_URL at import
// time, which a unit-scope test has no business providing.
vi.mock('@orbit/server/client', () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status = 500) {
      super(message);
      this.status = status;
    }
  },
}));

import { listSources, listTriggers, listDeliveries } from '@orbit/server/triggers-client';
import { DELIVERY_PAGE_SIZE } from '../_types';
import { fetchTriggersPage } from './triggers-server';

beforeEach(() => {
  vi.mocked(listSources).mockResolvedValue([]);
  vi.mocked(listTriggers).mockResolvedValue([]);
});

describe('fetchTriggersPage', () => {
  it('asks for exactly the page size the client pager continues from', async () => {
    vi.mocked(listDeliveries).mockResolvedValue({ deliveries: [], nextCursor: null });
    await fetchTriggersPage('tok');
    // omitPayload too: the grid renders payloadExcerpt and never the full
    // body, so asking for it would ship a hundred webhook payloads a page.
    expect(listDeliveries).toHaveBeenCalledWith('tok', {
      limit: DELIVERY_PAGE_SIZE,
      omitPayload: true,
    });
  });

  it('carries the cursor out so the first Load more has somewhere to start', async () => {
    vi.mocked(listDeliveries).mockResolvedValue({
      deliveries: [],
      nextCursor: '2026-10-07T12:00:00Z|d1',
    });
    const res = await fetchTriggersPage('tok');
    expect(res.ok).toBe(true);
    if (res.ok) expect(res.deliveryCursor).toBe('2026-10-07T12:00:00Z|d1');
  });

  it('reports no cursor when the first page is the whole history', async () => {
    vi.mocked(listDeliveries).mockResolvedValue({ deliveries: [], nextCursor: null });
    const res = await fetchTriggersPage('tok');
    expect(res.ok).toBe(true);
    if (res.ok) expect(res.deliveryCursor).toBeNull();
  });
});

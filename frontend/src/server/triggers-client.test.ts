import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@runkora/server/config', () => ({
  config: { koraTriggersUrl: 'http://kora-triggers.test/api/v1' },
}));

import { listDeliveries, listTriggers } from './triggers-client';

function stubFetch(body: unknown) {
  const fetchMock = vi.fn<(url: string, init: RequestInit) => Promise<Response>>(() =>
    Promise.resolve(Response.json(body)),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => vi.unstubAllGlobals());

describe('triggers-client', () => {
  it('lists deliveries without the full payload, bounded to the window it is given', async () => {
    const fetchMock = stubFetch({ deliveries: [], next_cursor: 'c2' });
    await listDeliveries('token-a', {
      triggerId: 'trg-1',
      status: 'filtered',
      receivedAfter: '2026-10-30T12:00:00.000Z',
      cursor: 'c1',
      limit: 50,
    });
    const url = new URL(fetchMock.mock.calls[0][0]);
    expect(url.pathname).toBe('/api/v1/trigger-deliveries');
    expect(Object.fromEntries(url.searchParams)).toEqual({
      trigger_id: 'trg-1',
      status: 'filtered',
      received_after: '2026-10-30T12:00:00.000Z',
      cursor: 'c1',
      limit: '50',
      omit_payload: 'true',
    });
  });

  it('lists triggers over the window it is given', async () => {
    const fetchMock = stubFetch({ triggers: [], stats: null });
    await listTriggers('token-a', { agentTypeId: 'at-1', window: '7d' });
    expect(fetchMock.mock.calls[0][0]).toBe(
      'http://kora-triggers.test/api/v1/triggers?agent_type_id=at-1&window=7d',
    );
  });
});

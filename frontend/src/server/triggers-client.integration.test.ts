/**
 * Integration tests for `listDeliveries` in the typed triggers client.
 *
 * This is the one place the backend's delivery envelope
 * (`{ deliveries, has_next_page, next_cursor }`) is translated into the
 * frontend's `DeliveryPage` (`{ deliveries, nextCursor }`), so the mapping is
 * worth pinning on both sides: the query string it builds going out, and the
 * page it returns coming back — including `nextCursor` being null on the last
 * page, which is what hides the "Load more" control.
 *
 * `@orbit/server/config` is vi.mock'd so the client addresses a stub host that
 * MSW intercepts; no cluster or env file is involved.
 */
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse, type JsonBodyType } from 'msw';
import { server } from '@orbit/test-utils';
import type { TriggerDelivery } from '@orbit/shared/api';

vi.mock('@orbit/server/config', () => ({
  config: { orbitTriggersUrl: 'http://test-triggers-svc' },
}));

import { listDeliveries } from './triggers-client';
import { ApiError } from './api-error';

const UPSTREAM = 'http://test-triggers-svc/trigger-deliveries';

function makeDelivery(id: string): TriggerDelivery {
  return {
    id,
    tenant_id: 'test-tenant',
    kind: 'scheduled',
    trigger_id: 'trg-1',
    trigger_name: 'nightly-sweep',
    status: 'dispatched',
    received_at: '2026-10-07T12:00:00Z',
  };
}

/** Answer every upstream list call with `body`, recording the URL asked for. */
function respondWith(body: JsonBodyType, init?: ResponseInit): { url: URL | null } {
  const seen: { url: URL | null } = { url: null };
  server.use(
    http.get(UPSTREAM, ({ request }) => {
      seen.url = new URL(request.url);
      return HttpResponse.json(body, init);
    }),
  );
  return seen;
}

describe('listDeliveries (server client)', () => {
  it('maps the backend envelope onto a DeliveryPage', async () => {
    const rows = [makeDelivery('d-1'), makeDelivery('d-2')];
    respondWith({ deliveries: rows, has_next_page: true, next_cursor: 'cur-2' });

    const page = await listDeliveries('test-access-token', { limit: 2 });

    // Exact shape: the camelCase cursor, and nothing of has_next_page, which
    // the frontend derives from nextCursor instead of carrying twice.
    expect(page).toEqual({ deliveries: rows, nextCursor: 'cur-2' });
  });

  it('reports nextCursor null on the last page', async () => {
    respondWith({
      deliveries: [makeDelivery('d-1')],
      has_next_page: false,
      next_cursor: null,
    });

    const page = await listDeliveries('test-access-token');

    expect(page.deliveries).toHaveLength(1);
    expect(page.nextCursor).toBeNull();
  });

  it('reports nextCursor null when the backend omits next_cursor entirely', async () => {
    respondWith({ deliveries: [makeDelivery('d-1')] });

    const page = await listDeliveries('test-access-token');

    expect(page.nextCursor).toBeNull();
  });

  it('resolves an empty page when the backend returns no deliveries key', async () => {
    respondWith({});

    expect(await listDeliveries('test-access-token')).toEqual({
      deliveries: [],
      nextCursor: null,
    });
  });

  it('resolves an empty page for a bodyless 204', async () => {
    server.use(http.get(UPSTREAM, () => new HttpResponse(null, { status: 204 })));

    expect(await listDeliveries('test-access-token')).toEqual({
      deliveries: [],
      nextCursor: null,
    });
  });

  it('builds the upstream query from the options, repeating status and outcome', async () => {
    const seen = respondWith({ deliveries: [], next_cursor: null });

    await listDeliveries('test-access-token', {
      sourceId: 'src-1',
      triggerId: 'trg-1',
      kind: 'webhook',
      status: ['failed', 'missed'],
      outcome: ['denied', 'dropped'],
      limit: 25,
      cursor: 'cur-1',
    });

    expect(seen.url!.toString()).toBe(
      'http://test-triggers-svc/trigger-deliveries' +
        '?source_id=src-1&trigger_id=trg-1&kind=webhook' +
        '&status=failed&status=missed&outcome=denied&outcome=dropped&limit=25&cursor=cur-1',
    );
    expect(seen.url!.searchParams.getAll('status')).toEqual(['failed', 'missed']);
    expect(seen.url!.searchParams.getAll('outcome')).toEqual(['denied', 'dropped']);
  });

  it('asks for the bare path when no options are given', async () => {
    const seen = respondWith({ deliveries: [] });

    await listDeliveries('test-access-token');

    expect(seen.url!.toString()).toBe('http://test-triggers-svc/trigger-deliveries');
    expect(seen.url!.search).toBe('');
  });

  it('omits a null cursor instead of sending the string "null"', async () => {
    const seen = respondWith({ deliveries: [] });

    await listDeliveries('test-access-token', { cursor: null, status: [], outcome: [] });

    expect(seen.url!.search).toBe('');
  });

  it('sends the session token as a bearer credential', async () => {
    let auth: string | null = null;
    server.use(
      http.get(UPSTREAM, ({ request }) => {
        auth = request.headers.get('authorization');
        return HttpResponse.json({ deliveries: [] });
      }),
    );

    await listDeliveries('test-access-token');

    expect(auth).toBe('Bearer test-access-token');
  });

  it('throws an ApiError carrying the upstream status, code and message', async () => {
    server.use(
      http.get(UPSTREAM, () =>
        HttpResponse.json(
          { code: 'invalid_cursor', message: 'cursor does not match this filter set' },
          { status: 400 },
        ),
      ),
    );

    const err = await listDeliveries('test-access-token', { cursor: 'bogus' }).catch(
      (e: unknown) => e,
    );

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({
      status: 400,
      code: 'invalid_cursor',
      message: 'cursor does not match this filter set',
    });
  });
});

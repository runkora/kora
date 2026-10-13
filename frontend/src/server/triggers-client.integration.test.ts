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

  it('refuses a body that omits next_cursor rather than calling it the last page', async () => {
    // This test used to assert nextCursor === null here, which encoded the bug.
    // The upstream always writes next_cursor, so coercing its absence to null
    // reports "last page"; the BFF then re-wraps that as a well-formed
    // `{ data, next_cursor }`, the browser's own guard sees nothing wrong, and
    // Load more vanishes - the list is truncated in silence.
    respondWith({ deliveries: [makeDelivery('d-1')], has_next_page: true });

    await expect(listDeliveries('test-access-token')).rejects.toThrow(ApiError);
  });

  it('refuses an empty object rather than calling it an empty page', async () => {
    // `{}` is a gateway artefact, not a result: the upstream always writes all
    // three keys. Reading it as an empty final page is indistinguishable from a
    // tenant that genuinely has no deliveries, which is how an error page from
    // something in between used to truncate the list without saying so.
    respondWith({});

    await expect(listDeliveries('test-access-token')).rejects.toThrow(ApiError);
  });

  it('refuses a bodyless 204, which this endpoint never sends', async () => {
    // The list endpoint always answers 200 with an envelope, so a 204 came from
    // something in between. Accepting it causes the same silent truncation.
    server.use(http.get(UPSTREAM, () => new HttpResponse(null, { status: 204 })));

    await expect(listDeliveries('test-access-token')).rejects.toThrow(ApiError);
  });

  it('accepts a null deliveries array as a genuinely empty page', async () => {
    // Go marshals the nil slice the repository returns for no rows, so an empty
    // page really does arrive as `"deliveries": null`. Validating that field
    // with Array.isArray would reject a tenant's first visit, which is why
    // has_next_page carries the check instead.
    respondWith({ deliveries: null, has_next_page: false, next_cursor: null });

    expect(await listDeliveries('test-access-token')).toEqual({
      deliveries: [],
      nextCursor: null,
    });
  });

  it('refuses an empty-string next_cursor', async () => {
    // Not null, so a consumer offers Load more; but a falsy cursor is omitted
    // from the next request, so the backend answers page one and the rows on
    // screen are appended again. The contract is null or a non-empty string.
    respondWith({ deliveries: [makeDelivery('d-1')], has_next_page: true, next_cursor: '' });

    await expect(listDeliveries('test-access-token')).rejects.toThrow(ApiError);
  });

  it('refuses an envelope whose has_next_page contradicts its next_cursor', async () => {
    // Upstream derives has_next_page as `nextCursor != ""` and writes
    // next_cursor only when non-empty, so these two cannot legitimately
    // disagree. When they do, trusting either one puts the page boundary
    // somewhere neither side meant.
    respondWith({ deliveries: [makeDelivery('d-1')], has_next_page: false, next_cursor: 'cur-2' });

    await expect(listDeliveries('test-access-token')).rejects.toThrow(ApiError);
  });

  it('builds the upstream query from the options, repeating status and outcome', async () => {
    const seen = respondWith({ deliveries: [], has_next_page: false, next_cursor: null });

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
    const seen = respondWith({ deliveries: [], has_next_page: false, next_cursor: null });

    await listDeliveries('test-access-token');

    expect(seen.url!.toString()).toBe('http://test-triggers-svc/trigger-deliveries');
    expect(seen.url!.search).toBe('');
  });

  it('omits a null cursor instead of sending the string "null"', async () => {
    const seen = respondWith({ deliveries: [], has_next_page: false, next_cursor: null });

    await listDeliveries('test-access-token', { cursor: null, status: [], outcome: [] });

    expect(seen.url!.search).toBe('');
  });

  it('sends the session token as a bearer credential', async () => {
    let auth: string | null = null;
    server.use(
      http.get(UPSTREAM, ({ request }) => {
        auth = request.headers.get('authorization');
        return HttpResponse.json({ deliveries: [], has_next_page: false, next_cursor: null });
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

/**
 * Integration tests for the browser-side `listDeliveries` and, through it, the
 * private `sendPage`.
 *
 * Errors here are values, never exceptions: a throw would take the Triggers
 * page down with it, so every failure branch below has to resolve.
 */
import { describe, it, expect } from 'vitest';
import { http, HttpResponse, type JsonBodyType } from 'msw';
import { server } from '@orbit/test-utils';
import type { TriggerDelivery } from '@orbit/shared/api';
import { listDeliveries } from './triggers-api';

const BFF = '/api/trigger-deliveries';

function makeDelivery(id: string): TriggerDelivery {
  return {
    id,
    tenant_id: 'test-tenant',
    kind: 'webhook',
    source_id: 'src-1',
    trigger_id: 'trg-1',
    status: 'failed',
    received_at: '2026-10-07T12:00:00Z',
  };
}

/** Answer the BFF list call with `body`, recording the URL asked for. */
function respondWith(body: JsonBodyType, init?: ResponseInit): { url: URL | null } {
  const seen: { url: URL | null } = { url: null };
  server.use(
    http.get(BFF, ({ request }) => {
      seen.url = new URL(request.url);
      return HttpResponse.json(body, init);
    }),
  );
  return seen;
}

describe('listDeliveries (browser client)', () => {
  it('returns the page and the cursor that belongs to it', async () => {
    const rows = [makeDelivery('d-1'), makeDelivery('d-2')];
    respondWith({ data: rows, next_cursor: 'cur-2' });

    await expect(listDeliveries({ limit: 2 })).resolves.toEqual({
      deliveries: rows,
      nextCursor: 'cur-2',
      error: null,
    });
  });

  it('reports nextCursor null on the last page', async () => {
    respondWith({ data: [makeDelivery('d-1')], next_cursor: null });

    const page = await listDeliveries();

    expect(page.deliveries).toHaveLength(1);
    expect(page.nextCursor).toBeNull();
    expect(page.error).toBeNull();
  });

  // The route always writes both keys, so a body missing `data` is a gateway
  // artefact, not an empty result.
  it('reports an error when the envelope is missing its data array', async () => {
    respondWith({});

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  it('reports an error for a body that is the JSON literal null', async () => {
    respondWith(null);

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  it('reports an error when data is present but next_cursor is missing', async () => {
    respondWith({ data: [] });

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  it('reports an error when next_cursor is not a string or null', async () => {
    respondWith({ data: [], next_cursor: 42 });

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  it('reports an error for an empty-string next_cursor', async () => {
    // '' is not null, so the grid would offer Load more; but the request
    // builder only serializes a truthy cursor, so the click omits it and the
    // backend answers page one, appending rows already on screen and leaving
    // Load more showing. An unbounded duplicate loop is worse than an error.
    respondWith({ data: [makeDelivery('d-1')], next_cursor: '' });

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  it('accepts a genuinely empty page, which has data but no rows', async () => {
    respondWith({ data: [], next_cursor: null });

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: null,
    });
  });

  it('sends status and outcome as repeated keys, with limit and cursor', async () => {
    const seen = respondWith({ data: [], next_cursor: null });

    await listDeliveries({
      status: ['failed', 'missed'],
      outcome: ['denied', 'dropped'],
      limit: 50,
      cursor: 'cur-1',
    });

    expect(seen.url!.pathname).toBe(BFF);
    expect(seen.url!.search).toBe(
      '?status=failed&status=missed&outcome=denied&outcome=dropped&limit=50&cursor=cur-1',
    );
    expect(seen.url!.searchParams.getAll('status')).toEqual(['failed', 'missed']);
    expect(seen.url!.searchParams.getAll('outcome')).toEqual(['denied', 'dropped']);
  });

  it('asks the server to omit the payload when told to', async () => {
    // The grid renders payloadExcerpt only, so fetching the full body costs a
    // hundred webhook payloads a page and renders none of them.
    const seen = respondWith({ data: [], next_cursor: null });
    await listDeliveries({ omitPayload: true });
    expect(seen.url!.searchParams.get('omit_payload')).toBe('true');
  });

  it('asks for the bare route when no filters are given', async () => {
    const seen = respondWith({ data: [] });

    await listDeliveries();

    expect(seen.url!.search).toBe('');
  });

  it('omits a null cursor instead of sending the string "null"', async () => {
    const seen = respondWith({ data: [] });

    await listDeliveries({ cursor: null, status: [], outcome: [] });

    expect(seen.url!.search).toBe('');
  });

  // ── Error as value ─────────────────────────────────────────────────────────

  it('resolves with the message from a refusal rather than throwing', async () => {
    server.use(
      http.get(BFF, () =>
        HttpResponse.json({ code: 'forbidden', message: 'not your tenant' }, { status: 403 }),
      ),
    );

    await expect(listDeliveries({ status: ['failed'] })).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'not your tenant',
    });
  });

  it('falls back to a status message when the error body carries none', async () => {
    server.use(http.get(BFF, () => HttpResponse.text('upstream exploded', { status: 500 })));

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'Request failed (500)',
    });
  });

  // A 200 that is not JSON — an HTML error page from a proxy, say.
  it('reports an error when a 200 body is not JSON, rather than an empty page', async () => {
    server.use(
      http.get(BFF, () =>
        HttpResponse.text('<html><body>gateway</body></html>', {
          headers: { 'Content-Type': 'text/html' },
        }),
      ),
    );

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  // res.text() rejects when the body fails after the headers arrived, which a
  // plain `await` would let through.
  it('reports an error rather than throwing when the body fails mid-stream', async () => {
    server.use(
      http.get(BFF, () => {
        const stream = new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode('{"data":[{"id":"d-1"}'));
            controller.error(new Error('connection reset while reading body'));
          },
        });
        return new HttpResponse(stream, {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  // A zero-length 200 is the same class of gateway artefact as the HTML error
  // page: the route always writes a body, so an empty one is not "no rows".
  it('reports an error for a zero-length 200 rather than an empty page', async () => {
    server.use(http.get(BFF, () => new HttpResponse(null, { status: 200 })));

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'The server returned a response that could not be read.',
    });
  });

  // A non-ok body that is the JSON literal `null` parses successfully, so the
  // catch in errorMessage never fires and reading .message off it throws.
  it('reports a status message when a non-ok body is the JSON literal null', async () => {
    server.use(http.get(BFF, () => HttpResponse.json(null, { status: 500 })));

    await expect(listDeliveries()).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'Request failed (500)',
    });
  });

  it('resolves with a network-error message when the request cannot be made', async () => {
    server.use(http.get(BFF, () => HttpResponse.error()));

    await expect(listDeliveries({ limit: 20 })).resolves.toEqual({
      deliveries: [],
      nextCursor: null,
      error: 'Network error: could not reach the server.',
    });
  });

  it('drops the cursor on an error so the caller cannot page off a failed page', async () => {
    server.use(http.get(BFF, () => HttpResponse.json({ next_cursor: 'cur-9' }, { status: 502 })));

    const page = await listDeliveries({ cursor: 'cur-8' });

    expect(page.nextCursor).toBeNull();
    expect(page.error).not.toBeNull();
  });
});

/**
 * Integration tests for the browser-side `listDeliveries` and, through it,
 * `sendPage` — the paginated sibling of `send`.
 *
 * `sendPage` is private, so these drive it through the exported
 * `listDeliveries`. Two contracts are under test:
 *
 *  1. The request: repeated `status`/`outcome`, plus `limit` and `cursor`,
 *     reach the BFF route as a query string it can read back with getAll.
 *  2. The response: errors are values, never exceptions. A refusal, a body
 *     that is not JSON, and the network being down all have to resolve — a
 *     throw here would take the Triggers page down with it.
 *
 * MSW intercepts the relative BFF path, so no Next.js server is involved.
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

  it('reports an empty page when the route answers with neither key', async () => {
    respondWith({});

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
  //
  // Every branch below has to resolve. `resolves.toEqual` is the assertion
  // that matters: a rejection fails the test even if the shape is right.

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

  // A 200 that is not JSON (an HTML error page from a proxy, say). Reporting it
  // as an empty page would be indistinguishable from the end of history, so the
  // grid would say "no deliveries" and hide Load more on a truncated list.
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

  // The header of triggers-api.ts promises these functions never throw, and
  // every caller relies on it: an escaping rejection skips the completion
  // handler entirely, so Load more stays on a disabled "Loading..." with no
  // error line and therefore no Retry. res.text() rejects when the body fails
  // after the headers arrived, which a plain `await` would let through.
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

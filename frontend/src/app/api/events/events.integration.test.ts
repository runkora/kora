/**
 * Integration tests for GET /api/events and GET /api/events/stream.
 *
 * Both routes pass only the filters kora-events reads. The stream route also maps `after` to
 * Last-Event-ID for the first connection and passes the upstream's refusals through.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: {
    koraEventsUrl: 'http://test-events-svc/v1',
    koraTriggersUrl: 'http://test-triggers-svc',
  },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET as LIST } from './route';
import { GET as STREAM } from './stream/route';
import { getSession } from '@runkora/server/session';

const EVENTS_URL = 'http://test-events-svc/v1/events';

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@runkora.test', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['events:read'], evaluatedAt: 0 },
  };
}

const request = (path: string, headers: Record<string, string> = {}) =>
  new NextRequest(`http://localhost${path}`, { headers });
const noParams = { params: Promise.resolve({}) } as never;

beforeEach(() => {
  vi.mocked(getSession).mockResolvedValue(makeSession() as never);
});

describe('GET /api/events', () => {
  it('forwards the list filters, repeated, with the session token', async () => {
    let url: URL | undefined;
    let auth: string | null = null;
    server.use(
      http.get(EVENTS_URL, ({ request: r }) => {
        url = new URL(r.url);
        auth = r.headers.get('authorization');
        return HttpResponse.json({ events: [], next_cursor: null });
      }),
    );

    const res = await LIST(
      request('/api/events?category=a&category=b&agent=triage&q=x&cursor=c&limit=5&tenant=other'),
      noParams,
    );

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ data: { events: [], next_cursor: null } });
    expect(url?.searchParams.getAll('category')).toEqual(['a', 'b']);
    expect(url?.searchParams.get('q')).toBe('x');
    expect(url?.searchParams.get('cursor')).toBe('c');
    expect(url?.searchParams.has('tenant')).toBe(false);
    expect(auth).toBe('Bearer test-access-token');
  });

  it('passes a refusal through as its status', async () => {
    server.use(
      http.get(EVENTS_URL, () => HttpResponse.json({ code: 'forbidden' }, { status: 403 })),
    );
    const res = await LIST(request('/api/events'), noParams);
    expect(res.status).toBe(403);
    expect(((await res.json()) as { code: string }).code).toBe('upstream_error');
  });
});

describe('GET /api/events/stream', () => {
  const sse = () =>
    new HttpResponse('id: 1\ndata: {}\n\n', { headers: { 'Content-Type': 'text/event-stream' } });

  it('drops q and other parameters, and replays from `after`', async () => {
    let url: URL | undefined;
    let lastEventId: string | null = null;
    server.use(
      http.get(`${EVENTS_URL}/stream`, ({ request: r }) => {
        url = new URL(r.url);
        lastEventId = r.headers.get('last-event-id');
        return sse();
      }),
    );

    const res = await STREAM(request('/api/events/stream?type=t&q=x&run_id=r&after=42'), noParams);

    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toBe('text/event-stream');
    expect(await res.text()).toContain('id: 1');
    expect(url?.search).toBe('?type=t');
    expect(lastEventId).toBe('42');
  });

  it("prefers the browser's Last-Event-ID over `after`", async () => {
    let lastEventId: string | null = null;
    server.use(
      http.get(`${EVENTS_URL}/stream`, ({ request: r }) => {
        lastEventId = r.headers.get('last-event-id');
        return sse();
      }),
    );
    await STREAM(request('/api/events/stream?after=42', { 'Last-Event-ID': '57' }), noParams);
    expect(lastEventId).toBe('57');
  });

  it('passes the upstream refusal through', async () => {
    server.use(
      http.get(`${EVENTS_URL}/stream`, () =>
        HttpResponse.json({ code: 'forbidden', message: 'No access.' }, { status: 403 }),
      ),
    );
    const res = await STREAM(request('/api/events/stream'), noParams);
    expect(res.status).toBe(403);
    expect(await res.json()).toEqual({ code: 'forbidden', message: 'No access.' });
  });

  it('answers 502 when kora-events cannot be reached', async () => {
    server.use(http.get(`${EVENTS_URL}/stream`, () => HttpResponse.error()));
    const res = await STREAM(request('/api/events/stream'), noParams);
    expect(res.status).toBe(502);
  });
});

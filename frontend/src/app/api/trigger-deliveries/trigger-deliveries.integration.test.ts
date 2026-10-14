/**
 * Integration tests for GET /api/trigger-deliveries, which reads the browser's
 * query string, hands the parts to the typed triggers client, and re-wraps the
 * page as `{ data, next_cursor }`.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse, type JsonBodyType } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';
import type { TriggerDelivery } from '@orbit/shared/api';

vi.mock('@orbit/server/config', () => ({
  config: { orbitTriggersUrl: 'http://test-triggers-svc' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@orbit/server/session';

const UPSTREAM = 'http://test-triggers-svc/trigger-deliveries';

interface Seen {
  url: URL | null;
  auth: string | null;
  called: boolean;
}

/** Intercept the one upstream call, record what it looked like, answer `body`. */
function captureUpstream(body: JsonBodyType, init?: ResponseInit): Seen {
  const seen: Seen = { url: null, auth: null, called: false };
  server.use(
    http.get(UPSTREAM, ({ request }) => {
      seen.url = new URL(request.url);
      seen.auth = request.headers.get('authorization');
      seen.called = true;
      return HttpResponse.json(body, init);
    }),
  );
  return seen;
}

function makeRequest(query = ''): NextRequest {
  return new NextRequest(`http://localhost/api/trigger-deliveries${query ? `?${query}` : ''}`);
}

function makeDelivery(id: string): TriggerDelivery {
  return {
    id,
    tenant_id: 'test-tenant',
    kind: 'webhook',
    source_id: 'src-1',
    trigger_id: 'trg-1',
    trigger_name: 'comment-review',
    event_type: 'issue_comment',
    status: 'dispatched',
    received_at: '2026-10-07T12:00:00Z',
  };
}

describe('GET /api/trigger-deliveries', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
      accessToken: 'test-access-token',
      refreshToken: 'test-refresh',
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('forwards every filter it accepts to orbit-triggers under the session token', async () => {
    const incoming = new URLSearchParams();
    incoming.set('source_id', 'src-1');
    incoming.set('trigger_id', 'trg-1');
    incoming.set('kind', 'webhook');
    incoming.append('status', 'failed');
    incoming.append('status', 'missed');
    incoming.append('outcome', 'denied');
    incoming.append('outcome', 'dropped');
    incoming.set('limit', '25');
    incoming.set('cursor', '2026-10-07T12:00:00Z|d-9');

    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });
    const res = await GET(makeRequest(incoming.toString()));

    expect(res.status).toBe(200);
    expect(seen.auth).toBe('Bearer test-access-token');

    const q = seen.url!.searchParams;
    expect(q.get('source_id')).toBe('src-1');
    expect(q.get('trigger_id')).toBe('trg-1');
    expect(q.get('kind')).toBe('webhook');
    expect(q.get('limit')).toBe('25');
    // The cursor is opaque and may contain characters that need escaping; it
    // has to arrive upstream byte-for-byte or the next page is a different one.
    expect(q.get('cursor')).toBe('2026-10-07T12:00:00Z|d-9');
  });

  // Joining them into "failed,missed" would be one unknown status upstream,
  // matching nothing.
  it('keeps status and outcome repeated rather than comma-joining them', async () => {
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });
    await GET(makeRequest('status=failed&status=missed&outcome=denied&outcome=dropped'));

    const q = seen.url!.searchParams;
    expect(q.getAll('status')).toEqual(['failed', 'missed']);
    expect(q.getAll('outcome')).toEqual(['denied', 'dropped']);
    expect(seen.url!.search).toContain('status=failed&status=missed');
    expect(seen.url!.search).toContain('outcome=denied&outcome=dropped');
    expect(seen.url!.search).not.toContain('%2C'); // an encoded comma
  });

  it('builds the exact upstream URL for a fully specified page request', async () => {
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });
    await GET(
      makeRequest(
        'source_id=src-1&trigger_id=trg-1&kind=webhook&status=failed&status=missed&outcome=denied&limit=25&cursor=cur-1',
      ),
    );

    expect(seen.url!.toString()).toBe(
      'http://test-triggers-svc/trigger-deliveries' +
        '?source_id=src-1&trigger_id=trg-1&kind=webhook' +
        '&status=failed&status=missed&outcome=denied&limit=25&cursor=cur-1',
    );
  });

  it('sends no query string upstream when the browser sent no filters', async () => {
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });
    const res = await GET(makeRequest());

    expect(res.status).toBe(200);
    expect(seen.called).toBe(true);
    expect(seen.url!.search).toBe('');
  });

  // Number('abc') is NaN, and `limit=NaN` upstream is a 400 waiting to happen.
  it('drops a limit that is not a number rather than forwarding NaN', async () => {
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });
    await GET(makeRequest('limit=abc'));

    expect(seen.url!.search).toBe('');
  });

  it('returns the page as { data, next_cursor }, the shape the browser client reads', async () => {
    const delivery = makeDelivery('d-1');
    captureUpstream({ deliveries: [delivery], has_next_page: true, next_cursor: 'cur-2' });

    const res = await GET(makeRequest('limit=1'));

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ data: [delivery], next_cursor: 'cur-2' });
  });

  it('reports next_cursor null on the last page', async () => {
    captureUpstream({ deliveries: [makeDelivery('d-1')], has_next_page: false, next_cursor: null });

    const res = await GET(makeRequest());

    expect(await res.json()).toEqual({ data: [makeDelivery('d-1')], next_cursor: null });
  });

  it('passes an upstream refusal through with its status, code and message', async () => {
    server.use(
      http.get(UPSTREAM, () =>
        HttpResponse.json(
          { code: 'invalid_cursor', message: 'cursor does not match this filter set' },
          { status: 400 },
        ),
      ),
    );

    const res = await GET(makeRequest('cursor=bogus'));

    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({
      code: 'invalid_cursor',
      message: 'cursor does not match this filter set',
    });
  });

  it('surfaces an upstream 500 instead of answering with an empty page', async () => {
    server.use(http.get(UPSTREAM, () => HttpResponse.json({}, { status: 500 })));

    const res = await GET(makeRequest());

    expect(res.status).toBe(500);
    expect(await res.json()).not.toHaveProperty('data');
  });

  it('returns 401 without calling upstream when the session is invalid', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });

    const res = await GET(makeRequest('status=failed'));

    expect(res.status).toBe(401);
    expect(await res.json()).toEqual({ code: 'unauthorized', message: 'No valid session' });
    expect(seen.called).toBe(false);
  });

  it('returns 401 when there is no session at all', async () => {
    vi.mocked(getSession).mockResolvedValue(undefined as never);
    vi.mocked(isSessionValid).mockReturnValue(false);
    const seen = captureUpstream({ deliveries: [], has_next_page: false, next_cursor: null });

    const res = await GET(makeRequest());

    expect(res.status).toBe(401);
    expect(seen.called).toBe(false);
  });
});

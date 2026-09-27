/**
 * Integration tests for GET /api/agents/runs.
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd to point the agents client at a fake
 *   orbit-agents host.
 * - `@orbit/server/session` is vi.mock'd so the route sees a valid session.
 * - `resolveUserDisplayNames` is vi.mock'd; name resolution has its own tests.
 * - MSW intercepts the upstream `/api/v1/runs` call and records its query.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

vi.mock('@orbit/server/config', () => ({
  config: { orbitAgentsUrl: 'http://test-agents' },
}));

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

vi.mock('@orbit/server/services/identity-server', () => ({
  resolveUserDisplayNames: vi.fn(),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@orbit/server/session';
import { resolveUserDisplayNames } from '@orbit/server/services/identity-server';

const RUNS_URL = 'http://test-agents/api/v1/runs';

function run(id: string, createdBy = 'user-1') {
  return {
    id,
    tenant_id: 'test-tenant',
    agent_type_id: 'at-1',
    state: 'completed',
    created_by: createdBy,
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
  };
}

function get(query = ''): Promise<Response> {
  return GET(new NextRequest(`http://localhost/api/agents/runs${query}`));
}

/** Captures the query the route sent upstream, answering with `body`. */
function captureUpstream(body: Record<string, unknown>): { query: () => URLSearchParams } {
  let seen = new URLSearchParams();
  server.use(
    http.get(RUNS_URL, ({ request }) => {
      seen = new URL(request.url).searchParams;
      return HttpResponse.json(body);
    }),
  );
  return { query: () => seen };
}

describe('GET /api/agents/runs', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map());
  });

  it('forwards agent_type_id, limit and cursor, scoped to the session tenant', async () => {
    const upstream = captureUpstream({ runs: [], has_more: false, next_cursor: null, limit: 5 });

    await get('?agent_type_id=at-1&limit=5&cursor=2026-09-01T00%3A00%3A00Z%7Crun-9');

    const q = upstream.query();
    expect(q.get('tenant_id')).toBe('test-tenant');
    expect(q.get('agent_type_id')).toBe('at-1');
    expect(q.get('limit')).toBe('5');
    expect(q.get('cursor')).toBe('2026-09-01T00:00:00Z|run-9');
  });

  it('does not forward a caller-supplied tenant_id or unknown parameters', async () => {
    const upstream = captureUpstream({ runs: [], has_more: false, next_cursor: null, limit: 25 });

    await get('?tenant_id=other-tenant&state=failed');

    const q = upstream.query();
    expect(q.get('tenant_id')).toBe('test-tenant');
    expect(q.has('state')).toBe(false);
  });

  it('returns the page with its pagination fields and resolved starter names', async () => {
    captureUpstream({
      runs: [run('run-1', 'user-1'), run('run-2', 'svc-1')],
      has_more: true,
      next_cursor: '2026-09-01T00:00:00Z|run-2',
      limit: 2,
    });
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map([['user-1', 'Ada Lovelace']]));

    const res = await get('?limit=2');
    const body = await res.json();

    expect(res.status).toBe(200);
    expect(body.has_more).toBe(true);
    expect(body.next_cursor).toBe('2026-09-01T00:00:00Z|run-2');
    expect(body.limit).toBe(2);
    expect(body.data.map((r: { id: string }) => r.id)).toEqual(['run-1', 'run-2']);
    expect(body.data[0].created_by_name).toBe('Ada Lovelace');
    expect(body.data[1].created_by_name).toBeNull();
  });

  it('defaults the pagination fields when upstream omits them', async () => {
    captureUpstream({ runs: [run('run-1')] });

    const body = await (await get()).json();

    expect(body.has_more).toBe(false);
    expect(body.next_cursor).toBeNull();
    expect(body.limit).toBe(1);
  });

  it('passes an upstream error through with its status', async () => {
    server.use(
      http.get(RUNS_URL, () =>
        HttpResponse.json(
          { code: 'invalid_field', message: 'cursor is not a valid pagination cursor' },
          { status: 400 },
        ),
      ),
    );

    const res = await get('?cursor=garbage');
    const body = await res.json();

    expect(res.status).toBe(400);
    expect(body.message).toBe('cursor is not a valid pagination cursor');
  });

  it('returns 401 without a valid session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);

    const res = await get();

    expect(res.status).toBe(401);
  });
});

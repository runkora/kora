/**
 * Integration tests for GET /api/agents/runs and GET /api/agents/types/{typeId}/runs.
 *
 * MSW stands in for orbit-agents and records the query each route sends upstream.
 * Name resolution is mocked; it has its own tests.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraAgentsUrl: 'http://test-agents' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

vi.mock('@runkora/server/services/identity-server', () => ({
  resolveUserDisplayNames: vi.fn(),
}));

import { GET } from './route';
import { GET as GET_FOR_TYPE } from '../types/[typeId]/runs/route';
import { getSession, isSessionValid } from '@runkora/server/session';
import { resolveUserDisplayNames } from '@runkora/server/services/identity-server';

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

/** Answers GET /api/v1/runs with `body` and returns the query it was sent. */
function captureUpstream(body: Record<string, unknown>): () => URLSearchParams {
  let seen = new URLSearchParams();
  server.use(
    http.get(RUNS_URL, ({ request }) => {
      seen = new URL(request.url).searchParams;
      return HttpResponse.json(body);
    }),
  );
  return () => seen;
}

const get = (query = '') => GET(new NextRequest(`http://localhost/api/agents/runs${query}`));

describe('GET /api/agents/runs', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map());
  });

  it('forwards the filters, page size and cursor and nothing else', async () => {
    const query = captureUpstream({ runs: [], next_cursor: null, limit: 5 });

    await get('?agent_type_id=at-1&state=pending,active&limit=5&cursor=abc&tenant_id=other&x=1');

    expect(Object.fromEntries(query())).toEqual({
      agent_type_id: 'at-1',
      state: 'pending,active',
      limit: '5',
      cursor: 'abc',
    });
  });

  it('joins repeated state keys into one list', async () => {
    const query = captureUpstream({ runs: [], next_cursor: null, limit: 50 });
    await get('?state=failed&state=pending');
    expect(query().get('state')).toBe('failed,pending');
  });

  it('returns the page, the next cursor and resolved starter names', async () => {
    captureUpstream({
      runs: [run('run-1', 'user-1'), run('run-2', 'svc-1')],
      next_cursor: 'next',
      limit: 2,
    });
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map([['user-1', 'Ada Lovelace']]));

    const res = await get('?limit=2');
    const body = await res.json();

    expect(res.status).toBe(200);
    expect(body.next_cursor).toBe('next');
    expect(body.data.map((r: { id: string }) => r.id)).toEqual(['run-1', 'run-2']);
    expect(body.data[0].created_by_name).toBe('Ada Lovelace');
    expect(body.data[1].created_by_name).toBeNull();
  });

  it('passes an upstream validation error through with its status', async () => {
    server.use(
      http.get(RUNS_URL, () =>
        HttpResponse.json(
          { code: 'invalid_field', message: 'limit must be a positive integer' },
          { status: 400 },
        ),
      ),
    );

    const res = await get('?limit=0');

    expect(res.status).toBe(400);
    expect((await res.json()).message).toBe('limit must be a positive integer');
  });

  it('returns 401 without a valid session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    expect((await get()).status).toBe(401);
  });
});

describe('GET /api/agents/types/{typeId}/runs', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map());
    server.use(
      http.get('http://test-agents/api/v1/agent-types/:id', ({ params }) =>
        params.id === 'coder'
          ? HttpResponse.json({ code: 'not_found', message: 'not found' }, { status: 404 })
          : HttpResponse.json({ id: params.id, slug: 'coder' }),
      ),
      http.get('http://test-agents/api/v1/agent-types', () =>
        HttpResponse.json({ agent_types: [{ id: 'at-1', slug: 'coder' }] }),
      ),
    );
  });

  const getForType = (typeId: string, query = '') =>
    GET_FOR_TYPE(new NextRequest(`http://localhost/api/agents/types/${typeId}/runs${query}`), {
      params: Promise.resolve({ typeId }),
    } as never);

  it('pins the agent type from the path and forwards the page position', async () => {
    const query = captureUpstream({ runs: [run('run-3')], next_cursor: null, limit: 50 });

    const res = await getForType('coder', '?agent_type_id=at-other&state=failed&cursor=abc');
    const body = await res.json();

    expect(Object.fromEntries(query())).toEqual({
      agent_type_id: 'at-1',
      state: 'failed',
      cursor: 'abc',
    });
    expect(body.data.map((r: { id: string }) => r.id)).toEqual(['run-3']);
    expect(body.next_cursor).toBeNull();
  });
});

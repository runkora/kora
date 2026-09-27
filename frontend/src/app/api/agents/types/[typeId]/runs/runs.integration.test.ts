/**
 * Integration tests for GET /api/agents/types/[typeId]/runs.
 *
 * Strategy: `@orbit/server/config`, `@orbit/server/session` and
 * `resolveUserDisplayNames` are vi.mock'd. MSW answers the agent-type lookups
 * and the upstream `/api/v1/runs` call, recording the query it received.
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

const AGENTS = 'http://test-agents/api/v1';
const AGENT_TYPE = { id: 'at-uuid-1', slug: 'coder', tenant_id: 'test-tenant', name: 'Coder' };

function get(typeId: string, query = ''): Promise<Response> {
  return GET(new NextRequest(`http://localhost/api/agents/types/${typeId}/runs${query}`), {
    params: Promise.resolve({ typeId }),
  });
}

/** Serves the agent-type lookups: by id for AGENT_TYPE.id, 404 otherwise. */
function serveAgentTypes() {
  server.use(
    http.get(`${AGENTS}/agent-types/:id`, ({ params }) =>
      params.id === AGENT_TYPE.id
        ? HttpResponse.json(AGENT_TYPE)
        : HttpResponse.json({ code: 'not_found', message: 'not found' }, { status: 404 }),
    ),
    http.get(`${AGENTS}/agent-types`, () => HttpResponse.json({ agent_types: [AGENT_TYPE] })),
  );
}

function captureRuns(body: Record<string, unknown>): { query: () => URLSearchParams } {
  let seen = new URLSearchParams();
  server.use(
    http.get(`${AGENTS}/runs`, ({ request }) => {
      seen = new URL(request.url).searchParams;
      return HttpResponse.json(body);
    }),
  );
  return { query: () => seen };
}

describe('GET /api/agents/types/[typeId]/runs', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue({
      accessToken: 'test-access-token',
      tenantId: 'test-tenant',
    } as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
    vi.mocked(resolveUserDisplayNames).mockResolvedValue(new Map());
    serveAgentTypes();
  });

  it('resolves a slug to the agent type id and forwards limit and cursor', async () => {
    const upstream = captureRuns({ runs: [], has_more: false, next_cursor: null, limit: 25 });

    const res = await get('coder', '?limit=25&cursor=2026-09-01T00%3A00%3A00Z%7Crun-9');

    expect(res.status).toBe(200);
    const q = upstream.query();
    expect(q.get('agent_type_id')).toBe(AGENT_TYPE.id);
    expect(q.get('tenant_id')).toBe('test-tenant');
    expect(q.get('limit')).toBe('25');
    expect(q.get('cursor')).toBe('2026-09-01T00:00:00Z|run-9');
  });

  it('accepts the agent type id directly', async () => {
    const upstream = captureRuns({ runs: [], has_more: false, next_cursor: null, limit: 25 });

    await get(AGENT_TYPE.id);

    expect(upstream.query().get('agent_type_id')).toBe(AGENT_TYPE.id);
  });

  it('ignores a caller-supplied agent_type_id in favour of the path', async () => {
    const upstream = captureRuns({ runs: [], has_more: false, next_cursor: null, limit: 25 });

    await get('coder', '?agent_type_id=someone-elses-agent');

    expect(upstream.query().get('agent_type_id')).toBe(AGENT_TYPE.id);
  });

  it('returns 404 for an unknown agent type', async () => {
    const res = await get('no-such-agent');

    expect(res.status).toBe(404);
    expect((await res.json()).code).toBe('not_found');
  });

  it('returns the page with its pagination fields', async () => {
    captureRuns({
      runs: [
        {
          id: 'run-1',
          tenant_id: 'test-tenant',
          agent_type_id: AGENT_TYPE.id,
          state: 'completed',
          created_by: 'user-1',
          created_at: '2026-09-01T00:00:00Z',
          updated_at: '2026-09-01T00:00:00Z',
        },
      ],
      has_more: true,
      next_cursor: '2026-09-01T00:00:00Z|run-1',
      limit: 1,
    });

    const body = await (await get('coder', '?limit=1')).json();

    expect(body.data).toHaveLength(1);
    expect(body.has_more).toBe(true);
    expect(body.next_cursor).toBe('2026-09-01T00:00:00Z|run-1');
  });

  it('passes an upstream error through with its status', async () => {
    server.use(
      http.get(`${AGENTS}/runs`, () =>
        HttpResponse.json({ code: 'unavailable', message: 'Service Unavailable' }, { status: 503 }),
      ),
    );

    const res = await get('coder');

    expect(res.status).toBe(503);
    expect((await res.json()).message).toBe('Service Unavailable');
  });
});

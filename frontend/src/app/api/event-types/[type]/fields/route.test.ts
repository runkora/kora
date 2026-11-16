/**
 * Tests for GET /api/event-types/[type]/fields.
 *
 * `@runkora/server/config` is mocked to set `koraEventsUrl`, `@runkora/server/session`
 * controls the session, and MSW stands in for kora-events.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraEventsUrl: 'http://test-events-svc/v1' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET } from './route';
import { getSession, isSessionValid } from '@runkora/server/session';

const TYPES = 'http://test-events-svc/v1/events/types';

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@runkora.test', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['triggers:read'], evaluatedAt: 0 },
  };
}

function get(type: string) {
  return GET(new NextRequest(`http://localhost/api/event-types/${type}/fields`), {
    params: Promise.resolve({ type }),
  } as never);
}

describe('GET /api/event-types/[type]/fields', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);
  });

  it('returns 401 without a session', async () => {
    vi.mocked(isSessionValid).mockReturnValue(false);
    expect((await get('agents.run.failed.v1')).status).toBe(401);
  });

  it("returns the type's fields from its schema", async () => {
    let auth = '';
    server.use(
      http.get(`${TYPES}/agents.run.failed.v1`, ({ request }) => {
        auth = request.headers.get('Authorization') ?? '';
        return HttpResponse.json({
          properties: {
            koraagent: { type: 'string', description: 'The agent slug.' },
            data: {
              type: 'object',
              properties: {
                entity: {
                  type: 'object',
                  properties: { state: { type: 'string', enum: ['failed', 'idle'] } },
                },
              },
            },
          },
        });
      }),
    );
    const res = await get('agents.run.failed.v1');
    expect(res.status).toBe(200);
    expect(auth).toBe('Bearer test-access-token');
    const body = (await res.json()) as { data: { fields: { path: string; enum?: string[] }[] } };
    expect(body.data.fields.map((f) => f.path)).toEqual([
      'data',
      'data.entity',
      'data.entity.state',
      'koraagent',
    ]);
    expect(body.data.fields.find((f) => f.path === 'data.entity.state')?.enum).toEqual([
      'failed',
      'idle',
    ]);
  });

  it('passes an upstream 404 through', async () => {
    server.use(
      http.get(`${TYPES}/nope.v1`, () =>
        HttpResponse.json(
          { code: 'resource_not_found', message: 'Unknown event type.' },
          { status: 404 },
        ),
      ),
    );
    expect((await get('nope.v1')).status).toBe(404);
  });
});

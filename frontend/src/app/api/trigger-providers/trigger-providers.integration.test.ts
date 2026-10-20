/**
 * Integration tests for the event types routes: GET /api/trigger-providers/[provider]/event-types
 * and GET /api/trigger-sources/[id]/event-types.
 *
 * The console reads a provider's category and sender roles from these, so
 * each route must pass both through, and keep `senders` absent when the
 * backend omits it.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@runkora/test-utils';

vi.mock('@runkora/server/config', () => ({
  config: { koraTriggersUrl: 'http://test-triggers-svc' },
}));

vi.mock('@runkora/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn().mockReturnValue(true),
}));

import { GET as providerEventTypes } from './[provider]/event-types/route';
import { GET as sourceEventTypes } from '../trigger-sources/[id]/event-types/route';
import { GET as listSources } from '../trigger-sources/route';
import { getSession } from '@runkora/server/session';

const BASE = 'http://test-triggers-svc';

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['triggers:read'], evaluatedAt: 0 },
  };
}

const GITHUB_ROLES = [
  { value: 'OWNER', label: 'Owner', description: 'Owns the repository' },
  { value: 'MEMBER', label: 'Member', description: 'Member of the organization' },
];

describe('event types routes', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
  });

  it("passes a provider's category and sender roles through", async () => {
    server.use(
      http.get(`${BASE}/trigger-providers/github/event-types`, () =>
        HttpResponse.json({
          declared: ['push'],
          category: 'webhook',
          senders: { roles: GITHUB_ROLES },
        }),
      ),
    );
    const res = await providerEventTypes(new NextRequest('http://localhost/x'), {
      params: Promise.resolve({ provider: 'github' }),
    } as never);
    const body = (await res.json()) as { data: unknown };
    expect(body.data).toEqual({
      declared: ['push'],
      category: 'webhook',
      senders: { roles: GITHUB_ROLES },
    });
  });

  it('keeps senders absent for a provider that identifies no sender', async () => {
    server.use(
      http.get(`${BASE}/trigger-providers/alertmanager/event-types`, () =>
        HttpResponse.json({ declared: ['alert.firing'], category: 'webhook' }),
      ),
    );
    const res = await providerEventTypes(new NextRequest('http://localhost/x'), {
      params: Promise.resolve({ provider: 'alertmanager' }),
    } as never);
    const body = (await res.json()) as { data: Record<string, unknown> };
    expect(body.data).not.toHaveProperty('senders');
    expect(body.data.category).toBe('webhook');
  });

  it("passes a chat source's category, observed types and empty roles through", async () => {
    server.use(
      http.get(`${BASE}/trigger-sources/src-9/event-types`, () =>
        HttpResponse.json({
          declared: ['mention', 'direct_message'],
          observed: ['mention'],
          category: 'chat',
          senders: { roles: [] },
        }),
      ),
    );
    const res = await sourceEventTypes(new NextRequest('http://localhost/x'), {
      params: Promise.resolve({ id: 'src-9' }),
    } as never);
    const body = (await res.json()) as { data: unknown };
    expect(body.data).toEqual({
      declared: ['mention', 'direct_message'],
      observed: ['mention'],
      category: 'chat',
      senders: { roles: [] },
    });
  });

  it('passes each source category through on the source list', async () => {
    server.use(
      http.get(`${BASE}/trigger-sources`, () =>
        HttpResponse.json({
          sources: [
            { id: 's1', name: 'acme-slack', provider: 'slack', category: 'chat' },
            { id: 's2', name: 'acme/widgets', provider: 'github', category: 'webhook' },
          ],
        }),
      ),
    );
    const res = await listSources(new NextRequest('http://localhost/api/trigger-sources'), {
      params: Promise.resolve({}),
    } as never);
    const body = (await res.json()) as { data: { category: string }[] };
    expect(body.data.map((s) => s.category)).toEqual(['chat', 'webhook']);
  });
});

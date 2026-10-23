import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@runkora/test-utils/msw/server';

/**
 * Unit tests for fetchAllTenantUsers — walks pages of the identity
 * GET /identity/users endpoint until every user in the tenant has been
 * collected, since the identity API clamps `limit` to 100 server-side and
 * returns only one page per call (see issue #221: users past position 100,
 * sorted by display_name, were silently dropped when the caller assumed a
 * single limit=500 request returned everyone).
 */

const API_URL = 'http://kora-api.test';

function user(id: string) {
  return { id, email: `${id}@example.com`, display_name: id };
}

let fetchAllTenantUsers: typeof import('./tokens-server').fetchAllTenantUsers;

beforeAll(async () => {
  vi.stubEnv('KORA_API_URL', API_URL);
  vi.stubEnv('KORA_TRIGGERS_URL', 'http://kora-triggers.test');
  vi.stubEnv('INFERENCE_GATEWAY_URL', 'http://inference-gateway.test');
  vi.stubEnv('POLICY_GATEWAY_URL', 'http://policy-gateway.test');
  vi.stubEnv('POLICY_SERVICE_URL', 'http://policy-service.test');
  vi.stubEnv('KORA_AGENTS_URL', 'http://kora-agents.test');
  vi.stubEnv('TOOL_GATEWAY_URL', 'http://tool-gateway.test');

  ({ fetchAllTenantUsers } = await import('./tokens-server'));
});

afterAll(() => {
  vi.unstubAllEnvs();
});

describe('fetchAllTenantUsers', () => {
  it('returns all users when the tenant has a single page', async () => {
    server.use(
      http.get(`${API_URL}/identity/users`, ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get('page')).toBe('1');
        expect(url.searchParams.get('limit')).toBe('100');
        return HttpResponse.json({
          users: [user('u1'), user('u2')],
          total: 2,
          page: 1,
          limit: 100,
        });
      }),
    );

    const result = await fetchAllTenantUsers('token-abc');

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.users.map((u) => u.id)).toEqual(['u1', 'u2']);
    }
  });

  it('walks additional pages until every user has been collected', async () => {
    const allUsers = Array.from({ length: 101 }, (_, i) => user(`u${i + 1}`));
    let requestCount = 0;

    server.use(
      http.get(`${API_URL}/identity/users`, ({ request }) => {
        requestCount++;
        const url = new URL(request.url);
        const page = Number(url.searchParams.get('page'));
        const limit = Number(url.searchParams.get('limit'));
        const start = (page - 1) * limit;
        return HttpResponse.json({
          users: allUsers.slice(start, start + limit),
          total: allUsers.length,
          page,
          limit,
        });
      }),
    );

    const result = await fetchAllTenantUsers('token-abc');

    expect(requestCount).toBe(2);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.users).toHaveLength(101);
      expect(result.users.map((u) => u.id)).toEqual(allUsers.map((u) => u.id));
    }
  });

  it('stops once the accumulated users reach total, without an extra request', async () => {
    let requestCount = 0;
    server.use(
      http.get(`${API_URL}/identity/users`, () => {
        requestCount++;
        return HttpResponse.json({
          users: [user('u1')],
          total: 1,
          page: 1,
          limit: 100,
        });
      }),
    );

    const result = await fetchAllTenantUsers('token-abc');

    expect(requestCount).toBe(1);
    expect(result.ok).toBe(true);
  });

  it('returns ok:false when a page request fails', async () => {
    server.use(
      http.get(`${API_URL}/identity/users`, () => new HttpResponse('boom', { status: 500 })),
    );

    const result = await fetchAllTenantUsers('token-abc');

    expect(result.ok).toBe(false);
  });
});

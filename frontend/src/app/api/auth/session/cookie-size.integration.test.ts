// @vitest-environment node
/**
 * Seals real iron-session cookies (no iron-session mock) to check that an eligible set too large
 * for the 4096-byte cookie limit is dropped, so sign-in still succeeds and the switcher hides.
 */
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { unsealData } from 'iron-session';
import { server } from '@runkora/test-utils';
import { createBackendSessionResponse } from '@runkora/test-utils/factories/session';
import { SESSION_COOKIE_NAME, type SessionPayload } from '@runkora/server/auth-config';

const jar = vi.hoisted(() => {
  const values = new Map<string, string>();
  return {
    values,
    get: (name: string) => (values.has(name) ? { name, value: values.get(name)! } : undefined),
    set: (name: string, value: string) => void values.set(name, value),
  };
});

vi.mock('next/headers', () => ({ cookies: async () => jar }));
vi.mock('@runkora/server/config', () => ({
  config: { koraApiUrl: 'http://test-kora-api', policyGatewayUrl: 'http://test-policy-gateway' },
}));
vi.mock('@runkora/server/session', () => ({ getSession: vi.fn(), isSessionValid: vi.fn() }));

import { POST } from './route';

const SECRET = 'test-bff-secret-that-is-at-least-32-chars!';

beforeAll(() => vi.stubEnv('BFF_SESSION_SECRET', SECRET));
afterAll(() => vi.unstubAllEnvs());

function tenants(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: `kora-tenant-example-${i}`,
    display_name: `Example Platform Engineering and Developer Experience ${i}`,
  }));
}

async function signInWith(count: number) {
  jar.values.clear();
  server.use(
    http.post('http://test-kora-api/identity/session', () =>
      HttpResponse.json({ ...createBackendSessionResponse(), tenants: tenants(count) }),
    ),
  );
  const res = await POST(
    new Request('http://localhost/api/auth/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id_token: 'tok', tenant_id: 'kora-tenant-example' }),
    }),
  );
  const sealed = jar.values.get(SESSION_COOKIE_NAME);
  const session = sealed ? await unsealData<SessionPayload>(sealed, { password: SECRET }) : null;
  return { status: res.status, sealed, session };
}

describe('session cookie size', () => {
  it('keeps a small eligible set in the cookie', async () => {
    const { status, session } = await signInWith(2);
    expect(status).toBe(200);
    expect(session?.tenants).toHaveLength(2);
  });

  it('seals without the eligible set when it would push the cookie past 4096 bytes', async () => {
    const { status, sealed, session } = await signInWith(60);
    expect(status).toBe(200);
    expect(sealed!.length).toBeLessThanOrEqual(4096);
    expect(session?.accessToken).toBeTruthy();
    expect(session).not.toHaveProperty('tenants');
  });
});

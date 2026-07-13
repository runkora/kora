/**
 * Integration tests for GET /api/dev-environment/exit.
 *
 * Strategy:
 * - `next/headers` is vi.mock'd with a `cookies()` returning an object whose
 *   `set` call is a tracked spy, so tests can assert the routing cookie is
 *   cleared with the exact attributes the gateway itself uses to set it
 *   (devenvironment_app_ingress.go's `devEnvRouteCookieName`).
 * - `server-only` is shimmed by the Vitest alias so the bare import at the
 *   top of the route module does not throw in Node/jsdom.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextRequest } from 'next/server';

// ── Module mocks ──────────────────────────────────────────────────────────────

const { cookieSet } = vi.hoisted(() => ({ cookieSet: vi.fn() }));

vi.mock('next/headers', () => ({
  cookies: vi.fn().mockResolvedValue({ set: cookieSet }),
}));

// ── Import under test ─────────────────────────────────────────────────────────

import { GET } from './route';

function makeGetRequest(): NextRequest {
  return new NextRequest('http://localhost/api/dev-environment/exit');
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GET /api/dev-environment/exit', () => {
  beforeEach(() => {
    cookieSet.mockReset();
  });

  it('clears the orbit-devenv-route cookie', async () => {
    await GET(makeGetRequest());

    expect(cookieSet).toHaveBeenCalledOnce();
    expect(cookieSet).toHaveBeenCalledWith(
      'orbit-devenv-route',
      '',
      expect.objectContaining({ path: '/', httpOnly: true, sameSite: 'lax', maxAge: 0 }),
    );
  });

  it('redirects to /', async () => {
    const res = await GET(makeGetRequest());

    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('http://localhost/');
  });
});

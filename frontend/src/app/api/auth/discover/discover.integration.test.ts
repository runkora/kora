/**
 * Integration tests for GET /api/auth/discover?email=...
 *
 * Strategy:
 * - `@orbit/server/config` is vi.mock'd so `config.orbitApiUrl` resolves to
 *   `http://test-orbit-api` without triggering `requireEnv` at import time.
 * - MSW intercepts the upstream fetch to the Go identity service
 *   (`http://test-orbit-api/identity/tenants/discover`).
 * - `server-only` is shimmed by the Vitest alias so the bare import at the
 *   top of the route module does not throw in Node/jsdom.
 */
import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { NextRequest } from 'next/server';
import { server } from '@orbit/test-utils';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@orbit/server/config', () => ({
  config: { orbitApiUrl: 'http://test-orbit-api' },
}));

// ── Import under test ─────────────────────────────────────────────────────────

import { GET } from './route';

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeGetRequest(email?: string): NextRequest {
  const url = email
    ? `http://localhost/api/auth/discover?email=${encodeURIComponent(email)}`
    : 'http://localhost/api/auth/discover';
  return new NextRequest(url, { method: 'GET' });
}

const UPSTREAM_URL = 'http://test-orbit-api/identity/tenants/discover';

const tenantConfig = {
  tenant_id: 'acme-corp',
  issuer_url: 'https://auth.acme.com',
  client_id: 'orbit-acme',
  scopes: ['openid', 'profile', 'email'],
};

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GET /api/auth/discover', () => {
  describe('request validation', () => {
    it('returns 400 when email query parameter is absent', async () => {
      const res = await GET(makeGetRequest());
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('invalid_field');
      expect(body.message).toMatch(/email/i);
    });

    it('returns 400 when email is an empty string', async () => {
      const res = await GET(makeGetRequest(''));
      expect(res.status).toBe(400);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('invalid_field');
    });
  });

  describe('successful upstream response', () => {
    it('forwards the tenant config body with a 200 status', async () => {
      server.use(
        http.get(UPSTREAM_URL, () => HttpResponse.json(tenantConfig, { status: 200 })),
      );

      const res = await GET(makeGetRequest('alice@acme.com'));
      expect(res.status).toBe(200);
      const body = (await res.json()) as typeof tenantConfig;
      expect(body.tenant_id).toBe('acme-corp');
      expect(body.issuer_url).toBe('https://auth.acme.com');
      expect(body.client_id).toBe('orbit-acme');
      expect(body.scopes).toEqual(['openid', 'profile', 'email']);
    });

    it('forwards the email to the upstream as a query parameter', async () => {
      let capturedUrl: string | null = null;
      server.use(
        http.get(UPSTREAM_URL, ({ request }) => {
          capturedUrl = request.url;
          return HttpResponse.json(tenantConfig);
        }),
      );

      await GET(makeGetRequest('bob@example.com'));

      expect(capturedUrl).not.toBeNull();
      expect(new URL(capturedUrl!).searchParams.get('email')).toBe('bob@example.com');
    });

    it('forwards a non-200 upstream status verbatim (e.g. 404 tenant not found)', async () => {
      server.use(
        http.get(UPSTREAM_URL, () =>
          HttpResponse.json(
            { error: 'tenant_not_found', message: 'No tenant for that domain' },
            { status: 404 },
          ),
        ),
      );

      const res = await GET(makeGetRequest('unknown@missing.com'));
      expect(res.status).toBe(404);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('tenant_not_found');
    });
  });

  describe('upstream failure handling', () => {
    it('returns 503 when the upstream is unreachable (network error)', async () => {
      server.use(http.get(UPSTREAM_URL, () => HttpResponse.error()));

      const res = await GET(makeGetRequest('alice@acme.com'));
      expect(res.status).toBe(503);
      const body = (await res.json()) as Record<string, unknown>;
      expect(body.error).toBe('service_unavailable');
    });
  });
});

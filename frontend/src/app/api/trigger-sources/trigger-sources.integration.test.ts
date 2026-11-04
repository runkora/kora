/**
 * Integration tests for POST /api/trigger-sources and PATCH /api/trigger-sources/[id].
 *
 * Both routes copy an explicit list of fields to kora-triggers, so the
 * write-only signing secret needs a test that each route forwards it.
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

import { POST } from './route';
import { PATCH } from './[id]/route';
import { getSession } from '@runkora/server/session';

const SOURCES_URL = 'http://test-triggers-svc/trigger-sources';

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@runkora.test', name: 'Test' },
    accessToken: 'test-access-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: ['triggers:write'], evaluatedAt: 0 },
  };
}

function makeRequest(method: string, url: string, body: unknown): NextRequest {
  return new NextRequest(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

describe('trigger source routes', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
  });

  it('forwards signing_secret on create', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(SOURCES_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'src-1', webhook_path: '/x' }, { status: 201 });
      }),
    );

    const res = await POST(
      makeRequest('POST', 'http://localhost/api/trigger-sources', {
        name: 'acme-slack',
        provider: 'slack',
        signing_secret: 'slack-secret',
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(res.status).toBe(201);
    expect(received).toEqual({
      name: 'acme-slack',
      provider: 'slack',
      config: {},
      signing_secret: 'slack-secret',
    });
  });

  it('omits signing_secret on create when the request has none', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.post(SOURCES_URL, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'src-1', webhook_path: '/x' }, { status: 201 });
      }),
    );

    await POST(
      makeRequest('POST', 'http://localhost/api/trigger-sources', {
        name: 'acme/widgets',
        provider: 'github',
      }),
      { params: Promise.resolve({}) } as never,
    );

    expect(received).not.toHaveProperty('signing_secret');
  });

  it('forwards signing_secret with a rotation', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.patch(`${SOURCES_URL}/src-1`, async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'src-1', webhook_path: '/x' });
      }),
    );

    const res = await PATCH(
      makeRequest('PATCH', 'http://localhost/api/trigger-sources/src-1', {
        rotate_secret: true,
        signing_secret: 'new-slack-secret',
      }),
      { params: Promise.resolve({ id: 'src-1' }) } as never,
    );

    expect(res.status).toBe(200);
    expect(received).toEqual({ rotate_secret: true, signing_secret: 'new-slack-secret' });
  });
});

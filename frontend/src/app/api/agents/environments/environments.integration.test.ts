/**
 * Integration tests for the environments BFF routes. MSW stands in for
 * orbit-agents and the session is mocked.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
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

import { POST } from './route';
import { PUT } from './[id]/route';
import { getSession } from '@runkora/server/session';

const session = {
  user: { sub: 'sub', email: 'test@orbit.local', name: 'Test' },
  accessToken: 'token',
  refreshToken: 'refresh',
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  tenantId: 'tenant',
};

describe('environments BFF routes', () => {
  beforeEach(() => {
    vi.mocked(getSession).mockResolvedValue(session as never);
  });

  it('forwards the sandbox image and size on create', async () => {
    let received: unknown;
    server.use(
      http.post('http://test-agents/api/v1/environments', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ id: 'env-1' }, { status: 201 });
      }),
    );
    const res = await POST(
      new NextRequest('http://localhost/api/agents/environments', {
        method: 'POST',
        body: JSON.stringify({
          slug: 'go',
          name: 'Go services',
          sandbox_image: 'acme-go',
          size: { cpu: '4', memory: '8Gi', disk: '20Gi' },
        }),
      }),
    );
    expect(res.status).toBe(201);
    expect(received).toEqual({
      slug: 'go',
      name: 'Go services',
      description: '',
      sandbox_image: 'acme-go',
      size: { cpu: '4', memory: '8Gi', disk: '20Gi' },
      egress_hosts: [],
      connectors: [],
    });
  });

  it('forwards the slug and the name on update', async () => {
    let received: unknown;
    server.use(
      http.put('http://test-agents/api/v1/environments/env-1', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ id: 'env-1' });
      }),
    );
    const res = await PUT(
      new NextRequest('http://localhost/api/agents/environments/env-1', {
        method: 'PUT',
        body: JSON.stringify({ slug: 'go', name: 'Go services' }),
      }),
      { params: Promise.resolve({ id: 'env-1' }) },
    );
    expect(res.status).toBe(200);
    expect(received).toMatchObject({ slug: 'go', name: 'Go services' });
  });
});

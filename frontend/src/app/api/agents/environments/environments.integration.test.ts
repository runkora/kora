/**
 * Integration tests for the environments BFF routes. MSW stands in for
 * orbit-agents; the session is mocked.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
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

import { POST } from './route';
import { DELETE } from './[id]/route';
import { getSession } from '@orbit/server/session';

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
          name: 'Go services',
          sandbox_image: 'acme-go',
          size: { cpu: '4', memory: '8Gi', disk: '20Gi' },
        }),
      }),
    );
    expect(res.status).toBe(201);
    expect(received).toEqual({
      name: 'Go services',
      description: '',
      sandbox_image: 'acme-go',
      size: { cpu: '4', memory: '8Gi', disk: '20Gi' },
      egress_hosts: [],
      connectors: [],
    });
  });

  it('passes the 409 for an attached environment through on delete', async () => {
    server.use(
      http.delete('http://test-agents/api/v1/environments/env-1', () =>
        HttpResponse.json(
          { code: 'conflict', message: 'environment is attached to an agent; detach it first' },
          { status: 409 },
        ),
      ),
    );
    const res = await DELETE(
      new NextRequest('http://localhost/api/agents/environments/env-1', { method: 'DELETE' }),
      { params: Promise.resolve({ id: 'env-1' }) },
    );
    expect(res.status).toBe(409);
  });
});

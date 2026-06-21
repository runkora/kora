import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextRequest, NextResponse } from 'next/server';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@orbit/server/session', () => ({
  getSession: vi.fn(),
  isSessionValid: vi.fn(),
}));

vi.mock('../../../(dashboard)/policy/_services/policy-server', () => ({
  fetchGrants: vi.fn(),
  createGrant: vi.fn(),
}));

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchGrants, createGrant } from '../../../(dashboard)/policy/_services/policy-server';
import { GET, POST } from './route';

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeSession(overrides: Record<string, unknown> = {}) {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: [], evaluatedAt: 0 },
    ...overrides,
  };
}

function makeRequest(body?: unknown): NextRequest {
  const req = new NextRequest('http://localhost/api/policy/grants', {
    method: 'POST',
  });

  if (body !== undefined) {
    // Mock json() method
    vi.spyOn(req, 'json').mockResolvedValue(body);
  }

  return req;
}

// ── GET Tests ─────────────────────────────────────────────────────────────────

describe('GET /api/policy/grants', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns 401 when session is invalid', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(false);

    const res = await GET();
    expect(res.status).toBe(401);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('unauthorized');
    expect(body.message).toContain('No valid session');
  });

  it('returns grants data when fetch succeeds', async () => {
    const session = makeSession();
    vi.mocked(getSession).mockResolvedValue(session as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const mockGrants = [
      {
        id: 'grant-1',
        principal_type: 'HUMAN',
        principal_id: 'user-123',
        scopes: ['inference:invoke'],
      },
      {
        id: 'grant-2',
        principal_type: 'AGENT',
        principal_id: 'agent-456',
        scopes: ['models:read'],
      },
    ];

    vi.mocked(fetchGrants).mockResolvedValue({
      ok: true,
      data: mockGrants,
    } as never);

    const res = await GET();
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toEqual(mockGrants);
  });

  it('passes correct token and tenant ID to fetchGrants', async () => {
    const session = makeSession({ accessToken: 'specific-token', tenantId: 'specific-tenant' });
    vi.mocked(getSession).mockResolvedValue(session as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(fetchGrants).mockResolvedValue({
      ok: true,
      data: [],
    } as never);

    await GET();

    expect(fetchGrants).toHaveBeenCalledWith('specific-token', 'specific-tenant');
  });

  it('returns 502 when upstream returns 5xx error', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(fetchGrants).mockResolvedValue({
      ok: false,
      status: 503,
      message: 'Service unavailable',
    } as never);

    const res = await GET();
    expect(res.status).toBe(502);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('upstream_error');
    expect(body.message).toContain('Failed to load grants');
  });

  it('returns 4xx when upstream returns 4xx error', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(fetchGrants).mockResolvedValue({
      ok: false,
      status: 403,
      message: 'Forbidden',
    } as never);

    const res = await GET();
    expect(res.status).toBe(403);

    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('upstream_error');
  });

  it('returns empty data array when no grants exist', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(fetchGrants).mockResolvedValue({
      ok: true,
      data: [],
    } as never);

    const res = await GET();
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toEqual([]);
  });
});

// ── POST Tests ────────────────────────────────────────────────────────────────

describe('POST /api/policy/grants', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns 401 when session is invalid', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(false);

    const req = makeRequest({
      principal_type: 'HUMAN',
      principal_id: 'user-123',
      scopes: ['inference:invoke'],
    });

    const res = await POST(req);
    expect(res.status).toBe(401);

    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('unauthorized');
  });

  it('returns 400 when request body is invalid JSON', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const req = new NextRequest('http://localhost/api/policy/grants', { method: 'POST' });
    vi.spyOn(req, 'json').mockRejectedValue(new Error('Invalid JSON'));

    const res = await POST(req);
    expect(res.status).toBe(400);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('bad_request');
    expect(body.message).toContain('Invalid JSON body');
  });

  it('creates grant with tenant_id from session when not provided', async () => {
    const session = makeSession({ tenantId: 'session-tenant' });
    vi.mocked(getSession).mockResolvedValue(session as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const requestBody = {
      principal_type: 'HUMAN',
      principal_id: 'user-123',
      scopes: ['inference:invoke'],
    };

    vi.mocked(createGrant).mockResolvedValue({
      ok: true,
      data: { id: 'grant-new', ...requestBody, tenant_id: 'session-tenant' },
    } as never);

    const req = makeRequest(requestBody);
    await POST(req);

    expect(createGrant).toHaveBeenCalledWith('test-token', {
      ...requestBody,
      tenant_id: 'session-tenant',
    });
  });

  it('uses tenant_id from request body when provided', async () => {
    const session = makeSession({ tenantId: 'session-tenant' });
    vi.mocked(getSession).mockResolvedValue(session as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const requestBody = {
      principal_type: 'HUMAN',
      principal_id: 'user-123',
      scopes: ['inference:invoke'],
      tenant_id: 'override-tenant',
    };

    vi.mocked(createGrant).mockResolvedValue({
      ok: true,
      data: { id: 'grant-new', ...requestBody },
    } as never);

    const req = makeRequest(requestBody);
    await POST(req);

    expect(createGrant).toHaveBeenCalledWith('test-token', {
      ...requestBody,
      tenant_id: 'override-tenant',
    });
  });

  it('returns 201 with grant data when creation succeeds', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const mockGrant = {
      id: 'grant-123',
      tenant_id: 'test-tenant',
      principal_type: 'AGENT',
      principal_id: 'agent-456',
      scopes: ['models:read', 'inference:invoke'],
      created_at: '2025-01-01T00:00:00Z',
    };

    vi.mocked(createGrant).mockResolvedValue({
      ok: true,
      data: mockGrant,
    } as never);

    const req = makeRequest({
      principal_type: 'AGENT',
      principal_id: 'agent-456',
      scopes: ['models:read', 'inference:invoke'],
    });

    const res = await POST(req);
    expect(res.status).toBe(201);

    const body = (await res.json()) as { data: unknown };
    expect(body.data).toEqual(mockGrant);
  });

  it('returns 502 when upstream returns 5xx error', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(createGrant).mockResolvedValue({
      ok: false,
      status: 500,
      message: 'Internal server error',
    } as never);

    const req = makeRequest({
      principal_type: 'HUMAN',
      principal_id: 'user-123',
      scopes: ['scope'],
    });

    const res = await POST(req);
    expect(res.status).toBe(502);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('upstream_error');
    expect(body.message).toBe('Internal server error');
  });

  it('returns 4xx when upstream returns validation error', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(createGrant).mockResolvedValue({
      ok: false,
      status: 422,
      message: 'Invalid principal type',
    } as never);

    const req = makeRequest({
      principal_type: 'INVALID',
      principal_id: 'user-123',
      scopes: ['scope'],
    });

    const res = await POST(req);
    expect(res.status).toBe(422);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('upstream_error');
    expect(body.message).toBe('Invalid principal type');
  });

  it('passes access token to createGrant', async () => {
    const session = makeSession({ accessToken: 'specific-token' });
    vi.mocked(getSession).mockResolvedValue(session as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    vi.mocked(createGrant).mockResolvedValue({
      ok: true,
      data: { id: 'grant-new' },
    } as never);

    const req = makeRequest({
      principal_type: 'HUMAN',
      principal_id: 'user-123',
      scopes: ['scope'],
    });

    await POST(req);

    expect(createGrant).toHaveBeenCalledWith(
      'specific-token',
      expect.objectContaining({
        principal_type: 'HUMAN',
        principal_id: 'user-123',
        scopes: ['scope'],
      }),
    );
  });

  it('handles budget assignment in request body', async () => {
    vi.mocked(getSession).mockResolvedValue(makeSession() as never);
    vi.mocked(isSessionValid).mockReturnValue(true);

    const requestBody = {
      principal_type: 'AGENT',
      principal_id: 'agent-789',
      scopes: ['inference:invoke'],
      budget: {
        limit_usd: 100.0,
        reset_cadence: 'weekly',
        per_run_limit_usd: 10.0,
      },
    };

    vi.mocked(createGrant).mockResolvedValue({
      ok: true,
      data: { id: 'grant-with-budget', ...requestBody, tenant_id: 'test-tenant' },
    } as never);

    const req = makeRequest(requestBody);
    const res = await POST(req);

    expect(res.status).toBe(201);
    expect(createGrant).toHaveBeenCalledWith(
      'test-token',
      expect.objectContaining({
        budget: requestBody.budget,
      }),
    );
  });
});

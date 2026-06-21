import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextRequest } from 'next/server';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@orbit/server/route-helpers', () => ({
  withAuth: (handler: unknown) => handler,
}));

vi.mock('../../../(dashboard)/model-gateway/_services/models-server', () => ({
  fetchRawModelsFromGateway: vi.fn(),
}));

vi.mock('../../../(dashboard)/policy/_services/policy-server', () => ({
  fetchEffectiveScopes: vi.fn(),
}));

import { fetchRawModelsFromGateway } from '../../../(dashboard)/model-gateway/_services/models-server';
import { fetchEffectiveScopes } from '../../../(dashboard)/policy/_services/policy-server';
import { GET } from './route';

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeRequest(): NextRequest {
  return new NextRequest('http://localhost/api/models');
}

function makeSession() {
  return {
    user: { sub: 'test-sub', email: 'test@orbit.local', name: 'Test' },
    accessToken: 'test-token',
    refreshToken: 'test-refresh',
    expiresAt: Math.floor(Date.now() / 1000) + 3600,
    tenantId: 'test-tenant',
    permissions: { scopes: [], evaluatedAt: 0 },
  };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('GET /api/models', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('returns filtered models when both upstream calls succeed', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet 4.6' },
      { id: 'anthropic/claude-opus-4-8', name: 'Claude Opus 4.8' },
      { id: 'openai/gpt-4o', name: 'GPT-4o' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: [
          'inference:anthropic:claude-sonnet-4-6:use',
          'inference:anthropic:claude-opus-4-8:use',
        ],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toHaveLength(2);
    expect(body.data[0]).toMatchObject({ id: 'anthropic/claude-sonnet-4-6' });
    expect(body.data[1]).toMatchObject({ id: 'anthropic/claude-opus-4-8' });
  });

  it('returns all models when no model-specific scopes present (broad access)', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet 4.6' },
      { id: 'openai/gpt-4o', name: 'GPT-4o' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    // No model-specific scopes - only general scopes like "inference:invoke"
    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: ['inference:invoke', 'models:read'],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    // All models should pass through since no model-specific scopes
    expect(body.data).toHaveLength(2);
  });

  it('filters out models without matching scopes', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet 4.6' },
      { id: 'anthropic/claude-opus-4-8', name: 'Claude Opus 4.8' },
      { id: 'openai/gpt-4o', name: 'GPT-4o' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    // Only has access to one model
    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: ['inference:anthropic:claude-sonnet-4-6:use'],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toHaveLength(1);
    expect(body.data[0]).toMatchObject({ id: 'anthropic/claude-sonnet-4-6' });
  });

  it('returns empty array when no models match scopes', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet 4.6' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    // Scopes for different models
    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: ['inference:openai:gpt-4o:use'],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toHaveLength(0);
  });

  it('returns 502 when fetchRawModelsFromGateway fails with 5xx error', async () => {
    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: false,
      status: 503,
      message: 'Service unavailable',
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: { scopes: [] },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(502);

    const body = (await res.json()) as { code: string; message: string };
    expect(body.code).toBe('upstream_error');
    expect(body.message).toContain('Failed to load models');
  });

  it('returns 4xx when fetchRawModelsFromGateway fails with 4xx error', async () => {
    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: false,
      status: 403,
      message: 'Forbidden',
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: { scopes: [] },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(403);

    const body = (await res.json()) as { code: string };
    expect(body.code).toBe('upstream_error');
  });

  it('returns all models when fetchEffectiveScopes fails', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet 4.6' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    // Scopes fetch fails - should default to empty array (broad access)
    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: false,
      status: 500,
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    // Should return all models when scopes unavailable (fail-open for convenience)
    expect(body.data).toHaveLength(1);
  });

  it('handles models with malformed IDs gracefully', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Valid' },
      { id: 'malformed-id', name: 'Invalid format' },
      { id: 'too/many/slashes', name: 'Too many slashes' },
      { id: '/leading-slash', name: 'Leading slash' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: ['inference:anthropic:claude-sonnet-4-6:use'],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    // Only valid model with matching scope should pass
    expect(body.data).toHaveLength(1);
    expect(body.data[0]).toMatchObject({ id: 'anthropic/claude-sonnet-4-6' });
  });

  it('filters correctly with multiple inference scope patterns', async () => {
    const mockModels = [
      { id: 'anthropic/claude-sonnet-4-6', name: 'Claude Sonnet' },
      { id: 'openai/gpt-4o', name: 'GPT-4o' },
    ];

    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: mockModels,
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: {
        scopes: [
          'inference:anthropic:claude-sonnet-4-6:use',
          'inference:openai:gpt-4o:use',
          'models:read', // non-inference scope should be ignored
          'inference:some-other:action', // inference scope but not :use pattern
        ],
      },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toHaveLength(2);
  });

  it('handles empty models array from gateway', async () => {
    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: [],
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: { scopes: ['inference:anthropic:claude-sonnet-4-6:use'] },
    } as never);

    const res = await GET(makeRequest(), makeSession() as never);
    expect(res.status).toBe(200);

    const body = (await res.json()) as { data: unknown[] };
    expect(body.data).toHaveLength(0);
  });

  it('calls both upstream services with the session access token', async () => {
    vi.mocked(fetchRawModelsFromGateway).mockResolvedValue({
      ok: true,
      models: [],
    } as never);

    vi.mocked(fetchEffectiveScopes).mockResolvedValue({
      ok: true,
      data: { scopes: [] },
    } as never);

    const session = makeSession();
    await GET(makeRequest(), session as never);

    expect(fetchRawModelsFromGateway).toHaveBeenCalledWith(session.accessToken);
    expect(fetchEffectiveScopes).toHaveBeenCalledWith(session.accessToken);
  });
});

import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils/msw/server';

/**
 * Unit tests for fetchAnalyticsPrincipalByIdFromGateway — the server-side
 * gateway call added for the admin drill-down view (GET
 * /v1/analytics/principals/{id} via the inference gateway).
 *
 * `@orbit/server/config` evaluates every required env var eagerly at module
 * load time (see src/server/config.ts — each property calls requireEnv()
 * directly in the object literal). That means the env vars it needs must be
 * stubbed *before* analytics-server.ts is first imported anywhere in this
 * file's module graph. A dynamic import inside beforeAll, after vi.stubEnv,
 * achieves that; a static top-level import would execute before any test
 * setup code runs and throw "Missing required environment variable".
 */

const GATEWAY_URL = 'http://inference-gateway.test';

function analyticsPayload(overrides: Record<string, unknown> = {}) {
  return {
    scope: 'personal',
    period_days: 30,
    granularity: 'daily',
    kpis: {
      total_requests: 0,
      total_tokens: 0,
      prompt_tokens: 0,
      completion_tokens: 0,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      estimated_cost_usd: 0,
      avg_latency_ms: 0,
      p95_latency_ms: 0,
      p99_latency_ms: 0,
      error_rate: 0,
      cache_hit_rate: 0,
    },
    buckets: [],
    cost_by_model: [],
    tokens_by_model: [],
    ...overrides,
  };
}

let fetchAnalyticsPrincipalByIdFromGateway: typeof import('./analytics-server').fetchAnalyticsPrincipalByIdFromGateway;

beforeAll(async () => {
  vi.stubEnv('ORBIT_API_URL', 'http://orbit-api.test');
  vi.stubEnv('INFERENCE_GATEWAY_URL', GATEWAY_URL);
  vi.stubEnv('POLICY_GATEWAY_URL', 'http://policy-gateway.test');
  vi.stubEnv('POLICY_SERVICE_URL', 'http://policy-service.test');
  vi.stubEnv('ORBIT_AGENTS_URL', 'http://orbit-agents.test');
  vi.stubEnv('ORBIT_TRIGGERS_URL', 'http://orbit-triggers.test');
  vi.stubEnv('TOOL_GATEWAY_URL', 'http://tool-gateway.test');
  vi.stubEnv('SKILLS_SERVICE_URL', 'http://skills-service.test');

  ({ fetchAnalyticsPrincipalByIdFromGateway } = await import('./analytics-server'));
});

afterAll(() => {
  vi.unstubAllEnvs();
});

describe('fetchAnalyticsPrincipalByIdFromGateway', () => {
  it('sends the bearer token and returns the parsed data on success', async () => {
    let capturedAuth: string | null = null;
    server.use(
      http.get(`${GATEWAY_URL}/analytics/principals/:id`, ({ request, params }) => {
        capturedAuth = request.headers.get('authorization');
        expect(params.id).toBe('principal-1');
        return HttpResponse.json(analyticsPayload({ period_days: 30 }));
      }),
    );

    const result = await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {
      days: 30,
      granularity: 'daily',
    });

    expect(capturedAuth).toBe('Bearer token-abc');
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.data.period_days).toBe(30);
    }
  });

  it('passes days and granularity as query params', async () => {
    let capturedUrl = '';
    server.use(
      http.get(`${GATEWAY_URL}/analytics/principals/:id`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(analyticsPayload({ period_days: 7, granularity: 'weekly' }));
      }),
    );

    await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {
      days: 7,
      granularity: 'weekly',
    });

    const url = new URL(capturedUrl);
    expect(url.searchParams.get('days')).toBe('7');
    expect(url.searchParams.get('granularity')).toBe('weekly');
  });

  it('omits query params that are not provided', async () => {
    let capturedUrl = '';
    server.use(
      http.get(`${GATEWAY_URL}/analytics/principals/:id`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(analyticsPayload());
      }),
    );

    await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {});

    expect(new URL(capturedUrl).search).toBe('');
  });

  it('URL-encodes the principal id in the gateway path', async () => {
    let capturedPathname = '';
    server.use(
      http.get(`${GATEWAY_URL}/analytics/principals/:id`, ({ request }) => {
        capturedPathname = new URL(request.url).pathname;
        return HttpResponse.json(analyticsPayload());
      }),
    );

    await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'user@example.com', {});

    expect(capturedPathname).toBe('/analytics/principals/user%40example.com');
  });

  it('returns ok:false with the response body as the message on a non-OK response', async () => {
    server.use(
      http.get(
        `${GATEWAY_URL}/analytics/principals/:id`,
        () => new HttpResponse('forbidden — missing platform:analytics:read', { status: 403 }),
      ),
    );

    const result = await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {});

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.status).toBe(403);
      expect(result.message).toBe('forbidden — missing platform:analytics:read');
    }
  });

  it('returns a 502 network-error message when the gateway is unreachable', async () => {
    server.use(http.get(`${GATEWAY_URL}/analytics/principals/:id`, () => HttpResponse.error()));

    const result = await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {});

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.status).toBe(502);
      expect(result.message).toBe('Failed to reach inference gateway');
    }
  });

  it('returns a 504 timeout message when the request aborts with a TimeoutError', async () => {
    // The real path uses AbortSignal.timeout(15_000) — waiting 15s for a real
    // timeout would make this test slow and flaky. Stub global fetch directly
    // to exercise the DOMException('TimeoutError') branch deterministically.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new DOMException('The operation timed out.', 'TimeoutError'))),
    );

    try {
      const result = await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {});
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.status).toBe(504);
        expect(result.message).toBe('Inference gateway timed out');
      }
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('returns a 502 invalid-response message when the body is not JSON', async () => {
    server.use(
      http.get(
        `${GATEWAY_URL}/analytics/principals/:id`,
        () => new HttpResponse('not json', { status: 200 }),
      ),
    );

    const result = await fetchAnalyticsPrincipalByIdFromGateway('token-abc', 'principal-1', {});

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.status).toBe(502);
      expect(result.message).toBe('Invalid response from inference gateway');
    }
  });
});

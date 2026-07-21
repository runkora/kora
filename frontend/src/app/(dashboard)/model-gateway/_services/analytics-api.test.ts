import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from '@orbit/test-utils/msw/server';
import { fetchAnalyticsPrincipalById } from './analytics-api';
import type { AnalyticsResponse } from '../_types';

/**
 * Unit tests for fetchAnalyticsPrincipalById — the admin drill-down client
 * fetch helper added alongside the Principals table "view usage" link.
 *
 * Exercises fetchFromBFF's branches (success / HTTP error / network error /
 * malformed body) through this function specifically, since it is the new
 * surface added for this feature.
 */

function analyticsResponse(overrides: Partial<AnalyticsResponse> = {}): AnalyticsResponse {
  return {
    scope: 'tenant',
    period_days: 30,
    granularity: 'daily',
    kpis: {
      total_requests: 100,
      total_tokens: 1000,
      prompt_tokens: 600,
      completion_tokens: 400,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      estimated_cost_usd: 1.23,
      avg_latency_ms: 250,
      p95_latency_ms: 500,
      p99_latency_ms: 800,
      error_rate: 0.01,
      cache_hit_rate: 0,
    },
    buckets: [],
    cost_by_model: [],
    tokens_by_model: [],
    ...overrides,
  };
}

describe('fetchAnalyticsPrincipalById', () => {
  it('requests the BFF endpoint for the given principal and returns the parsed data', async () => {
    let capturedUrl = '';
    server.use(
      http.get('/api/analytics/principals/:id', ({ request, params }) => {
        capturedUrl = request.url;
        expect(params.id).toBe('principal-1');
        return HttpResponse.json(analyticsResponse({ period_days: 7, granularity: 'weekly' }));
      }),
    );

    const result = await fetchAnalyticsPrincipalById('principal-1', {
      days: 7,
      granularity: 'weekly',
    });

    expect(result.error).toBeNull();
    expect(result.data?.period_days).toBe(7);
    expect(result.data?.granularity).toBe('weekly');
    expect(new URL(capturedUrl).searchParams.get('days')).toBe('7');
    expect(new URL(capturedUrl).searchParams.get('granularity')).toBe('weekly');
  });

  it('returns the message from a JSON error body', async () => {
    server.use(
      http.get('/api/analytics/principals/:id', () =>
        HttpResponse.json({ message: 'Principal not found' }, { status: 404 }),
      ),
    );

    const result = await fetchAnalyticsPrincipalById('missing-principal', {});

    expect(result.data).toBeNull();
    expect(result.error).toBe('Principal not found');
  });

  it('falls back to a generic status-code message when the error body is not JSON', async () => {
    server.use(
      http.get(
        '/api/analytics/principals/:id',
        () => new HttpResponse('gateway exploded', { status: 500 }),
      ),
    );

    const result = await fetchAnalyticsPrincipalById('principal-1', {});

    expect(result.data).toBeNull();
    expect(result.error).toBe('Request failed (500)');
  });

  it('returns a network-error message when the request cannot reach the server', async () => {
    server.use(http.get('/api/analytics/principals/:id', () => HttpResponse.error()));

    const result = await fetchAnalyticsPrincipalById('principal-1', {});

    expect(result.data).toBeNull();
    expect(result.error).toBe('Network error — could not reach the server.');
  });

  it('returns an invalid-response error when the success body is not JSON', async () => {
    server.use(
      http.get(
        '/api/analytics/principals/:id',
        () => new HttpResponse('not json', { status: 200 }),
      ),
    );

    const result = await fetchAnalyticsPrincipalById('principal-1', {});

    expect(result.data).toBeNull();
    expect(result.error).toBe('Invalid response from server.');
  });

  it('URL-encodes special characters in the principal id', async () => {
    let capturedPathname = '';
    server.use(
      http.get('/api/analytics/principals/:id', ({ request }) => {
        capturedPathname = new URL(request.url).pathname;
        return HttpResponse.json(analyticsResponse());
      }),
    );

    await fetchAnalyticsPrincipalById('user@example.com', {});

    expect(capturedPathname).toBe('/api/analytics/principals/user%40example.com');
  });
});

import { renderHook, act, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { server } from '@runkora/test-utils/msw/server';
import { usePrincipalAnalytics } from './use-principal-analytics';
import type { AnalyticsResponse } from '../_types';

/**
 * Unit tests for usePrincipalAnalytics — powers the admin drill-down view
 * (GET /v1/analytics/principals/{id}). Mirrors useAnalyticsMe/useAnalytics
 * (use-analytics.ts) but with a principalId fixed for the hook's lifetime
 * instead of exposed as mutable filter state.
 */

function analyticsResponse(overrides: Partial<AnalyticsResponse> = {}): AnalyticsResponse {
  return {
    scope: 'tenant',
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

describe('usePrincipalAnalytics — initial state', () => {
  it('starts idle with no data and no error when neither is provided', () => {
    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', null));

    expect(result.current.status).toBe('idle');
    expect(result.current.data).toBeNull();
    expect(result.current.error).toBeNull();
    expect(result.current.days).toBe(30);
    expect(result.current.granularity).toBe('daily');
  });

  it('starts success and derives days/granularity from initialData', () => {
    const data = analyticsResponse({ period_days: 7, granularity: 'weekly' });
    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', data));

    expect(result.current.status).toBe('success');
    expect(result.current.data).toBe(data);
    expect(result.current.days).toBe(7);
    expect(result.current.granularity).toBe('weekly');
  });

  it('starts in error status when initialError is provided, even with initialData present', () => {
    // Mirrors useAnalytics/useAnalyticsMe: initialError takes precedence over
    // initialData when deriving the starting status.
    const data = analyticsResponse();
    const { result } = renderHook(() =>
      usePrincipalAnalytics('principal-1', data, 'Session expired'),
    );

    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('Session expired');
  });

  it('prefers an explicit initialDays over the value derived from initialData', () => {
    const data = analyticsResponse({ period_days: 7 });
    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', data, null, 90));

    expect(result.current.days).toBe(90);
  });
});

describe('usePrincipalAnalytics — setDays', () => {
  it('updates days immediately and refetches for the new period', async () => {
    const initial = analyticsResponse({ period_days: 30 });
    let capturedUrl = '';
    server.use(
      http.get('/api/analytics/principals/:id', ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(analyticsResponse({ period_days: 90 }));
      }),
    );

    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', initial));

    act(() => {
      result.current.setDays(90);
    });

    // days flips synchronously; status flips to loading before the fetch resolves.
    expect(result.current.days).toBe(90);

    await waitFor(() => expect(result.current.status).toBe('success'));

    expect(result.current.data?.period_days).toBe(90);
    const url = new URL(capturedUrl, 'http://localhost');
    expect(url.pathname).toBe('/api/analytics/principals/principal-1');
    expect(url.searchParams.get('days')).toBe('90');
    expect(url.searchParams.get('granularity')).toBe('daily');
  });

  it('sets error status and preserves the previous data when the refetch fails', async () => {
    const initial = analyticsResponse({ period_days: 30 });
    server.use(
      http.get('/api/analytics/principals/:id', () =>
        HttpResponse.json({ message: 'Principal not found' }, { status: 404 }),
      ),
    );

    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', initial));

    act(() => {
      result.current.setDays(7);
    });

    await waitFor(() => expect(result.current.status).toBe('error'));

    expect(result.current.error).toBe('Principal not found');
    expect(result.current.data).toBe(initial);
  });
});

describe('usePrincipalAnalytics — setGranularity', () => {
  it('updates granularity immediately and refetches using the current days', async () => {
    const initial = analyticsResponse({ period_days: 30, granularity: 'daily' });
    let capturedUrl = '';
    server.use(
      http.get('/api/analytics/principals/:id', ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(analyticsResponse({ period_days: 30, granularity: 'weekly' }));
      }),
    );

    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', initial));

    act(() => {
      result.current.setGranularity('weekly');
    });

    expect(result.current.granularity).toBe('weekly');

    await waitFor(() => expect(result.current.status).toBe('success'));

    const url = new URL(capturedUrl, 'http://localhost');
    expect(url.searchParams.get('days')).toBe('30');
    expect(url.searchParams.get('granularity')).toBe('weekly');
  });
});

describe('usePrincipalAnalytics — refresh', () => {
  it('refetches using the current days/granularity without changing filter state', async () => {
    const initial = analyticsResponse({ period_days: 30, granularity: 'daily' });
    let requestCount = 0;
    server.use(
      http.get('/api/analytics/principals/:id', () => {
        requestCount += 1;
        return HttpResponse.json(analyticsResponse({ period_days: 30, granularity: 'daily' }));
      }),
    );

    const { result } = renderHook(() => usePrincipalAnalytics('principal-1', initial));

    await act(async () => {
      await result.current.refresh();
    });

    expect(requestCount).toBe(1);
    expect(result.current.days).toBe(30);
    expect(result.current.granularity).toBe('daily');
    expect(result.current.status).toBe('success');
  });
});

import type { ComponentProps } from 'react';
import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import userEvent from '@testing-library/user-event';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../../../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../../../../_preferences/preferences.schema';
import { PrincipalUsageView } from './principal-usage-view';
import type { AnalyticsResponse } from '../../../../_types';

/**
 * Integration tests for PrincipalUsageView — the admin drill-down page body
 * added for the "view usage" link on the Principals table.
 *
 * Renders the full tree (AnalyticsDashboard -> AnalyticsPeriodControls ->
 * PeriodSelector), so a <PreferencesProvider> ancestor is required: PeriodSelector
 * calls usePreferences() unconditionally on every render, regardless of which
 * control is interacted with.
 */

function analyticsResponse(overrides: Partial<AnalyticsResponse> = {}): AnalyticsResponse {
  return {
    scope: 'tenant',
    period_days: 30,
    granularity: 'daily',
    kpis: {
      total_requests: 10,
      total_tokens: 100,
      prompt_tokens: 60,
      completion_tokens: 40,
      cache_read_tokens: 0,
      cache_creation_tokens: 0,
      estimated_cost_usd: 0.5,
      avg_latency_ms: 100,
      p95_latency_ms: 200,
      p99_latency_ms: 300,
      error_rate: 0,
      cache_hit_rate: 0,
    },
    buckets: [],
    cost_by_model: [],
    tokens_by_model: [],
    ...overrides,
  };
}

function renderView(props: Partial<ComponentProps<typeof PrincipalUsageView>> = {}) {
  return renderWithProviders(
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
      <PrincipalUsageView
        principalId="principal-1"
        displayName={null}
        email={null}
        initialData={null}
        initialError={null}
        {...props}
      />
    </PreferencesProvider>,
  );
}

describe('PrincipalUsageView — label derivation', () => {
  it('uses displayName as the primary label and email as the secondary label when both are present', () => {
    renderView({ displayName: 'Alice Admin', email: 'alice@example.com' });

    expect(screen.getByText('Usage — Alice Admin')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Full inference activity for Alice Admin (alice@example.com) — spend, tokens, and request volume.',
      ),
    ).toBeInTheDocument();
  });

  it('falls back to email as the primary label with no secondary label when displayName is absent', () => {
    renderView({ displayName: null, email: 'alice@example.com' });

    expect(screen.getByText('Usage — alice@example.com')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Full inference activity for alice@example.com — spend, tokens, and request volume.',
      ),
    ).toBeInTheDocument();
  });

  it('uses displayName alone with no secondary label when email is absent', () => {
    renderView({ displayName: 'Alice Admin', email: null });

    expect(screen.getByText('Usage — Alice Admin')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Full inference activity for Alice Admin — spend, tokens, and request volume.',
      ),
    ).toBeInTheDocument();
  });

  it('falls back to the principalId for both labels when neither displayName nor email is present', () => {
    // Documents existing behaviour rather than prescribing it: when neither
    // identity field is available, primaryLabel and secondaryLabel both
    // resolve to principalId, so it is shown twice ("principal-1 (principal-1)").
    renderView({ principalId: 'principal-1', displayName: null, email: null });

    expect(screen.getByText('Usage — principal-1')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Full inference activity for principal-1 (principal-1) — spend, tokens, and request volume.',
      ),
    ).toBeInTheDocument();
  });
});

describe('PrincipalUsageView — breadcrumb', () => {
  it('links back to the analytics tab with the principals tab selected', () => {
    renderView();

    const link = screen.getByRole('link', { name: /Analytics/i });
    expect(link).toHaveAttribute('href', '/model-gateway/analytics?tab=principals');
  });
});

describe('PrincipalUsageView — period controls drive a live refetch', () => {
  it('loads and renders KPI data after switching granularity', async () => {
    let capturedUrl = '';
    server.use(
      http.get('/api/analytics/principals/:id', ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(analyticsResponse({ granularity: 'weekly' }));
      }),
    );

    renderView();

    // No data yet — KPI cards are gated on `data` being non-null.
    expect(screen.queryByText('Cost')).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Weekly' }));

    await waitFor(() => expect(screen.getByText('Cost')).toBeInTheDocument());

    const url = new URL(capturedUrl, 'http://localhost');
    expect(url.pathname).toBe('/api/analytics/principals/principal-1');
    // No initialData was provided, so days falls back to the 30-day default.
    expect(url.searchParams.get('days')).toBe('30');
    expect(url.searchParams.get('granularity')).toBe('weekly');
  });

  it('shows the error banner and preserves the previous view when the refetch fails', async () => {
    server.use(
      http.get('/api/analytics/principals/:id', () =>
        HttpResponse.json({ message: 'Principal not found' }, { status: 404 }),
      ),
    );

    renderView({ initialData: analyticsResponse({ granularity: 'daily' }) });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Monthly' }));

    await waitFor(() => expect(screen.getByText('Principal not found')).toBeInTheDocument());

    // The KPI cards from the prior successful load are still shown alongside
    // the error banner — the hook does not clear `data` on a failed refetch.
    expect(screen.getByText('Cost')).toBeInTheDocument();
  });
});

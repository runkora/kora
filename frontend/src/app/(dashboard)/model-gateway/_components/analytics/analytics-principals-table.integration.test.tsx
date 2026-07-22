import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import userEvent from '@testing-library/user-event';
import { screen, waitFor } from '@testing-library/react';
import { renderWithProviders, server } from '@orbit/test-utils';
import { PreferencesProvider } from '../../../_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../../../_preferences/preferences.schema';
import { AnalyticsPrincipalsTable } from './analytics-principals-table';
import type { AnalyticsPrincipalsPageResponse, PrincipalUsageRow } from '../../_types';

/**
 * Integration tests for AnalyticsPrincipalsTable — the admin Principals list.
 *
 * Covers the "whole row is clickable" behaviour: rows navigate via the
 * DataTable's onRowClick (router.push) rather than a <Link> nested in the
 * user cell. next/navigation is vi.mock'd so router.push is a spy.
 *
 * The app-level DataTable wrapper (src/components/data-table.tsx) reads
 * column-visibility preferences via usePreferences(), so a <PreferencesProvider>
 * ancestor is required — same requirement as PrincipalUsageView's tests.
 */

vi.mock('next/navigation', () => ({
  useRouter: vi.fn(),
}));

import { useRouter } from 'next/navigation';

function renderTable(days: 7 | 30 | 90 = 30) {
  return renderWithProviders(
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
      <AnalyticsPrincipalsTable days={days} />
    </PreferencesProvider>,
  );
}

function principalRow(overrides: Partial<PrincipalUsageRow> = {}): PrincipalUsageRow {
  return {
    principal_id: 'principal-1',
    principal_type: 'HUMAN',
    request_count: 10,
    total_tokens: 100,
    prompt_tokens: 60,
    completion_tokens: 40,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
    estimated_cost_usd: 0.5,
    error_rate: 0,
    avg_latency_ms: 120,
    last_active_at: '2026-07-01T00:00:00Z',
    email: null,
    display_name: null,
    ...overrides,
  };
}

function pageResponse(data: PrincipalUsageRow[]): AnalyticsPrincipalsPageResponse {
  return { data, total: data.length, page: 1, limit: 20 };
}

function mockPrincipalsList(data: PrincipalUsageRow[]) {
  server.use(http.get('/api/analytics/principals', () => HttpResponse.json(pageResponse(data))));
}

describe('AnalyticsPrincipalsTable — row click navigation', () => {
  it('navigates to the drill-down page for the clicked row, with no query params', async () => {
    const push = vi.fn();
    vi.mocked(useRouter).mockReturnValue({ push } as unknown as ReturnType<typeof useRouter>);

    mockPrincipalsList([
      principalRow({
        principal_id: 'dev-user-1',
        display_name: 'Dev User',
        email: 'dev@orbit.local.clearroute.io',
      }),
    ]);

    renderTable();

    const nameCell = await screen.findByText('Dev User');

    const user = userEvent.setup();
    await user.click(nameCell);

    expect(push).toHaveBeenCalledTimes(1);
    // Only principal_id is carried — no display_name/email query params (see
    // page.tsx, which now resolves identity server-side instead).
    expect(push).toHaveBeenCalledWith('/model-gateway/analytics/principals/dev-user-1');
  });

  it('navigates using principal_id when display_name/email are absent', async () => {
    const push = vi.fn();
    vi.mocked(useRouter).mockReturnValue({ push } as unknown as ReturnType<typeof useRouter>);

    mockPrincipalsList([principalRow({ principal_id: 'service-account-7' })]);

    renderTable();

    const cell = await screen.findByText('service-account-7');

    const user = userEvent.setup();
    await user.click(cell);

    expect(push).toHaveBeenCalledWith('/model-gateway/analytics/principals/service-account-7');
  });

  it('clicking a different cell in the same row still navigates (whole row is the click target)', async () => {
    const push = vi.fn();
    vi.mocked(useRouter).mockReturnValue({ push } as unknown as ReturnType<typeof useRouter>);

    mockPrincipalsList([
      principalRow({ principal_id: 'dev-user-2', display_name: 'Another Dev', request_count: 42 }),
    ]);

    renderTable();

    // Click the requests-count cell, not the user cell — the whole <tr> is
    // the click target, not just the name.
    const requestsCell = await screen.findByText('42');

    const user = userEvent.setup();
    await user.click(requestsCell);

    expect(push).toHaveBeenCalledWith('/model-gateway/analytics/principals/dev-user-2');
  });

  it('does not render a nested link in the user cell', async () => {
    vi.mocked(useRouter).mockReturnValue({
      push: vi.fn(),
    } as unknown as ReturnType<typeof useRouter>);

    mockPrincipalsList([
      principalRow({ principal_id: 'dev-user-1', display_name: 'Dev User', email: 'dev@x.com' }),
    ]);

    renderTable();

    await screen.findByText('Dev User');

    // Previously the user cell wrapped its content in its own <Link>; now
    // the whole row is the interactive element and the cell is plain text.
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('exposes a screen-reader hint that the row is clickable', async () => {
    vi.mocked(useRouter).mockReturnValue({
      push: vi.fn(),
    } as unknown as ReturnType<typeof useRouter>);

    mockPrincipalsList([principalRow({ principal_id: 'dev-user-1', display_name: 'Dev User' })]);

    renderTable();

    await screen.findByText('Dev User');

    await waitFor(() => expect(screen.getByText('View full usage')).toBeInTheDocument());
  });
});

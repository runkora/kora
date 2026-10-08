/**
 * Integration tests for the Deliveries paging state machine in TriggersPage.
 *
 * The paging logic is a four-field state machine (rows, cursor, applied filter,
 * epoch) driven by overlapping async responses, and the two defects found here
 * by review were both ordering defects: a superseded response writing, and an
 * in-flight response appending onto a fresh server page. Neither is reachable
 * without controlling WHEN each response lands, so every request is intercepted
 * by MSW and parked until the test resolves it, in whatever order it likes.
 *
 * MSW rather than a module mock on `listDeliveries`: the query string the page
 * asks for (status, limit, cursor) is half of the behavior under test, and the
 * envelope unwrapping in `_services/triggers-api.ts` — `{ data, next_cursor }`,
 * and `message` on a failure — is the other half. A mocked service would assert
 * what the test author believed about both.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { renderWithProviders, server } from '@orbit/test-utils';
import type { TriggerDelivery } from '@orbit/shared/api';
import { TriggersPage } from './triggers-page';
import { toDeliveryRow } from '../_types';
import type { DeliveryRow } from '../_types';

// The page calls router.refresh() from its mutation hooks; nothing under test
// here navigates, so an inert router is enough.
vi.mock('next/navigation', () => ({
  useRouter: () => ({ refresh: vi.fn(), push: vi.fn(), replace: vi.fn() }),
}));

// ── Deferred request harness ──────────────────────────────────────────────────

/**
 * One intercepted GET /api/trigger-deliveries, held open.
 *
 * `respond` and `fail` are the two ways the server can answer. Holding the
 * response lets a test have two requests in flight at once and answer the newer
 * one first, which is the only way to reach the superseded-write path.
 */
interface PendingDeliveryRequest {
  /** Repeated `status` params, in order. Empty for the "All statuses" query. */
  statuses: string[];
  cursor: string | null;
  limit: string | null;
  respond: (deliveries: TriggerDelivery[], nextCursor: string | null) => void;
  fail: (message: string, status?: number) => void;
}

let pending: PendingDeliveryRequest[] = [];

beforeEach(() => {
  pending = [];
  server.use(
    http.get('/api/trigger-deliveries', ({ request }) => {
      const url = new URL(request.url);
      return new Promise<Response>((send) => {
        pending.push({
          statuses: url.searchParams.getAll('status'),
          cursor: url.searchParams.get('cursor'),
          limit: url.searchParams.get('limit'),
          respond: (deliveries, nextCursor) =>
            send(HttpResponse.json({ data: deliveries, next_cursor: nextCursor })),
          fail: (message, status = 500) => send(HttpResponse.json({ message }, { status })),
        });
      });
    }),
  );
});

/** The n-th request the page has issued, once it has actually been issued. */
async function request(index: number): Promise<PendingDeliveryRequest> {
  await waitFor(() => {
    expect(pending.length).toBeGreaterThan(index);
  });
  return pending[index] as PendingDeliveryRequest;
}

/**
 * Drain every queued response through fetch, MSW and React.
 *
 * Needed for the two discard assertions: a correctly discarded response leaves
 * no trace, so there is nothing to `waitFor`. Ten macrotask turns is well past
 * what the path takes — the mutation runs in the report confirm a bad write
 * does become visible inside this window.
 */
async function settle(): Promise<void> {
  await act(async () => {
    for (let turn = 0; turn < 10; turn += 1) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
  });
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

function wireDelivery(id: string, overrides: Partial<TriggerDelivery> = {}): TriggerDelivery {
  return {
    id,
    tenant_id: 'tenant-1',
    kind: 'webhook',
    source_id: 'src-1',
    trigger_id: 'trg-1',
    trigger_name: `trigger for ${id}`,
    event_type: 'push',
    status: 'dispatched',
    received_at: '2026-10-01T00:00:00Z',
    runs: [],
    ...overrides,
  };
}

function serverRow(id: string, overrides: Partial<TriggerDelivery> = {}): DeliveryRow {
  return toDeliveryRow(wireDelivery(id, overrides));
}

function pageElement(deliveries: DeliveryRow[], initialDeliveryCursor: string | null) {
  return (
    <TriggersPage
      sources={[]}
      triggers={[]}
      deliveries={deliveries}
      initialDeliveryCursor={initialDeliveryCursor}
      agents={[]}
      error={null}
    />
  );
}

function renderPage(deliveries: DeliveryRow[], initialDeliveryCursor: string | null) {
  return renderWithProviders(pageElement(deliveries, initialDeliveryCursor));
}

// ── Queries ───────────────────────────────────────────────────────────────────

const ROW_LABEL_PREFIX = 'View payload and outcome for delivery ';

/** The delivery ids on screen, in render order. Ids are short, so untruncated. */
function visibleRowIds(): string[] {
  return screen
    .queryAllByRole('row')
    .map((row) => row.getAttribute('aria-label'))
    .filter((label): label is string => label !== null && label.startsWith(ROW_LABEL_PREFIX))
    .map((label) => label.slice(ROW_LABEL_PREFIX.length));
}

function statusFilter(): HTMLElement {
  return screen.getByRole('combobox', { name: 'Filter deliveries by status' });
}

function loadMoreButton(): HTMLElement | null {
  return screen.queryByRole('button', { name: /Load more|Loading/ });
}

/** Deliveries is the second tab; its panel is unmounted until it is selected. */
async function openDeliveriesTab(): Promise<void> {
  await userEvent.click(screen.getByRole('tab', { name: 'Deliveries' }));
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('TriggersPage deliveries paging', () => {
  it('appends each Load more page onto the rows already on screen', async () => {
    renderPage([serverRow('d1')], 'cursor-1');
    await openDeliveriesTab();
    expect(visibleRowIds()).toEqual(['d1']);

    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    const first = await request(0);
    // Continues the server's page rather than re-reading page one, and keeps the
    // page size the server render used.
    expect(first.cursor).toBe('cursor-1');
    expect(first.limit).toBe('100');
    expect(first.statuses).toEqual([]);

    first.respond([wireDelivery('d2'), wireDelivery('d3')], 'cursor-2');
    await waitFor(() => {
      expect(visibleRowIds()).toEqual(['d1', 'd2', 'd3']);
    });

    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    const second = await request(1);
    expect(second.cursor).toBe('cursor-2');

    second.respond([wireDelivery('d4')], null);
    await waitFor(() => {
      expect(visibleRowIds()).toEqual(['d1', 'd2', 'd3', 'd4']);
    });
    // A spent cursor is how "that is all of them" is said.
    expect(loadMoreButton()).not.toBeInTheDocument();
  });

  it('starts a new query from the beginning when the status filter changes', async () => {
    renderPage([serverRow('d1'), serverRow('d2')], 'cursor-1');
    await openDeliveriesTab();

    await userEvent.selectOptions(statusFilter(), 'failed');
    const query = await request(0);
    expect(query.statuses).toEqual(['failed']);
    // A new filter is a new query, not a narrowing: it must not continue the
    // previous keyset.
    expect(query.cursor).toBeNull();
    expect(query.limit).toBe('100');

    query.respond([wireDelivery('d7', { status: 'failed' })], null);
    await waitFor(() => {
      // Replaced, not appended.
      expect(visibleRowIds()).toEqual(['d7']);
    });
    // The previous filter's cursor is gone with its rows.
    expect(loadMoreButton()).not.toBeInTheDocument();
  });

  it('keeps the cursor of the filter whose rows are showing, not the superseded one', async () => {
    renderPage([serverRow('d1')], 'cursor-1');
    await openDeliveriesTab();

    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    const superseded = await request(0);

    await userEvent.selectOptions(statusFilter(), 'failed');
    const current = await request(1);
    expect(current.statuses).toEqual(['failed']);

    // Out of order on purpose: the newer query answers first, so the older
    // response arrives into a page it no longer describes.
    current.respond([wireDelivery('d7', { status: 'failed' })], null);
    await waitFor(() => {
      expect(visibleRowIds()).toEqual(['d7']);
    });

    superseded.respond([wireDelivery('d2')], 'cursor-superseded');
    await settle();

    // Neither its rows nor its cursor may land: appending would relabel the old
    // filter's rows as the new filter's answer, and installing its cursor would
    // make every further page walk the wrong keyset.
    expect(visibleRowIds()).toEqual(['d7']);
    expect(loadMoreButton()).not.toBeInTheDocument();
  });

  it('discards an in-flight Load more when a fresh server page arrives as a prop', async () => {
    const { rerender } = renderPage([serverRow('d1')], 'cursor-1');
    await openDeliveriesTab();

    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    const inFlight = await request(0);

    // router.refresh() landed: a new deliveries array, new identity, which the
    // render-phase re-seed adopts and whose epoch retires the request above.
    rerender(pageElement([serverRow('d9')], null));
    await waitFor(() => {
      expect(visibleRowIds()).toEqual(['d9']);
    });

    inFlight.respond([wireDelivery('d2')], 'cursor-stale');
    await settle();

    // The old filter's rows appended onto the fresh page, plus the old keyset's
    // cursor installed, is what made the deliveries in between unreachable.
    expect(visibleRowIds()).toEqual(['d9']);
    expect(loadMoreButton()).not.toBeInTheDocument();
  });

  it('surfaces a failed Load more and keeps the cursor so it cannot read as the end', async () => {
    renderPage([serverRow('d1')], 'cursor-1');
    await openDeliveriesTab();

    await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
    (await request(0)).fail('deliveries backend is unavailable');

    await waitFor(() => {
      expect(screen.getByText('deliveries backend is unavailable')).toBeInTheDocument();
    });
    // The rows already loaded are still the truth about what was loaded.
    expect(visibleRowIds()).toEqual(['d1']);
    // Load more survives: a failure is not "that is all of them".
    expect(screen.getByRole('button', { name: 'Load more' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled();
  });

  it('clears the rows on a failed filter change and offers a Retry that re-issues it', async () => {
    renderPage([serverRow('d1')], 'cursor-1');
    await openDeliveriesTab();

    await userEvent.selectOptions(statusFilter(), 'failed');
    (await request(0)).fail('deliveries backend is unavailable');

    await waitFor(() => {
      expect(screen.getByText('deliveries backend is unavailable')).toBeInTheDocument();
    });
    // Stale rows under the new label would claim to be the answer.
    expect(visibleRowIds()).toEqual([]);
    expect(screen.getByText('No failed deliveries.')).toBeInTheDocument();
    // Nothing left to page, so Retry is the only way back: re-picking the same
    // option in the select fires no change event.
    expect(loadMoreButton()).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    const retried = await request(1);
    expect(retried.statuses).toEqual(['failed']);
    expect(retried.cursor).toBeNull();

    retried.respond([wireDelivery('d7', { status: 'failed' })], 'cursor-failed-2');
    await waitFor(() => {
      expect(visibleRowIds()).toEqual(['d7']);
    });
    expect(screen.queryByText('deliveries backend is unavailable')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Load more' })).toBeEnabled();
  });
});

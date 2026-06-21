/**
 * Unit tests for triggers-page.tsx — deliveries tab status filter and payload excerpt
 *
 * Covers:
 * - Client-side status filter behavior (single/multiple selection, clear, empty results)
 * - Payload excerpt rendering priority chain (run count > error > run ID > excerpt > "—")
 * - Payload excerpt truncation (> 120 chars)
 * - Filter options configuration matches backend status values
 */
import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { userEvent } from '@testing-library/user-event';
import { TriggersPage } from './triggers-page';
import type { DeliveryRow } from '../_types';

// ── Test fixtures ─────────────────────────────────────────────────────────────

const baseDelivery: DeliveryRow = {
  id: 'del-1',
  sourceId: 'src-1',
  eventType: 'push',
  status: 'dispatched',
  triggerId: 'trig-1',
  runId: null,
  error: null,
  payloadExcerpt: null,
  receivedAt: '2026-06-20T10:00:00Z',
  runs: [],
};

function makeDelivery(overrides: Partial<DeliveryRow>): DeliveryRow {
  return { ...baseDelivery, ...overrides };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('TriggersPage — Deliveries tab', () => {
  describe('Status filter', () => {
    it('filters to a single status (failed only)', async () => {
      const user = userEvent.setup();
      const deliveries = [
        makeDelivery({ id: 'del-1', status: 'dispatched' }),
        makeDelivery({ id: 'del-2', status: 'failed' }),
        makeDelivery({ id: 'del-3', status: 'dispatched' }),
        makeDelivery({ id: 'del-4', status: 'failed' }),
      ];

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={deliveries}
          agents={[]}
          error={null}
        />,
      );

      // Click the Deliveries tab to show the deliveries table
      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      // Initially all 4 deliveries should be visible
      const table = screen.getByRole('table');
      expect(within(table).getAllByRole('row')).toHaveLength(5); // header + 4 data rows

      // Open the status filter (click the Status column header filter button)
      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);

      // Select "Failed" from the filter popover
      const failedOption = screen.getByRole('checkbox', { name: /failed/i });
      await user.click(failedOption);

      // Now only the 2 failed deliveries should be visible
      expect(within(table).getAllByRole('row')).toHaveLength(3); // header + 2 data rows
      expect(screen.getByText('del-2')).toBeInTheDocument();
      expect(screen.getByText('del-4')).toBeInTheDocument();
      expect(screen.queryByText('del-1')).not.toBeInTheDocument();
      expect(screen.queryByText('del-3')).not.toBeInTheDocument();
    });

    it('filters to multiple statuses (failed + skipped)', async () => {
      const user = userEvent.setup();
      const deliveries = [
        makeDelivery({ id: 'del-1', status: 'dispatched' }),
        makeDelivery({ id: 'del-2', status: 'failed' }),
        makeDelivery({ id: 'del-3', status: 'skipped' }),
        makeDelivery({ id: 'del-4', status: 'verified' }),
      ];

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={deliveries}
          agents={[]}
          error={null}
        />,
      );

      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);

      // Select both "Failed" and "Skipped"
      await user.click(screen.getByRole('checkbox', { name: /failed/i }));
      await user.click(screen.getByRole('checkbox', { name: /skipped/i }));

      // Should show del-2 (failed) and del-3 (skipped)
      const table = screen.getByRole('table');
      expect(within(table).getAllByRole('row')).toHaveLength(3); // header + 2 data rows
      expect(screen.getByText('del-2')).toBeInTheDocument();
      expect(screen.getByText('del-3')).toBeInTheDocument();
      expect(screen.queryByText('del-1')).not.toBeInTheDocument();
      expect(screen.queryByText('del-4')).not.toBeInTheDocument();
    });

    it('clears filter and shows all deliveries', async () => {
      const user = userEvent.setup();
      const deliveries = [
        makeDelivery({ id: 'del-1', status: 'dispatched' }),
        makeDelivery({ id: 'del-2', status: 'failed' }),
      ];

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={deliveries}
          agents={[]}
          error={null}
        />,
      );

      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      // Apply filter
      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);
      await user.click(screen.getByRole('checkbox', { name: /failed/i }));

      // Only failed should be visible
      const table = screen.getByRole('table');
      expect(within(table).getAllByRole('row')).toHaveLength(2); // header + 1 data row

      // Clear the filter
      const clearButton = screen.getByRole('button', { name: /clear filter/i });
      await user.click(clearButton);

      // All deliveries should be visible again
      expect(within(table).getAllByRole('row')).toHaveLength(3); // header + 2 data rows
      expect(screen.getByText('del-1')).toBeInTheDocument();
      expect(screen.getByText('del-2')).toBeInTheDocument();
    });

    it('shows filteredEmptyMessage when filter matches zero rows', async () => {
      const user = userEvent.setup();
      const deliveries = [
        makeDelivery({ id: 'del-1', status: 'dispatched' }),
        makeDelivery({ id: 'del-2', status: 'dispatched' }),
      ];

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={deliveries}
          agents={[]}
          error={null}
        />,
      );

      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      // Filter to "failed" when no failed deliveries exist
      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);
      await user.click(screen.getByRole('checkbox', { name: /failed/i }));

      // Should show the filteredEmptyMessage
      expect(screen.getByText(/no deliveries match your search or filter/i)).toBeInTheDocument();
    });

    it('filter works correctly with empty dataset', async () => {
      const user = userEvent.setup();

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[]}
          agents={[]}
          error={null}
        />,
      );

      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      // Should show the regular empty message
      expect(screen.getByText(/no deliveries\./i)).toBeInTheDocument();

      // Filter button should still be accessible
      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);

      // Should be able to select a filter option (even with no data)
      const failedOption = screen.getByRole('checkbox', { name: /failed/i });
      expect(failedOption).toBeInTheDocument();
    });
  });

  describe('Filter options configuration', () => {
    it('includes all four backend status values', async () => {
      const user = userEvent.setup();

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[makeDelivery({})]}
          agents={[]}
          error={null}
        />,
      );

      await user.click(screen.getByRole('tab', { name: /deliveries/i }));

      const statusHeader = screen.getByRole('columnheader', { name: /status/i });
      const filterButton = within(statusHeader).getByRole('button');
      await user.click(filterButton);

      // All four status options should be present
      expect(screen.getByRole('checkbox', { name: /dispatched/i })).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: /verified/i })).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: /skipped/i })).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: /failed/i })).toBeInTheDocument();
    });
  });

  describe('Payload excerpt rendering — priority chain', () => {
    it('shows run count when runs array has multiple entries (highest priority)', () => {
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [
          { triggerId: 'trig-1', runId: 'run-1', status: 'running', error: null },
          { triggerId: 'trig-2', runId: 'run-2', status: 'running', error: null },
        ],
        error: 'Some error',
        runId: 'run-123',
        payloadExcerpt: '{"key": "value"}',
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Run count should be displayed (2 runs)
      expect(screen.getByText(/2 runs/i)).toBeInTheDocument();

      // Error, runId, and payload excerpt should NOT be visible
      expect(screen.queryByText(/some error/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/run-123/)).not.toBeInTheDocument();
      expect(screen.queryByText(/"key"/)).not.toBeInTheDocument();
    });

    it('shows error when present and no multi-run count (second priority)', () => {
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: 'Webhook signature verification failed',
        runId: 'run-123',
        payloadExcerpt: '{"key": "value"}',
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Error should be displayed
      expect(screen.getByText(/webhook signature verification failed/i)).toBeInTheDocument();

      // RunId and payload excerpt should NOT be visible
      expect(screen.queryByText(/run-123/)).not.toBeInTheDocument();
      expect(screen.queryByText(/"key"/)).not.toBeInTheDocument();
    });

    it('shows runId when present and no error or multi-run count (third priority)', () => {
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: null,
        runId: 'run-abc123',
        payloadExcerpt: '{"key": "value"}',
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // RunId should be displayed (truncated to first 8 chars)
      expect(screen.getByText(/run run-abc12/i)).toBeInTheDocument();

      // Payload excerpt should NOT be visible
      expect(screen.queryByText(/"key"/)).not.toBeInTheDocument();
    });

    it('shows payload excerpt when no error, runId, or multi-run count (fourth priority)', () => {
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: null,
        runId: null,
        payloadExcerpt: '{"repository": "orbit", "action": "opened"}',
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Payload excerpt should be displayed
      expect(screen.getByText(/"repository": "orbit"/i)).toBeInTheDocument();
    });

    it('truncates payload excerpt when longer than 120 characters', () => {
      const longPayload = 'a'.repeat(150); // 150 characters
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: null,
        runId: null,
        payloadExcerpt: longPayload,
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Should show first 120 chars + ellipsis
      const expectedText = 'a'.repeat(120) + '…';
      expect(screen.getByText(expectedText)).toBeInTheDocument();
    });

    it('does not truncate payload excerpt when exactly 120 characters', () => {
      const exactPayload = 'b'.repeat(120); // Exactly 120 characters
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: null,
        runId: null,
        payloadExcerpt: exactPayload,
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Should show all 120 chars with no ellipsis
      expect(screen.getByText(exactPayload)).toBeInTheDocument();
      expect(screen.queryByText(/…/)).not.toBeInTheDocument();
    });

    it('shows "—" when nothing is available (lowest priority fallback)', () => {
      const delivery = makeDelivery({
        id: 'del-1',
        runs: [],
        error: null,
        runId: null,
        payloadExcerpt: null,
      });

      render(
        <TriggersPage
          sources={[]}
          triggers={[]}
          deliveries={[delivery]}
          agents={[]}
          error={null}
        />,
      );

      // Should show the "—" placeholder
      const detailCells = screen.getAllByRole('cell');
      const detailCell = detailCells.find((cell) => cell.textContent === '—');
      expect(detailCell).toBeInTheDocument();
    });
  });
});

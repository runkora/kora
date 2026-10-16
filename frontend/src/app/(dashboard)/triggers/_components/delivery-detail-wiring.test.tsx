/**
 * The run link is only useful if every surface that shows a delivery actually
 * passes the resolver down. These tests cover that wiring: the expanded row in
 * DeliveryList and the standalone DeliveryDetailPanel both have to reach
 * DeliveryDetail with resolveAgentSlug intact, or the run id silently degrades
 * to plain text on that surface while working on the others.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { makeDelivery } from '../_types/fixtures';
import { DeliveryList } from './delivery-list';
import { DeliveryDetailPanel } from './delivery-detail-panel';

const dispatched = () =>
  makeDelivery({
    id: 'd-1',
    receivedAt: '2026-07-28T09:00:00Z',
    triggerId: 't1',
    runId: 'run-7',
    status: 'dispatched',
    runs: [
      {
        triggerId: 't1',
        triggerName: 'gh-push',
        runId: 'run-7',
        status: 'dispatched',
        error: null,
        createdAt: '2026-07-28T09:00:01Z',
      },
    ],
  });

const slugOf = (id: string) => (id === 't1' ? 'coder' : undefined);

describe('DeliveryList expanded row', () => {
  it('renders no detail at all while the row is closed', () => {
    render(
      <DeliveryList
        deliveries={[dispatched()]}
        triggerId="t1"
        expandedId={null}
        onToggleExpanded={vi.fn()}
        resolveAgentSlug={slugOf}
      />,
    );
    expect(screen.queryByRole('link', { name: 'run-7' })).toBeNull();
  });

  it('falls back to plain text when the caller has no resolver', () => {
    render(
      <DeliveryList
        deliveries={[dispatched()]}
        triggerId="t1"
        expandedId="d-1"
        onToggleExpanded={vi.fn()}
      />,
    );
    expect(screen.queryByRole('link', { name: 'run-7' })).toBeNull();
    expect(screen.getByText('run-7')).toBeInTheDocument();
  });
});

describe('DeliveryDetailPanel', () => {
  it('passes the resolver through so the run links', () => {
    render(
      <DeliveryDetailPanel delivery={dispatched()} onClose={vi.fn()} resolveAgentSlug={slugOf} />,
    );
    expect(screen.getByRole('link', { name: 'run-7' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-7',
    );
  });

  it('renders the run as text when no resolver is given', () => {
    render(<DeliveryDetailPanel delivery={dispatched()} onClose={vi.fn()} />);
    expect(screen.queryByRole('link', { name: 'run-7' })).toBeNull();
    expect(screen.getByText('run-7')).toBeInTheDocument();
  });
});

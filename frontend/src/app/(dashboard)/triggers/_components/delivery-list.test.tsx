import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { DeliveryList } from './delivery-list';
import { makeDelivery } from '../_types/fixtures';
import { EMPTY_TRIGGER_STATS } from '../_types/stats';

const run = (status: string) => ({
  triggerId: 'trg-1',
  triggerName: 'gh-push',
  runId: null,
  status,
  error: null,
  createdAt: null,
});

const DELIVERIES = [
  makeDelivery({ id: 'd-ok', status: 'dispatched', runs: [run('dispatched')] }),
  makeDelivery({ id: 'd-bad', status: 'failed', runs: [run('failed')] }),
  makeDelivery({ id: 'd-skip', status: 'skipped', runs: [run('skipped')] }),
  makeDelivery({ id: 'd-old', status: 'dispatched', runs: [run('superseded')] }),
  makeDelivery({ id: 'd-dup', status: 'duplicate' }),
  makeDelivery({ id: 'd-miss', status: 'missed' }),
];

const STATS = {
  ...EMPTY_TRIGGER_STATS,
  deliveries: 12,
  statusCounts: { started: 4, failed: 2, filtered: 5, superseded: 1 },
};

function renderList(props: Partial<React.ComponentProps<typeof DeliveryList>> = {}) {
  const onToggleExpanded = vi.fn();
  renderWithProviders(
    <DeliveryList
      deliveries={DELIVERIES}
      filter="all"
      onFilterChange={vi.fn()}
      stats={STATS}
      window="24h"
      triggerId="trg-1"
      expandedId={null}
      onToggleExpanded={onToggleExpanded}
      {...props}
    />,
  );
  return { onToggleExpanded };
}

describe('DeliveryList', () => {
  it('shows a placeholder while the first page loads, and no table', () => {
    renderList({ loading: true });
    expect(screen.getByRole('status', { name: 'Loading deliveries' })).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('offers a retry when the first fetch fails', () => {
    const onRetry = vi.fn();
    renderList({ deliveries: [], error: 'boom', onRetry });
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
  });

  it("shows the trigger's counts for the window, which add up to All, and asks for the chosen status", () => {
    const onFilterChange = vi.fn();
    renderList({ onFilterChange, window: '7d' });
    const group = screen.getByRole('group', { name: /Filter deliveries by outcome.*last 7d/ });
    const count = (name: RegExp) =>
      Number(within(group).getByRole('button', { name }).textContent?.replace(/\D/g, ''));
    expect(count(/All/)).toBe(12);
    expect(count(/Started/)).toBe(4);
    expect(count(/Filtered/)).toBe(5);
    expect(count(/Missed/)).toBe(0);
    const chips = [/Started/, /Failed/, /Filtered/, /Skipped/, /Superseded/, /Missed/, /Verified/];
    expect(chips.reduce((sum, name) => sum + count(name), 0)).toBe(count(/All/));
    expect(screen.getByText('Last 7d')).toBeInTheDocument();
    fireEvent.click(within(group).getByRole('button', { name: /Filtered/ }));
    expect(onFilterChange).toHaveBeenCalledWith('filtered');
  });

  it('shows the chips without counts for all history', () => {
    renderList({ window: 'all' });
    const group = screen.getByRole('group', { name: 'Filter deliveries by outcome' });
    expect(within(group).getByRole('button', { name: 'Started' })).toHaveTextContent(/^Started$/);
    expect(screen.queryByText(/^Last /)).toBeNull();
  });

  it('keeps the filter on screen when a filtered page is empty', () => {
    renderList({ deliveries: [], filter: 'missed' });
    expect(screen.getByRole('group', { name: /Filter deliveries by outcome/ })).toBeInTheDocument();
    expect(screen.getByText('No missed deliveries in the last 24h.')).toBeInTheDocument();
  });

  it('opens a row in place and reports it', () => {
    const { onToggleExpanded } = renderList();
    fireEvent.click(screen.getByRole('row', { name: 'Show payload for delivery d-bad' }));
    expect(onToggleExpanded).toHaveBeenCalledWith('d-bad');
  });

  it('closes the open row', () => {
    const { onToggleExpanded } = renderList({ expandedId: 'd-bad' });
    fireEvent.click(screen.getByRole('row', { name: 'Show payload for delivery d-bad' }));
    expect(onToggleExpanded).toHaveBeenCalledWith(null);
  });

  it('pages past the loaded rows from the server with the one pager, counting N+ until the end', () => {
    const onLoadOlder = vi.fn();
    renderList({ hasMore: true, onLoadOlder });
    expect(screen.getByText(/of 6\+/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });

  it('says why the next page failed under the rows already shown', () => {
    renderList({ hasMore: true, error: 'boom' });
    expect(screen.getByText('Could not load more deliveries: boom')).toBeInTheDocument();
  });
});

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { DeliveryList } from './delivery-list';
import { makeDelivery } from '../_types/fixtures';

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
];

function renderList(props: Partial<React.ComponentProps<typeof DeliveryList>> = {}) {
  const onToggleExpanded = vi.fn();
  renderWithProviders(
    <DeliveryList
      deliveries={DELIVERIES}
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

  it("counts each outcome from this trigger's own row, and filters to it", () => {
    renderList();
    const group = screen.getByRole('group', { name: 'Filter deliveries by outcome' });
    expect(within(group).getByRole('button', { name: /Dispatched/ })).toHaveTextContent('1');
    expect(within(group).getByRole('button', { name: /Failed/ })).toHaveTextContent('1');
    expect(within(group).getByRole('button', { name: /Superseded/ })).toHaveTextContent('1');
    fireEvent.click(within(group).getByRole('button', { name: /Superseded/ }));
    expect(screen.getByText('d-old')).toBeInTheDocument();
    expect(screen.queryByText('d-ok')).toBeNull();
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

  it('loads older deliveries on request, and says so while it does', () => {
    const onLoadOlder = vi.fn();
    const { unmount } = renderWithProviders(
      <DeliveryList
        deliveries={DELIVERIES}
        triggerId="trg-1"
        expandedId={null}
        onToggleExpanded={vi.fn()}
        hasMore
        onLoadOlder={onLoadOlder}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Load older' }));
    expect(onLoadOlder).toHaveBeenCalled();
    unmount();
    renderList({ hasMore: true, loadingMore: true });
    expect(screen.getByRole('button', { name: /Loading/ })).toBeDisabled();
  });

  it('says why a first page with nothing for this trigger is empty, when there is more', () => {
    renderList({ deliveries: [], hasMore: true });
    expect(screen.getByText(/Load older to keep looking/)).toBeInTheDocument();
  });
});

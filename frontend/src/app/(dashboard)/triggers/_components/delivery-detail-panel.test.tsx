import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { DeliveryDetailPanel, runHrefFor } from './delivery-detail-panel';
import { StaticPermissionsProvider } from '../../_components/permissions-provider';
import { makeDelivery } from '../_types/fixtures';

const failed = makeDelivery({
  id: 'd-1',
  triggerId: 't1',
  triggerName: 'gh-push',
  eventType: 'push',
  status: 'failed',
  error: 'agent type not found',
  receivedAt: '2026-07-28T09:00:00Z',
});

function renderPanel(props: Partial<React.ComponentProps<typeof DeliveryDetailPanel>> = {}) {
  return render(
    <StaticPermissionsProvider scopes={['triggers:write']}>
      <DeliveryDetailPanel delivery={failed} onClose={vi.fn()} {...props} />
    </StaticPermissionsProvider>,
  );
}

describe('DeliveryDetailPanel', () => {
  it('opens as a dialog titled by the event, with status and summary figures', () => {
    renderPanel();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('push')).toBeInTheDocument();
    expect(screen.getByText('Delivery d-1')).toBeInTheDocument();
    expect(screen.getByText('Failed')).toBeInTheDocument();
    expect(screen.getByText('gh-push')).toBeInTheDocument();
    expect(screen.getByText('2026-07-28 09:00:00 UTC')).toBeInTheDocument();
  });

  it('shows the failure the delivery was opened to explain', () => {
    renderPanel();
    expect(screen.getAllByText(/agent type not found/).length).toBeGreaterThan(0);
  });

  it('makes the trigger name the way to the trigger only when the caller can go there', () => {
    const onOpenTrigger = vi.fn();
    const { unmount } = renderPanel();
    expect(screen.queryByRole('button', { name: /gh-push/ })).not.toBeInTheDocument();
    expect(screen.getByText('gh-push')).toBeInTheDocument();
    unmount();

    renderPanel({ onOpenTrigger });
    expect(screen.queryByRole('button', { name: /open trigger/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /gh-push/ }));
    expect(onOpenTrigger).toHaveBeenCalledWith('t1');
  });

  it('links the run id to its run page when given a runHref', () => {
    const dispatched = makeDelivery({ id: 'd-2', triggerId: 't1', runId: 'run-9' });
    renderPanel({
      delivery: dispatched,
      runHref: (id) => `/agents/triager/runs/${id}`,
    });
    expect(screen.getByRole('link', { name: 'run-9' })).toHaveAttribute(
      'href',
      '/agents/triager/runs/run-9',
    );
  });
});

describe('DeliveryDetailPanel facts', () => {
  it('lists the coalescing key among the delivery facts, with the payload in its own block', () => {
    renderPanel({
      delivery: makeDelivery({
        id: 'd-3',
        triggerId: 't1',
        payloadExcerpt: '{"ref":"main"}',
        payloadBytes: 2048,
        coalesceKeys: [{ triggerId: 't1', key: 'acme/platform#1', keyTooLong: false }],
      }),
    });
    expect(screen.getByText('Coalescing key')).toBeInTheDocument();
    expect(screen.getByText('acme/platform#1')).toBeInTheDocument();
    expect(screen.getByText('Request payload')).toBeInTheDocument();
    expect(screen.getByText('2.0 KB')).toBeInTheDocument();
    // Each is said once.
    expect(screen.getAllByText('Request payload')).toHaveLength(1);
    expect(screen.getAllByText('Coalescing key')).toHaveLength(1);
  });

  it('says so when the payload has been purged', () => {
    renderPanel({ delivery: makeDelivery({ id: 'd-4', payloadPurged: true }) });
    expect(screen.getByText(/purged past this tenant/)).toBeInTheDocument();
  });
});

describe('DeliveryDetailPanel fan-out and long keys', () => {
  it('names the trigger link from the trigger it opens, not the first fan-out row', () => {
    const onOpenTrigger = vi.fn();
    const run = (triggerId: string, triggerName: string) => ({
      triggerId,
      triggerName,
      runId: null,
      status: 'dispatched',
      error: null,
      createdAt: null,
    });
    renderPanel({
      onOpenTrigger,
      delivery: makeDelivery({
        id: 'd-5',
        triggerId: 'gh-audit',
        triggerName: null,
        runs: [run('gh-push', 'gh-push'), run('gh-audit', 'gh-audit')],
      }),
    });
    const link = screen.getByRole('button', { name: /gh-audit/ });
    expect(screen.queryByRole('button', { name: /gh-push/ })).not.toBeInTheDocument();
    fireEvent.click(link);
    expect(onOpenTrigger).toHaveBeenCalledWith('gh-audit');
  });

  it('truncates a long coalescing key and keeps the whole key on hover', () => {
    const key = 'k'.repeat(256);
    renderPanel({
      delivery: makeDelivery({
        id: 'd-6',
        triggerId: 't1',
        coalesceKeys: [{ triggerId: 't1', key, keyTooLong: false }],
      }),
    });
    const value = screen.getByText(key);
    expect(value).toHaveAttribute('title', key);
    expect(value.className).toMatch(/truncate/);
  });
});

describe('DeliveryDetailPanel outcome and redeliver', () => {
  it('replays a failed delivery from the modal and says so', async () => {
    const onRedeliver = vi.fn().mockResolvedValue({ deliveryId: 'd-new', error: null });
    renderPanel({ onRedeliver });
    fireEvent.click(screen.getByRole('button', { name: 'Redeliver' }));
    expect(onRedeliver).toHaveBeenCalledWith(failed);
    expect(await screen.findByText('Replayed as a new delivery.')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Replayed' })).toBeDisabled());
  });

  it('offers no replay for a delivery that already dispatched, and no empty block', () => {
    renderPanel({
      onRedeliver: vi.fn(),
      delivery: makeDelivery({ id: 'd-7', triggerId: 't1', status: 'dispatched', runId: 'r1' }),
    });
    expect(screen.queryByRole('button', { name: 'Redeliver' })).not.toBeInTheDocument();
    expect(screen.queryByText('Request payload')).not.toBeInTheDocument();
  });
});

describe('runHrefFor', () => {
  it('names the run page from the owning agent, and only for a single-agent trigger', () => {
    const owner = { agentSlug: 'triager', routing: null } as Parameters<typeof runHrefFor>[0];
    expect(runHrefFor(owner)?.('r1')).toBe('/agents/triager/runs/r1');
    expect(runHrefFor(undefined)).toBeUndefined();
    expect(runHrefFor({ agentSlug: '', routing: null } as typeof owner)).toBeUndefined();
    expect(runHrefFor({ agentSlug: 'triager', routing: {} } as typeof owner)).toBeUndefined();
  });
});

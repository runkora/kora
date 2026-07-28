import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { DeliveryDetail } from './delivery-detail';
import type { DeliveryRow } from '../_types';

function delivery(overrides: Partial<DeliveryRow> = {}): DeliveryRow {
  return {
    id: 'd-1',
    kind: 'webhook',
    sourceId: 'src-1',
    eventType: 'push',
    status: 'dispatched',
    triggerId: 't1',
    runId: null,
    error: null,
    payloadExcerpt: '{"ref":"refs/heads/main"}',
    firedBy: null,
    scheduledAt: null,
    reason: null,
    payloadBytes: 512,
    payloadPurged: false,
    receivedAt: '2026-07-28T09:00:00Z',
    runs: [
      {
        triggerId: 't1',
        runId: 'run-9',
        status: 'dispatched',
        error: null,
        createdAt: '2026-07-28T09:00:01Z',
      },
    ],
    ...overrides,
  };
}

describe('scoping', () => {
  it('reads the outcome of the named trigger out of the fan-out', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          runs: [
            {
              triggerId: 't1',
              runId: 'run-mine',
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
            {
              triggerId: 't2',
              runId: 'run-theirs',
              status: 'failed',
              error: 'boom',
              createdAt: null,
            },
          ],
        })}
        triggerId="t1"
      />,
    );
    expect(screen.getByText('run-mine')).toBeTruthy();
    expect(screen.queryByText('boom')).toBeNull();
  });

  it('falls back to the delivery-level verdict when no trigger is named', () => {
    render(<DeliveryDetail delivery={delivery({ runId: 'run-top', error: 'top level' })} />);
    expect(screen.getByText('run-top')).toBeTruthy();
    expect(screen.getByText('top level')).toBeTruthy();
  });
});

describe('payload', () => {
  it('pretty-prints a JSON excerpt', () => {
    render(<DeliveryDetail delivery={delivery()} triggerId="t1" />);
    expect(screen.getByText(/refs\/heads\/main/)).toBeTruthy();
  });

  it('says so when the payload was purged, rather than showing nothing', () => {
    render(
      <DeliveryDetail
        delivery={delivery({ payloadPurged: true, payloadExcerpt: null })}
        triggerId="t1"
      />,
    );
    expect(screen.getByText(/purged past this tenant/)).toBeTruthy();
  });
});

describe('redeliver', () => {
  it('is not offered to a caller that cannot replay', () => {
    render(<DeliveryDetail delivery={delivery()} triggerId="t1" />);
    expect(screen.queryByRole('button', { name: /Redeliver/ })).toBeNull();
  });

  it('names the new delivery on success, and will not fire twice', async () => {
    const onRedeliver = vi.fn().mockResolvedValue({ deliveryId: 'd-2', error: null });
    render(<DeliveryDetail delivery={delivery()} triggerId="t1" onRedeliver={onRedeliver} />);

    fireEvent.click(screen.getByRole('button', { name: 'Redeliver' }));

    expect(await screen.findByText('d-2')).toBeTruthy();
    expect(screen.getByText('Replayed as a new delivery.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Replayed' }).hasAttribute('disabled')).toBe(true);
  });

  it('surfaces the failure and leaves the button usable', async () => {
    const onRedeliver = vi
      .fn()
      .mockResolvedValue({ deliveryId: null, error: 'delivery is not redeliverable' });
    render(<DeliveryDetail delivery={delivery()} triggerId="t1" onRedeliver={onRedeliver} />);

    fireEvent.click(screen.getByRole('button', { name: 'Redeliver' }));

    expect(await screen.findByText('delivery is not redeliverable')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Redeliver' }).hasAttribute('disabled')).toBe(false);
  });

  it('is offered even when nothing ran, which is when a replay matters most', () => {
    render(
      <DeliveryDetail
        delivery={delivery({ runs: [], runId: null })}
        triggerId="t1"
        onRedeliver={vi.fn()}
      />,
    );
    expect(screen.getByText('No run started for this delivery.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Redeliver' })).toBeTruthy();
  });
});

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { DeliveryDetail } from './delivery-detail';
import type { DeliveryRow } from '../_types';
import { makeDelivery } from '../_types/fixtures';

/** The detail tests want a delivery that carried a payload and started a run. */
function delivery(overrides: Partial<DeliveryRow> = {}): DeliveryRow {
  return makeDelivery({
    id: 'd-1',
    triggerId: 't1',
    payloadExcerpt: '{"ref":"refs/heads/main"}',
    payloadBytes: 512,
    receivedAt: '2026-07-28T09:00:00Z',
    runs: [
      {
        triggerId: 't1',
        triggerName: 'gh-push',
        runId: 'run-9',
        status: 'dispatched',
        error: null,
        createdAt: '2026-07-28T09:00:01Z',
      },
    ],
    ...overrides,
  });
}

/** A delivery whose outcome a replay could actually recover. */
function failedDelivery(): DeliveryRow {
  return delivery({
    status: 'failed',
    runs: [
      {
        triggerId: 't1',
        triggerName: 'gh-push',
        runId: null,
        status: 'failed',
        error: 'agent type not found',
        createdAt: null,
      },
    ],
  });
}

describe('scoping', () => {
  it('reads the outcome of the named trigger out of the fan-out', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          runs: [
            {
              triggerId: 't1',
              triggerName: null,
              runId: 'run-mine',
              status: 'dispatched',
              error: null,
              createdAt: null,
            },
            {
              triggerId: 't2',
              triggerName: 'theirs',
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
    render(<DeliveryDetail delivery={failedDelivery()} triggerId="t1" onRedeliver={onRedeliver} />);

    fireEvent.click(screen.getByRole('button', { name: 'Redeliver' }));

    expect(await screen.findByText('d-2')).toBeTruthy();
    expect(screen.getByText('Replayed as a new delivery.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Replayed' }).hasAttribute('disabled')).toBe(true);
  });

  it('surfaces the failure and leaves the button usable', async () => {
    const onRedeliver = vi
      .fn()
      .mockResolvedValue({ deliveryId: null, error: 'delivery is not redeliverable' });
    render(<DeliveryDetail delivery={failedDelivery()} triggerId="t1" onRedeliver={onRedeliver} />);

    fireEvent.click(screen.getByRole('button', { name: 'Redeliver' }));

    expect(await screen.findByText('delivery is not redeliverable')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Redeliver' }).hasAttribute('disabled')).toBe(false);
  });

  it('is offered even when nothing ran, which is when a replay matters most', () => {
    render(
      <DeliveryDetail
        delivery={delivery({ status: 'failed', runs: [], runId: null })}
        triggerId="t1"
        onRedeliver={vi.fn()}
      />,
    );
    expect(screen.getByText('No run started for this delivery.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Redeliver' })).toBeTruthy();
  });
});

describe('redeliver is offered only where it recovers something', () => {
  function renderFor(status: string) {
    return render(
      <DeliveryDetail
        delivery={delivery({
          status,
          runs: [
            {
              triggerId: 't1',
              triggerName: 'gh-push',
              runId: null,
              status,
              error: null,
              createdAt: null,
            },
          ],
        })}
        triggerId="t1"
        onRedeliver={vi.fn()}
      />,
    );
  }

  it.each(['failed', 'denied', 'dropped'])('offers replay of a %s outcome', (status) => {
    const view = renderFor(status);
    expect(screen.getByRole('button', { name: 'Redeliver' })).toBeTruthy();
    view.unmount();
  });

  it.each(['dispatched', 'verified', 'skipped', 'duplicate', 'missed'])(
    'hides replay of a %s outcome',
    (status) => {
      const view = renderFor(status);
      expect(screen.queryByRole('button', { name: 'Redeliver' })).toBeNull();
      view.unmount();
    },
  );

  it('reads the delivery status when no trigger is named', () => {
    render(
      <DeliveryDetail delivery={delivery({ status: 'failed', runs: [] })} onRedeliver={vi.fn()} />,
    );
    expect(screen.getByRole('button', { name: 'Redeliver' })).toBeTruthy();
  });

  it('judges each trigger in a fan-out on its own outcome', () => {
    const fanned = delivery({
      status: 'dispatched',
      runs: [
        {
          triggerId: 't1',
          triggerName: 'ok',
          runId: 'r1',
          status: 'dispatched',
          error: null,
          createdAt: null,
        },
        {
          triggerId: 't2',
          triggerName: 'broken',
          runId: null,
          status: 'failed',
          error: 'boom',
          createdAt: null,
        },
      ],
    });

    const ok = render(<DeliveryDetail delivery={fanned} triggerId="t1" onRedeliver={vi.fn()} />);
    expect(screen.queryByRole('button', { name: 'Redeliver' })).toBeNull();
    ok.unmount();

    render(<DeliveryDetail delivery={fanned} triggerId="t2" onRedeliver={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Redeliver' })).toBeTruthy();
  });
});

describe('why an arrival ended without dispatching', () => {
  it('shows the delivery-level reason when there is no error to show', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          status: 'duplicate',
          reason: 'already delivered under d-0',
          runs: [],
        })}
      />,
    );

    expect(screen.getByText('already delivered under d-0')).toBeTruthy();
  });

  it('prefers a real error over the reason, rather than showing both', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          status: 'failed',
          error: 'start run: boom',
          reason: 'a reason',
          runs: [],
        })}
      />,
    );

    expect(screen.getByText('start run: boom')).toBeTruthy();
    expect(screen.queryByText('a reason')).toBeNull();
  });

  // A fire refused before it ever fanned out carries its reason only on the
  // delivery. Scoping past that left the panel silent on exactly the rows
  // someone opens to ask why.
  it('falls back to the delivery error when this trigger has no fan-out row', () => {
    render(
      <DeliveryDetail
        delivery={delivery({ status: 'failed', error: 'task: required', runs: [] })}
        triggerId="t1"
      />,
    );

    expect(screen.getByText('task: required')).toBeTruthy();
  });
});

describe('coalescing', () => {
  it('shows the rendered key', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          coalesceKeys: [{ triggerId: 't1', key: 'acme/api#7', keyTooLong: false }],
        })}
      />,
    );
    expect(screen.getByText('acme/api#7')).toBeTruthy();
  });

  it('says so when the key was too long', () => {
    render(
      <DeliveryDetail
        delivery={delivery({ coalesceKeys: [{ triggerId: 't1', key: null, keyTooLong: true }] })}
      />,
    );
    expect(screen.getByText('Key too long, not coalesced')).toBeTruthy();
  });

  it('shows only the named trigger key when scoped', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          coalesceKeys: [
            { triggerId: 't1', key: 'mine', keyTooLong: false },
            { triggerId: 't2', key: 'theirs', keyTooLong: false },
          ],
        })}
        triggerId="t1"
      />,
    );
    expect(screen.getByText('mine')).toBeTruthy();
    expect(screen.queryByText('theirs')).toBeNull();
  });

  it('shows which delivery replaced a superseded run', () => {
    render(
      <DeliveryDetail
        delivery={delivery({
          status: 'superseded',
          runs: [
            {
              triggerId: 't1',
              triggerName: 'gh-push',
              runId: null,
              status: 'superseded',
              error: null,
              supersededBy: 'd-newer',
              createdAt: null,
            },
          ],
        })}
      />,
    );
    expect(screen.getByText('Replaced by delivery')).toBeTruthy();
    expect(screen.getByText('d-newer')).toBeTruthy();
  });

  it('shows nothing extra for an ordinary delivery', () => {
    render(<DeliveryDetail delivery={delivery()} />);
    expect(screen.queryByText('coalesce key')).toBeNull();
    expect(screen.queryByText('Replaced by delivery')).toBeNull();
  });
});

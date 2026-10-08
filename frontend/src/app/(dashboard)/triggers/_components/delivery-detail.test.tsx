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

  it('names the trigger on each key when several triggers coalesced', () => {
    const run = (triggerId: string, triggerName: string) => ({
      triggerId,
      triggerName,
      runId: null,
      status: 'dispatched',
      error: null,
      createdAt: null,
    });
    render(
      <DeliveryDetail
        delivery={delivery({
          runs: [run('t1', 'gh-push'), run('t2', 'gh-review')],
          coalesceKeys: [
            { triggerId: 't1', key: 'a', keyTooLong: false },
            { triggerId: 't2', key: 'b', keyTooLong: false },
          ],
        })}
      />,
    );
    expect(screen.getByText('Key (gh-push)')).toBeTruthy();
    expect(screen.getByText('Key (gh-review)')).toBeTruthy();
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
    expect(screen.getByText('Replaced by')).toBeTruthy();
    expect(screen.getByText('d-newer')).toBeTruthy();
  });

  it('shows nothing extra for an ordinary delivery', () => {
    render(<DeliveryDetail delivery={delivery()} />);
    expect(screen.queryByText('Coalescing')).toBeNull();
    expect(screen.queryByText('Replaced by')).toBeNull();
  });
});

describe('run link', () => {
  it('links the run id to that run on the agent that owns the trigger', () => {
    render(
      <DeliveryDetail
        delivery={delivery()}
        triggerId="t1"
        resolveAgentSlug={(id) => (id === 't1' ? 'coder' : undefined)}
      />,
    );
    // The fan-out names the trigger; the run route is keyed by agent slug, so
    // the resolver is what turns one into the other.
    const link = screen.getByRole('link', { name: 'run-9' });
    expect(link).toHaveAttribute('href', '/agents/coder/runs/run-9');
  });

  it('renders the run id as plain text when no slug can be resolved', () => {
    render(<DeliveryDetail delivery={delivery()} triggerId="t1" />);
    // A caller without the triggers list gets no link rather than a broken one.
    expect(screen.queryByRole('link', { name: 'run-9' })).toBeNull();
    expect(screen.getByText('run-9')).toBeInTheDocument();
  });

  it('still links when the surface is not scoped to one trigger', () => {
    // The unscoped panel reads the delivery's own run, and the trigger that
    // produced it is found in the fan-out.
    render(
      <DeliveryDetail
        delivery={delivery({ runId: 'run-9' })}
        resolveAgentSlug={(id) => (id === 't1' ? 'coder' : undefined)}
      />,
    );
    expect(screen.getByRole('link', { name: 'run-9' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-9',
    );
  });
});

describe('run link when the fan-out row is missing', () => {
  // outcomeRecorder.insert logs an InsertDeliveryRun failure and carries on, so
  // a delivery can hold a run_id and a trigger_id with no fan-out row behind
  // them. The owning trigger is still known, so the link is still buildable.
  it("falls back to the delivery's own trigger id", () => {
    const d = makeDelivery({
      id: 'd-orphan',
      receivedAt: '2026-07-28T09:00:00Z',
      triggerId: 't1',
      runId: 'run-5',
      status: 'dispatched',
      runs: [],
    });
    render(
      <DeliveryDetail
        delivery={d}
        resolveAgentSlug={(id) => (id === 't1' ? 'coder' : undefined)}
      />,
    );
    expect(screen.getByRole('link', { name: 'run-5' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-5',
    );
  });

  it('still renders text when even that trigger resolves to no slug', () => {
    const d = makeDelivery({
      id: 'd-orphan',
      receivedAt: '2026-07-28T09:00:00Z',
      triggerId: 't1',
      runId: 'run-5',
      status: 'dispatched',
      runs: [],
    });
    render(<DeliveryDetail delivery={d} resolveAgentSlug={() => undefined} />);
    expect(screen.queryByRole('link', { name: 'run-5' })).toBeNull();
    expect(screen.getByText('run-5')).toBeInTheDocument();
  });
});

describe('a delivery run missing from its own fan-out', () => {
  // outcomeRecorder.insert swallows an InsertDeliveryRun failure while
  // UpdateDeliveryOutcome still stamps the delivery, so delivery.runId can name
  // a run that has no fan-out row. With siblings present the list used to show
  // only them and suppress the inline cluster, hiding that run entirely.
  it('lists it alongside the runs that did write their rows', () => {
    const d = makeDelivery({
      id: 'd-mixed',
      receivedAt: '2026-07-28T09:00:00Z',
      triggerId: 't3',
      runId: 'run-orphan',
      status: 'dispatched',
      runs: [
        {
          triggerId: 't1',
          triggerName: 'a',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
        {
          triggerId: 't2',
          triggerName: 'b',
          runId: 'run-2',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:02Z',
        },
      ],
    });
    render(<DeliveryDetail delivery={d} resolveAgentSlug={() => 'coder'} />);

    expect(screen.getByRole('link', { name: 'run-1' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'run-2' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'run-orphan' })).toBeInTheDocument();
  });

  it('does not duplicate it when a fan-out row already carries it', () => {
    const d = makeDelivery({
      id: 'd-dup',
      receivedAt: '2026-07-28T09:00:00Z',
      triggerId: 't1',
      runId: 'run-1',
      status: 'dispatched',
      runs: [
        {
          triggerId: 't1',
          triggerName: 'a',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
        {
          triggerId: 't2',
          triggerName: 'b',
          runId: 'run-2',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:02Z',
        },
      ],
    });
    render(<DeliveryDetail delivery={d} resolveAgentSlug={() => 'coder'} />);
    expect(screen.getAllByRole('link', { name: 'run-1' })).toHaveLength(1);
  });
});

describe('fan-out runs', () => {
  it('links every run when one delivery started several', () => {
    // The issue asks for each run to be reachable, not just the first: one
    // arrival can match several triggers and start a run for each.
    const d = makeDelivery({
      id: 'd-2',
      receivedAt: '2026-07-28T09:00:00Z',
      runs: [
        {
          triggerId: 't1',
          triggerName: 'gh-push',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
        {
          triggerId: 't2',
          triggerName: 'gh-label',
          runId: 'run-2',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:02Z',
        },
      ],
    });
    const slugs: Record<string, string> = { t1: 'coder', t2: 'labeller' };
    render(<DeliveryDetail delivery={d} resolveAgentSlug={(id) => slugs[id]} />);

    expect(screen.getByRole('link', { name: 'run-1' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-1',
    );
    // The second run points at its own trigger's agent, not the first one's.
    expect(screen.getByRole('link', { name: 'run-2' })).toHaveAttribute(
      'href',
      '/agents/labeller/runs/run-2',
    );
  });

  it('names the trigger beside each run so they can be told apart', () => {
    const d = makeDelivery({
      id: 'd-3',
      runs: [
        {
          triggerId: 't1',
          triggerName: 'gh-push',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
        {
          triggerId: 't2',
          triggerName: 'gh-label',
          runId: 'run-2',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:02Z',
        },
      ],
    });
    render(<DeliveryDetail delivery={d} />);
    expect(screen.getByText('gh-push')).toBeInTheDocument();
    expect(screen.getByText('gh-label')).toBeInTheDocument();
  });
});

describe('run visibility agrees with the fan-out', () => {
  // service_dispatch writes the DeliveryRun before it stamps delivery.run_id,
  // so a read between the two sees a fan-out run and no delivery-level id.
  const midDispatch = () =>
    makeDelivery({
      id: 'd-mid',
      receivedAt: '2026-07-28T09:00:00Z',
      runId: null,
      runs: [
        {
          triggerId: 't1',
          triggerName: 'gh-push',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
      ],
    });

  it('shows the single fan-out run even when the delivery has no run id yet', () => {
    render(<DeliveryDetail delivery={midDispatch()} resolveAgentSlug={() => 'coder'} />);
    expect(screen.getByRole('link', { name: 'run-1' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-1',
    );
  });

  it('does not claim nothing ran while a run is reachable', () => {
    render(<DeliveryDetail delivery={midDispatch()} resolveAgentSlug={() => 'coder'} />);
    expect(screen.queryByText('No run started for this delivery.')).toBeNull();
  });

  it('does not claim nothing ran while listing several runs', () => {
    const d = makeDelivery({
      id: 'd-fan',
      receivedAt: '2026-07-28T09:00:00Z',
      runId: null,
      runs: [
        {
          triggerId: 't1',
          triggerName: 'gh-push',
          runId: 'run-1',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:01Z',
        },
        {
          triggerId: 't2',
          triggerName: 'gh-label',
          runId: 'run-2',
          status: 'dispatched',
          error: null,
          createdAt: '2026-07-28T09:00:02Z',
        },
      ],
    });
    render(<DeliveryDetail delivery={d} resolveAgentSlug={() => 'coder'} />);
    expect(screen.getByRole('link', { name: 'run-1' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'run-2' })).toBeInTheDocument();
    expect(screen.queryByText('No run started for this delivery.')).toBeNull();
  });

  it('still says so when nothing ran at all', () => {
    const d = makeDelivery({
      id: 'd-none',
      receivedAt: '2026-07-28T09:00:00Z',
      runId: null,
      runs: [],
    });
    render(<DeliveryDetail delivery={d} />);
    expect(screen.getByText('No run started for this delivery.')).toBeInTheDocument();
  });
});

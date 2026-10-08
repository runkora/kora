import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// The drawer reads the source's schema catalogue from the API.
const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
}));
vi.mock('../_services/triggers-api', () => api);

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchProviderEventTypes.mockResolvedValue({
    eventTypes: { declared: ['push', 'issue_comment'], associations: ['OWNER', 'MEMBER'] },
    error: null,
  });
  api.fetchEventTypes.mockResolvedValue({
    eventTypes: {
      declared: ['push', 'issue_comment'],
      observed: ['custom'],
      associations: ['OWNER', 'MEMBER'],
    },
    error: null,
  });
  api.fetchProviderFields.mockResolvedValue({
    fields: [{ path: 'ref', group: 'event', description: 'The pushed ref' }],
    error: null,
  });
});

import { TriggerDrawer } from './trigger-drawer';
import type { DeliveryRow, SourceRow, TriggerRow } from '../_types';
import { makeDelivery, makeTrigger } from '../_types/fixtures';

/** The drawer tests want a stored condition and a template worth displaying. */
function trigger(overrides: Partial<TriggerRow> = {}): TriggerRow {
  return makeTrigger({
    matchCondition: { action: ['opened'] },
    promptTemplate: 'Review {{ ref }}',
    ...overrides,
  });
}

const SOURCES: SourceRow[] = [
  {
    id: 'src-1',
    name: 'acme/widgets',
    provider: 'github',
    enabled: true,
    createdAt: '2026-07-01T00:00:00Z',
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
];

function renderDrawer(
  row: TriggerRow,
  onEdit?: () => Promise<string | null>,
  deliveries: DeliveryRow[] = [],
) {
  return render(
    <TriggerDrawer
      trigger={row}
      sources={SOURCES}
      deliveries={deliveries}
      ingestHost="orbit.local.clearroute.io:8443"
      onClose={vi.fn()}
      onToggleEnabled={vi.fn()}
      onDelete={vi.fn()}
      onEdit={onEdit ?? vi.fn().mockResolvedValue(null)}
    />,
  );
}

describe('prompt editing', () => {
  it('shows the prompt, which the panel previously never displayed', () => {
    renderDrawer(trigger());
    expect(screen.getByText('Prompt')).toBeTruthy();
  });

  it('offers no save affordance until something changes', () => {
    renderDrawer(trigger());
    expect(screen.queryByRole('button', { name: 'Save prompt' })).toBeNull();
  });
});

describe('event and filter editing', () => {
  it('seeds the rows from the stored match condition', () => {
    renderDrawer(trigger());
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('opened');
  });

  it('grows a save row once a filter changes, and drops it on discard', () => {
    renderDrawer(trigger());
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });

    expect(screen.getByRole('button', { name: 'Save filters' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(screen.queryByRole('button', { name: 'Save filters' })).toBeNull();
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('opened');
  });

  it('blocks the save on a half-written filter', () => {
    renderDrawer(trigger());
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: '' } });

    expect(screen.getByText('Finish or remove the empty filter')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save filters' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('sends the event and the condition together', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save filters' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      eventType: 'push',
      matchCondition: { action: ['closed'] },
      matchContains: {},
    });
  });

  it('saves a contains row to match_contains', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    fireEvent.change(screen.getByLabelText('Filter 1 operator'), { target: { value: 'contains' } });
    fireEvent.change(screen.getByLabelText('Filter 1 values'), {
      target: { value: '@kora-agent' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save filters' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      eventType: 'push',
      matchCondition: {},
      matchContains: { action: ['@kora-agent'] },
    });
  });

  it('keeps the edit and shows the reason when the save fails', async () => {
    const onEdit = vi.fn().mockResolvedValue('event_type is not valid for this source');
    renderDrawer(trigger(), onEdit);
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save filters' }));

    expect(await screen.findByText('event_type is not valid for this source')).toBeTruthy();
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('closed');
  });
});

describe('schema catalogue', () => {
  it("offers the source's schema and received event types, and the edited event's fields", async () => {
    renderDrawer(trigger());
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    expect(await screen.findByText('issue_comment')).toBeTruthy();
    expect(screen.getByText('custom')).toBeTruthy();
    expect(api.fetchEventTypes).toHaveBeenCalledWith('src-1');
    expect(api.fetchProviderEventTypes).not.toHaveBeenCalled();
    expect(api.fetchProviderFields).toHaveBeenCalledWith('github', 'push');

    fireEvent.click(screen.getByText('issue_comment'));
    expect(screen.getByRole('button', { name: 'Save filters' })).toBeTruthy();
    expect(api.fetchProviderFields).toHaveBeenCalledWith('github', 'issue_comment');
  });

  it('previews the first message with the data block', () => {
    renderDrawer(trigger());
    expect(screen.getByText('First message')).toBeTruthy();
    expect(screen.getByText(/"ref": ""/)).toBeTruthy();
  });
});

describe('sender restriction editing', () => {
  it('saves the logins and associations as the replacement restriction', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    fireEvent.change(await screen.findByLabelText('Logins'), {
      target: { value: 'octocat, hubot' },
    });
    fireEvent.click(screen.getByText('Member'));
    fireEvent.click(screen.getByRole('button', { name: 'Save senders' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      senderRestriction: { logins: ['octocat', 'hubot'], associations: ['MEMBER'] },
    });
  });

  it('seeds from the stored restriction', async () => {
    renderDrawer(trigger({ senderRestriction: { logins: ['octocat'], associations: ['OWNER'] } }));
    expect(((await screen.findByLabelText('Logins')) as HTMLInputElement).value).toBe('octocat');
  });

  it('is absent when the provider identifies no sender', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: { declared: ['alert.firing'], associations: [] },
      error: null,
    });
    renderDrawer(trigger());
    await screen.findByRole('button', { name: 'Event type' });
    expect(screen.queryByText('Senders')).toBeNull();
  });
});

describe('coalesce editing', () => {
  it('saves a key and window as the new setting', () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.change(screen.getByLabelText('Key'), { target: { value: '{{ pr.number }}' } });
    fireEvent.change(screen.getByLabelText('Window'), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save coalescing' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      coalesce: { key: '{{ pr.number }}', windowSeconds: 30 },
    });
  });

  it('seeds from the stored setting', () => {
    renderDrawer(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }));
    expect((screen.getByLabelText('Key') as HTMLInputElement).value).toBe('k');
    expect((screen.getByLabelText('Window') as HTMLInputElement).value).toBe('45');
  });

  it('clears the setting with null when switched off', () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }), onEdit);
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Save coalescing' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), { coalesce: null });
  });

  it('blocks saving when on without a window', () => {
    renderDrawer(trigger());
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.change(screen.getByLabelText('Key'), { target: { value: 'k' } });
    expect(
      (screen.getByRole('button', { name: 'Save coalescing' }) as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  it('is locked when overflow is not wait', () => {
    renderDrawer(trigger({ overflow: 'replace' }));
    expect(
      (screen.getByRole('switch', { name: /Coalesce events/ }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByText(/requires overflow set to wait/i)).toBeTruthy();
  });

  it('offers only Wait for overflow while coalescing is on', () => {
    renderDrawer(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }));
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Settings' }));
    const overflow = within(screen.getByRole('group', { name: 'Overflow behaviour' }));
    expect(overflow.getByText('Wait')).toBeTruthy();
    expect(overflow.queryByText('Replace')).toBeNull();
    expect(screen.getByText(/Coalescing requires Wait/)).toBeTruthy();
  });

  it('is absent for a scheduled trigger', () => {
    renderDrawer(trigger({ kind: 'scheduled' }));
    expect(screen.queryByText('Coalesce events')).toBeNull();
  });
});

describe('name editing', () => {
  /**
   * The name lives in Settings, which Tabs mounts only once selected. Radix
   * switches tabs on mousedown, not click.
   */
  function openSettings() {
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Settings' }));
  }

  it('blocks an empty name', () => {
    renderDrawer(trigger());
    openSettings();
    fireEvent.change(screen.getByLabelText('Trigger name'), { target: { value: '  ' } });

    expect(screen.getByText('Needs a name')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save name' }).hasAttribute('disabled')).toBe(true);
  });

  it('sends only the name, so a rename cannot disturb anything else', () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    openSettings();
    fireEvent.change(screen.getByLabelText('Trigger name'), { target: { value: ' renamed ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save name' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      name: 'renamed',
    });
  });
});

describe('schedule editing', () => {
  const scheduled = trigger({
    kind: 'scheduled',
    scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
  });

  it('reads as a row until Edit is pressed', () => {
    renderDrawer(scheduled);
    expect(screen.queryByLabelText('Cron expression')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(screen.getByLabelText('Cron expression')).toBeTruthy();
    expect(screen.getByLabelText('Timezone')).toBeTruthy();
  });

  it('blocks an expression that is not five fields', () => {
    renderDrawer(scheduled);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByLabelText('Cron expression'), { target: { value: '0 2 * *' } });

    expect(screen.getByText('Needs a five-field cron expression')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save schedule' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('sends the whole schedule, since the three settings only mean anything together', () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(scheduled, onEdit);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByLabelText('Cron expression'), {
      target: { value: '*/15 * * * *' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save schedule' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      scheduleConfig: { cronExpression: '*/15 * * * *', timezone: 'UTC', catchUp: false },
    });
  });
});

describe('read-only callers', () => {
  it('offers no edit affordances without onEdit', () => {
    render(
      <TriggerDrawer
        trigger={trigger({
          kind: 'scheduled',
          scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
        })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Add filter/ })).toBeNull();
  });
});

describe('ephemeral state is scoped to the open trigger', () => {
  it('does not carry a rotated secret from one trigger into the next', async () => {
    const onRotateSecret = vi.fn().mockResolvedValue('secret-for-A');
    const { rerender } = render(
      <TriggerDrawer
        trigger={trigger({ id: 'trg-A', name: 'trigger-a' })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );

    // Rotate A's secret: arm, then confirm. The revealed secret then shows.
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Break the old secret?' }));
    expect(await screen.findByText('secret-for-A')).toBeTruthy();

    // Reopening on a different trigger must not inherit A's revealed secret: B
    // shows the placeholder and the Rotate button, never A's live secret.
    rerender(
      <TriggerDrawer
        trigger={trigger({ id: 'trg-B', name: 'trigger-b' })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );

    expect(screen.queryByText('secret-for-A')).toBeNull();
    expect(screen.getByText('shown once at creation')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy();
  });
});

describe('run links', () => {
  // The drawer is the fourth surface that shows a delivery, and it resolves the
  // slug straight off the trigger it is scoped to rather than searching a list.
  // Nothing asserted that wiring, so deleting the prop left the whole triggers
  // suite green while the run silently became plain text here only.
  const dispatched = (): DeliveryRow =>
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

  it('links the run to the agent that owns this trigger', async () => {
    renderDrawer(trigger({ id: 't1', agentSlug: 'coder' }), undefined, [dispatched()]);
    // The drawer opens on Overview; the deliveries live behind their own tab,
    // and the Radix tab only activates under userEvent.
    await userEvent.click(screen.getByRole('tab', { name: 'Deliveries' }));
    await userEvent.click(screen.getByRole('button', { name: /payload for delivery d-1/i }));
    expect(screen.getByRole('link', { name: 'run-7' })).toHaveAttribute(
      'href',
      '/agents/coder/runs/run-7',
    );
  });

  it('renders the run as text when the trigger has no agent slug', async () => {
    renderDrawer(trigger({ id: 't1', agentSlug: '' }), undefined, [dispatched()]);
    // The drawer opens on Overview; the deliveries live behind their own tab,
    // and the Radix tab only activates under userEvent.
    await userEvent.click(screen.getByRole('tab', { name: 'Deliveries' }));
    await userEvent.click(screen.getByRole('button', { name: /payload for delivery d-1/i }));
    expect(screen.queryByRole('link', { name: 'run-7' })).toBeNull();
  });
});

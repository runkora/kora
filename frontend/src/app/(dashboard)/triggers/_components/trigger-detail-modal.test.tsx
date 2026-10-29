import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

// The modal reads the source's schema catalogue from the API.
const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
}));
vi.mock('../_services/triggers-api', () => api);

const GITHUB = {
  declared: ['push', 'issue_comment'],
  category: 'webhook',
  senders: {
    roles: [
      { value: 'OWNER', label: 'Owner', description: 'Owns the repository' },
      { value: 'MEMBER', label: 'Member', description: 'Member of the organization' },
    ],
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchProviderEventTypes.mockResolvedValue({
    eventTypes: GITHUB,
    error: null,
  });
  api.fetchEventTypes.mockResolvedValue({
    eventTypes: { ...GITHUB, observed: ['custom'] },
    error: null,
  });
  api.fetchProviderFields.mockResolvedValue({
    fields: [{ path: 'ref', group: 'event', description: 'The pushed ref' }],
    error: null,
  });
});

import { TriggerDetailModal } from './trigger-detail-modal';
import type { SourceRow, TriggerRow } from '../_types';
import { makeTrigger } from '../_types/fixtures';

/** The modal tests want a stored condition and a template worth displaying. */
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
    category: 'webhook',
    enabled: true,
    signingSecretSet: true,
    createdAt: '2026-07-01T00:00:00Z',
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
    declared: false,
  },
];

function renderDetailModal(row: TriggerRow, onEdit?: () => Promise<string | null>) {
  return renderWithProviders(
    <TriggerDetailModal
      canRunAgent
      trigger={row}
      sources={SOURCES}
      deliveries={[]}
      ingestHost="local.runkora.io:8443"
      onClose={vi.fn()}
      onToggleEnabled={vi.fn()}
      onDelete={vi.fn()}
      onEdit={onEdit ?? vi.fn().mockResolvedValue(null)}
    />,
  );
}

/**
 * The editors live on the Configuration tab, which Tabs mounts only once selected. Radix
 * switches tabs on mousedown, not click.
 */
function openConfiguration() {
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'Configuration' }));
}

/** Rotation and delete are the last Configuration section. */
async function openDangerZone() {
  await openSection('Danger zone');
}

/** The Configuration tab shows one section at a time; pick one from its rail. */
async function openSection(title: string) {
  openConfiguration();
  fireEvent.click(await screen.findByRole('button', { name: new RegExp(`^${title}`) }));
}

describe('prompt editing', () => {
  it('shows the prompt, which the panel previously never displayed', async () => {
    renderDetailModal(trigger());
    await openSection('Prompt');
    expect(screen.getByRole('heading', { name: 'Prompt' })).toBeTruthy();
  });

  it('offers no save affordance until something changes', () => {
    renderDetailModal(trigger());
    expect(screen.queryByRole('button', { name: 'Save prompt' })).toBeNull();
  });
});

describe('event and filter editing', () => {
  it('seeds the rows from the stored match condition', async () => {
    renderDetailModal(trigger());
    await openSection('Event filters');
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('opened');
  });

  it('grows a save row once a filter changes, and drops it on discard', async () => {
    renderDetailModal(trigger());
    await openSection('Event filters');
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });

    expect(screen.getByRole('button', { name: 'Save filters' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(screen.queryByRole('button', { name: 'Save filters' })).toBeNull();
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('opened');
  });

  it('blocks the save on a half-written filter', async () => {
    renderDetailModal(trigger());
    await openSection('Event filters');
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: '' } });

    expect(screen.getByText('Finish or remove the empty filter')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save filters' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('sends the event and the condition together', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger(), onEdit);
    await openSection('Event filters');
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
    renderDetailModal(trigger(), onEdit);
    await openSection('Event filters');
    fireEvent.click(screen.getByLabelText('Filter 1 operator'));
    fireEvent.click(screen.getByRole('option', { name: 'contains' }));
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
    renderDetailModal(trigger(), onEdit);
    await openSection('Event filters');
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save filters' }));

    expect(await screen.findByText('event_type is not valid for this source')).toBeTruthy();
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('closed');
  });
});

describe('schema catalogue', () => {
  it("offers the source's schema and received event types, and the edited event's fields", async () => {
    renderDetailModal(trigger());
    await openSection('Event filters');
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
});

describe('prompt preview', () => {
  it('shows the first message with the data block under Preview', async () => {
    renderDetailModal(trigger());
    await openSection('Prompt');
    expect(screen.queryByText('First message')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.getByText('First message')).toBeTruthy();
    expect(screen.getByText(/"ref": ""/)).toBeTruthy();
  });
});

describe('closing the modal', () => {
  it('discards unsaved changes in every section', async () => {
    const onClose = vi.fn();
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger()}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={onClose}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
      />,
    );
    await openSection('Trigger');
    const field = screen.getByLabelText(/Trigger name/) as HTMLInputElement;
    const before = field.value;
    fireEvent.change(field, { target: { value: `${before}-edited` } });
    expect(field.value).toBe(`${before}-edited`);

    fireEvent.click(screen.getByRole('button', { name: /close/i }));
    fireEvent.click(await screen.findByRole('button', { name: 'Discard changes' }));

    expect(onClose).toHaveBeenCalled();
    expect((screen.getByLabelText(/Trigger name/) as HTMLInputElement).value).toBe(before);
  });
});

describe('sender restriction editing', () => {
  it('saves the ids and roles as the replacement restriction', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger(), onEdit);
    await openSection('Senders');
    fireEvent.click(await screen.findByRole('radio', { name: 'Specific senders' }));
    fireEvent.change(await screen.findByLabelText('Additional senders'), {
      target: { value: 'octocat, hubot' },
    });
    fireEvent.click(screen.getByText('Member'));
    fireEvent.click(screen.getByRole('button', { name: 'Save senders' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      senderRestriction: { ids: ['octocat', 'hubot'], roles: ['MEMBER'] },
    });
  });

  it('seeds from the stored restriction', async () => {
    renderDetailModal(trigger({ senderRestriction: { ids: ['octocat'], roles: ['OWNER'] } }));
    await openSection('Senders');
    expect(
      ((await screen.findByLabelText('Additional senders')) as HTMLTextAreaElement).value,
    ).toBe('octocat');
    expect(screen.getByRole('checkbox', { name: 'Owner' })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Member' })).not.toBeChecked();
  });

  it('is absent when the provider identifies no sender', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: { declared: ['alert.firing'], category: 'webhook' },
      error: null,
    });
    renderDetailModal(trigger());
    await openSection('Event filters');
    await screen.findByRole('button', { name: 'Event type' });
    expect(screen.queryByText('Senders')).toBeNull();
  });
});

describe('coalesce editing', () => {
  it('saves a key and window as the new setting', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger(), onEdit);
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.change(screen.getByLabelText('Key'), { target: { value: '{{ pr.number }}' } });
    fireEvent.change(screen.getByLabelText('Window'), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save delivery' }));

    expect(onEdit).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      expect.objectContaining({ coalesce: { key: '{{ pr.number }}', windowSeconds: 30 } }),
    );
  });

  it('seeds from the stored setting', async () => {
    renderDetailModal(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }));
    await openSection('Delivery');
    expect((screen.getByLabelText('Key') as HTMLInputElement).value).toBe('k');
    expect((screen.getByLabelText('Window') as HTMLInputElement).value).toBe('45');
  });

  it('clears the setting with null when switched off', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }), onEdit);
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Save delivery' }));

    expect(onEdit).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      expect.objectContaining({ coalesce: null }),
    );
  });

  it('blocks saving when on without a window', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.change(screen.getByLabelText('Key'), { target: { value: 'k' } });
    expect(
      (screen.getByRole('button', { name: 'Save delivery' }) as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  it('is hidden when overflow is not wait', async () => {
    renderDetailModal(trigger({ overflow: 'replace' }));
    await openSection('Delivery');
    expect(screen.queryByRole('switch', { name: /Coalesce events/ })).toBeNull();
  });

  it('drops the setting when the overflow leaves wait', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }), onEdit);
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('radio', { name: /Replace/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Save delivery' }));

    expect(onEdit).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      expect.objectContaining({ overflow: 'replace', coalesce: null }),
    );
  });

  it('warns that Replace or Refuse turns coalescing off, until Wait is picked again', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }), onEdit);
    await openSection('Delivery');
    expect(screen.queryByText(/Coalescing only works with Wait/)).toBeNull();

    fireEvent.click(screen.getByRole('radio', { name: /Replace/ }));
    expect(
      screen.getByText('Coalescing only works with Wait. Saving with Replace turns it off.'),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: /Refuse/ }));
    expect(
      screen.getByText('Coalescing only works with Wait. Saving with Refuse turns it off.'),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: /Wait/ }));
    expect(screen.queryByText(/Coalescing only works with Wait/)).toBeNull();
    // The draft kept coalescing while the warning was up.
    expect((screen.getByLabelText('Key') as HTMLInputElement).value).toBe('k');
    fireEvent.click(screen.getByRole('radio', { name: /Replace/ }));
    fireEvent.click(screen.getByRole('radio', { name: /Wait/ }));
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    fireEvent.change(screen.getByLabelText('Time to start'), { target: { value: '31' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save delivery' }));
    expect(onEdit).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      expect.objectContaining({
        overflow: 'wait',
        coalesce: expect.objectContaining({ key: 'k' }),
      }),
    );
  });

  it('does not warn when coalescing is off under Replace', async () => {
    renderDetailModal(trigger({ overflow: 'replace' }));
    await openSection('Delivery');
    expect(screen.queryByText(/Coalescing only works with Wait/)).toBeNull();
  });

  it('does not warn for a scheduled trigger under Replace', async () => {
    renderDetailModal(trigger({ kind: 'scheduled', overflow: 'replace' }));
    await openSection('Delivery');
    expect(screen.queryByText(/Coalescing only works with Wait/)).toBeNull();
  });

  it('is absent for a scheduled trigger', async () => {
    renderDetailModal(trigger({ kind: 'scheduled' }));
    await openSection('Delivery');
    expect(screen.queryByText('Coalesce events')).toBeNull();
  });
});

describe('name editing', () => {
  it('blocks an empty name', async () => {
    renderDetailModal(trigger());
    await openSection('Trigger');
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: '  ' } });

    expect(screen.getByText('Needs a name')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save name' }).hasAttribute('disabled')).toBe(true);
  });

  it('sends only the name, so a rename cannot disturb anything else', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger(), onEdit);
    await openSection('Trigger');
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: ' renamed ' } });
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

  it('edits in place, with no Edit button to press first', async () => {
    renderDetailModal(scheduled);
    await openSection('Schedule');
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.getByLabelText('Frequency')).toBeTruthy();
    expect(screen.getByLabelText('Timezone')).toBeTruthy();
    expect(screen.getByRole('switch', { name: /Catch up on missed runs/ })).toBeTruthy();
  });

  it('blocks an expression that is not five fields', async () => {
    renderDetailModal(scheduled);
    await openSection('Schedule');
    fireEvent.click(screen.getByRole('radio', { name: /Custom expression/ }));
    fireEvent.change(screen.getByLabelText('Cron expression'), { target: { value: '0 2 * *' } });

    expect(screen.getAllByText('Needs five fields separated by spaces.').length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Save schedule' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('blocks an expression with a field out of range', async () => {
    renderDetailModal(scheduled);
    await openSection('Schedule');
    fireEvent.click(screen.getByRole('radio', { name: /Custom expression/ }));
    fireEvent.change(screen.getByLabelText('Cron expression'), { target: { value: '0 25 * * *' } });

    expect(screen.getAllByText('Hour must be between 0 and 23.').length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Save schedule' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('sends the whole schedule, since the three settings only mean anything together', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(scheduled, onEdit);
    await openSection('Schedule');
    fireEvent.click(screen.getByText('Minutes'));
    fireEvent.click(screen.getByRole('button', { name: 'Save schedule' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      scheduleConfig: { cronExpression: '*/15 * * * *', timezone: 'UTC', catchUp: false },
    });
  });
});

describe('read-only callers', () => {
  it('offers no edit affordances without onEdit', () => {
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger({
          kind: 'scheduled',
          scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
        })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
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
    const { rerender } = renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger({ id: 'trg-A', name: 'trigger-a' })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );

    // Rotate A's secret: arm, then confirm. The revealed secret then shows.
    await openDangerZone();
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Rotate secret' }));
    expect(await screen.findByText('secret-for-A')).toBeTruthy();

    // Reopening on a different trigger must not inherit A's revealed secret: B
    // shows the Rotate button and no revealed secret, never A's live secret.
    rerender(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger({ id: 'trg-B', name: 'trigger-b' })}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );

    // The tab is forgotten too, so B opens on Overview and the zone is reopened.
    expect(screen.queryByText('secret-for-A')).toBeNull();
    await openDangerZone();
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy();
    expect(screen.queryByText('secret-for-A')).toBeNull();
  });

  it('forgets the selected tab when the modal closes', async () => {
    const props = {
      canRunAgent: true,
      sources: SOURCES,
      deliveries: [],
      ingestHost: 'local.runkora.io:8443',
      onClose: vi.fn(),
      onToggleEnabled: vi.fn(),
      onDelete: vi.fn(),
      onEdit: vi.fn().mockResolvedValue(null),
    };
    const { rerender } = renderWithProviders(
      <TriggerDetailModal {...props} trigger={trigger({ id: 'trg-A' })} />,
    );
    await screen.findByRole('tab', { name: 'Configuration' });
    openConfiguration();
    expect(screen.getByRole('tab', { name: 'Configuration' }).getAttribute('data-state')).toBe(
      'active',
    );

    rerender(<TriggerDetailModal {...props} trigger={null} />);
    rerender(<TriggerDetailModal {...props} trigger={trigger({ id: 'trg-A' })} />);

    expect((await screen.findByRole('tab', { name: 'Overview' })).getAttribute('data-state')).toBe(
      'active',
    );
  });
});

describe('permissions', () => {
  function renderWith(scopes: string[], row: TriggerRow, canRunAgent = true) {
    return renderWithProviders(
      <TriggerDetailModal
        canRunAgent={canRunAgent}
        trigger={row}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onFire={vi.fn()}
      />,
      { scopes },
    );
  }
  const manual = (canFire: boolean) => trigger({ kind: 'manual', canFire });
  const scheduled = trigger({
    kind: 'scheduled',
    scheduleConfig: { cronExpression: '0 2 * * *', timezone: 'UTC', catchUp: false },
  });

  it('offers Run now only when the API says the caller may fire', () => {
    renderWith(['triggers:read'], manual(true));
    expect(screen.getByRole('button', { name: 'Run now' })).toBeTruthy();
  });

  it('disables Run now with a hint without fire access, even with triggers:write', () => {
    renderWith(['triggers:read', 'triggers:write'], manual(false));
    expect(screen.getByRole('button', { name: 'Run now' })).toBeDisabled();
    expect(screen.getByLabelText("You don't have permission to fire this trigger.")).toBeTruthy();
  });

  it('disables the enable switch and edits for a read-only caller, under one notice', async () => {
    renderWith(['triggers:read'], scheduled);
    await openSection('Schedule');
    expect(screen.getByRole('switch', { name: /^(Enable|Disable)/ })).toBeDisabled();
    expect(screen.queryByLabelText('Frequency')).toBeNull();
    expect(screen.getByText("You don't have permission to edit triggers.")).toBeTruthy();
  });

  it('says why delivery is read only without triggers:write', async () => {
    renderWith(['triggers:read'], trigger());
    await openSection('Delivery');
    expect(screen.getByText("You don't have permission to change delivery settings.")).toBeTruthy();
    expect(screen.getByLabelText('Time to start')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Save delivery' })).toBeDisabled();
  });

  it('keeps the switch and edits for a caller with triggers:write', async () => {
    renderWith(['triggers:read', 'triggers:write'], scheduled);
    await openSection('Schedule');
    expect(screen.getByRole('switch', { name: /^(Enable|Disable)/ })).toBeEnabled();
    expect(screen.getByLabelText('Frequency')).toBeTruthy();
    expect(screen.queryByText("You don't have permission to edit triggers.")).toBeNull();
  });

  it('keeps run-shaping edits read only without run authority on the agent', async () => {
    renderWith(['triggers:read', 'triggers:write'], scheduled, false);
    await openSection('Schedule');
    expect(
      screen.getByText(/You don't have permission to run this trigger's agent\./),
    ).toBeTruthy();
    expect(screen.getByRole('switch', { name: /^(Enable|Disable)/ })).toBeDisabled();
    expect(screen.queryByLabelText('Frequency')).toBeNull();
  });
});

describe('managed in git', () => {
  function renderManaged(
    row: TriggerRow,
    opts: { apiAuthoring?: boolean; sources?: SourceRow[] } = {},
  ) {
    return renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={row}
        sources={opts.sources ?? SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={vi.fn().mockResolvedValue('s')}
      />,
      { apiAuthoring: opts.apiAuthoring ?? true },
    );
  }

  it('shows a declared trigger read-only with no delete', async () => {
    renderManaged(trigger({ declared: true }));
    expect(screen.getByText('Managed in git')).toBeTruthy();
    expect(screen.getByText(/item is managed in git/)).toBeTruthy();
    expect(screen.getByRole('switch', { name: /^(Enable|Disable) / })).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
    await openDangerZone();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('treats every trigger in a locked tenant as managed in git, keeping API source rotation', async () => {
    renderManaged(trigger(), { apiAuthoring: false });
    expect(screen.getByText(/tenant is managed in git/)).toBeTruthy();
    await openDangerZone();
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeEnabled();
  });

  it.each(['Trigger', 'Event filters', 'Senders', 'Prompt', 'Delivery', 'Danger zone'])(
    'heads the %s section with who manages the trigger',
    async (section) => {
      renderManaged(trigger({ declared: true, repository: 'acme/platform-config' }));
      await openSection(section);
      const notice = screen.getByLabelText('Managed in git');
      expect(notice.textContent).toContain('Managed by acme/platform-config');
      expect(notice.textContent).toContain('Changes made here are not saved.');
    },
  );

  it('explains the disabled Delivery actions with the same message', async () => {
    renderManaged(trigger({ declared: true, repository: 'acme/platform-config' }));
    await openSection('Delivery');
    expect(
      screen.getByText(
        'Managed by acme/platform-config. Edit it in acme/platform-config; changes made here are not saved.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save delivery' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled();
  });

  it('names a locked tenant when the trigger is not declared', async () => {
    renderManaged(trigger(), { apiAuthoring: false });
    await openSection('Prompt');
    expect(screen.getByLabelText('Managed in git').textContent).toContain('Managed in git');
  });

  it.each(['Trigger', 'Delivery', 'Danger zone'])(
    'shows no managed notice on %s for an unmanaged trigger',
    async (section) => {
      renderManaged(trigger());
      await openSection(section);
      expect(screen.queryByLabelText('Managed in git')).toBeNull();
    },
  );

  it('offers no rotation for a declared source', async () => {
    renderManaged(trigger(), { sources: [{ ...SOURCES[0]!, declared: true }] });
    expect(screen.getByText('Managed in git')).toBeTruthy();
    await openDangerZone();
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeDisabled();
  });
});

describe('a source whose provider issues the secret', () => {
  const SLACK: SourceRow[] = [
    { ...SOURCES[0]!, name: 'acme slack', provider: 'slack', category: 'chat' },
  ];

  async function renderSlack(
    onRotateSecret: (t: TriggerRow, secret?: string) => Promise<string | null>,
  ) {
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger()}
        sources={SLACK}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );
    await openDangerZone();
  }

  it('asks for the new secret instead of generating one', async () => {
    const onRotateSecret = vi.fn().mockResolvedValue('');
    await renderSlack(onRotateSecret);
    expect(screen.queryByRole('button', { name: 'Rotate' })).toBeNull();

    fireEvent.change(screen.getByLabelText(/Signing secret/), {
      target: { value: ' new-secret ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Replace secret' }));

    expect(onRotateSecret).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      'new-secret',
      false,
    );
    expect(await screen.findByText(/The new secret is stored/)).toBeTruthy();
    expect((screen.getByLabelText(/Signing secret/) as HTMLInputElement).value).toBe('');
  });

  it('sets the first secret of a source that is waiting for one', async () => {
    const onRotateSecret = vi.fn().mockResolvedValue('');
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger()}
        sources={[{ ...SLACK[0]!, signingSecretSet: false }]}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );
    await openDangerZone();
    expect(screen.getByText('Waiting for signing secret')).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: 'first' } });
    fireEvent.click(screen.getByRole('button', { name: 'Set secret' }));

    expect(onRotateSecret).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      'first',
      true,
    );
    expect(await screen.findByText(/The signing secret is stored/)).toBeTruthy();
  });

  it('names the platform in the rotation hint', async () => {
    await renderSlack(vi.fn());
    expect(screen.getByText(/Regenerate it in your Slack app/)).toBeTruthy();
  });

  it('keeps the typed secret when the rotation fails', async () => {
    await renderSlack(vi.fn().mockResolvedValue(null));
    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: 'new-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Replace secret' }));
    expect(await screen.findByRole('button', { name: 'Replace secret' })).toBeTruthy();
    expect((screen.getByLabelText(/Signing secret/) as HTMLInputElement).value).toBe('new-secret');
    expect(screen.queryByText(/The new secret is stored/)).toBeNull();
  });
});

describe('a chat command trigger', () => {
  const CHAT_SOURCES: SourceRow[] = [
    { ...SOURCES[0]!, id: 'src-1', name: 'acme-slack', provider: 'slack', category: 'chat' },
  ];

  beforeEach(() => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: {
        declared: ['mention', 'direct_message'],
        category: 'chat',
        senders: { roles: [] },
      },
      error: null,
    });
  });

  function renderChat(row: TriggerRow, onEdit = vi.fn().mockResolvedValue(null)) {
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={row}
        sources={CHAT_SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={onEdit}
      />,
    );
    return onEdit;
  }

  it('reads as a chat command with the event label', () => {
    renderChat(trigger({ eventType: 'mention', matchCondition: {} }));
    expect(screen.getByText('Chat command · acme-slack · Mention')).toBeTruthy();
    expect(screen.getByText('Request URL')).toBeTruthy();
  });

  it('offers the chat events as cards and saves the chosen one', async () => {
    const onEdit = renderChat(trigger({ eventType: 'mention', matchCondition: {} }));
    await openSection('Event filters');
    fireEvent.click(await screen.findByRole('radio', { name: /Direct message/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Save filters' }));
    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      eventType: 'direct_message',
      matchCondition: {},
      matchContains: {},
    });
  });

  it('shows the stored sender ids with no role cards', async () => {
    renderChat(
      trigger({ eventType: 'mention', senderRestriction: { ids: ['U012AB3CD'], roles: [] } }),
    );
    await openSection('Senders');
    expect(((await screen.findByLabelText('Allowed senders')) as HTMLInputElement).value).toBe(
      'U012AB3CD',
    );
    expect(screen.queryByRole('checkbox')).toBeNull();
  });
});

describe('source missing', () => {
  it('says the source was deleted and shows no endpoint', () => {
    renderDetailModal(trigger({ sourceId: '', sourceMissing: true }));
    expect(screen.getByText(/The source of this trigger was deleted/)).toBeTruthy();
    expect(screen.queryByText(/\/api\/v1\/webhooks\//)).toBeNull();
  });

  it('can be turned off but not back on', () => {
    const view = renderDetailModal(trigger({ sourceId: '', sourceMissing: true, enabled: true }));
    expect(screen.getByRole('switch', { name: /^Disable/ }).hasAttribute('disabled')).toBe(false);
    view.unmount();
    renderDetailModal(trigger({ sourceId: '', sourceMissing: true, enabled: false }));
    expect(screen.getByRole('switch', { name: /^Enable/ }).hasAttribute('disabled')).toBe(true);
  });
});

describe('the Configuration sections follow the wizard', () => {
  it('lists the wizard sections in the wizard order for a webhook trigger', async () => {
    renderDetailModal(trigger());
    openConfiguration();
    // Senders appears once the source's catalogue says its provider identifies senders.
    await screen.findByRole('button', { name: /Senders/ });
    const rail = document.querySelector('[data-slot="configuration-rail"]');
    const titles = Array.from(rail?.querySelectorAll('button') ?? []).map(
      (b) => b.textContent ?? '',
    );
    const order = ['Trigger', 'Event filters', 'Senders', 'Prompt', 'Delivery', 'Danger zone'];
    expect(order.map((t) => titles.findIndex((x) => x.startsWith(t)))).toEqual([0, 1, 2, 3, 4, 5]);
  });

  it('shows the source as immutable, since a trigger cannot move to another source', async () => {
    renderDetailModal(trigger());
    await openSection('Trigger');
    expect(screen.getByText('Read-only')).toBeTruthy();
    expect(screen.getByLabelText('Source')).toBeDisabled();
  });

  it('links the target agent from the Overview, not the Configuration rail', async () => {
    renderDetailModal(trigger());
    expect(screen.getByRole('link', { name: /Open agent/ })).toBeTruthy();
    openConfiguration();
    await screen.findByRole('button', { name: /^Trigger/ });
    expect(screen.queryByRole('button', { name: /^Target/ })).toBeNull();
  });
});

describe('delivery editing', () => {
  it('saves the whole section in one request, so the bounds are never split', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDetailModal(trigger(), onEdit);
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('button', { name: 'More parallel runs' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save delivery' }));

    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onEdit).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      expect.objectContaining({
        maxParallel: expect.any(Number),
        overflow: expect.any(String),
        maxQueueDepth: expect.any(Number),
        timeoutSeconds: expect.any(Number),
        dedupeEnabled: expect.any(Boolean),
        autopauseEnabled: expect.any(Boolean),
      }),
    );
  });

  it('keeps save disabled until something changes', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    expect(
      (screen.getByRole('button', { name: 'Save delivery' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'More parallel runs' }));
    expect(
      (screen.getByRole('button', { name: 'Save delivery' }) as HTMLButtonElement).disabled,
    ).toBe(false);
  });

  it('marks the recommended behaviour for the kind and explains it', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    expect(screen.getAllByText('Recommended')).toHaveLength(1);
    expect(screen.getByText(/is recommended for webhook triggers/)).toBeTruthy();
  });

  it('recommends replace for a scheduled trigger', async () => {
    renderDetailModal(trigger({ kind: 'scheduled' }));
    await openSection('Delivery');
    expect(screen.getByText(/is recommended for scheduled triggers/)).toBeTruthy();
  });

  it('states each invalid value under its field and counts them beside save', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    fireEvent.change(screen.getByLabelText('Queue size'), { target: { value: '600' } });
    fireEvent.change(screen.getByLabelText('Time to start'), { target: { value: '2' } });
    expect(screen.getByText('Queue size must be between 1 and 500.')).toBeTruthy();
    expect(screen.getByText('Time to start must be between 5 and 600 seconds.')).toBeTruthy();
    expect(screen.getByText('Fix 2 values to save.')).toBeTruthy();
    expect(screen.getByText('Fix the errors above to see what these settings do.')).toBeTruthy();
    expect(
      (screen.getByRole('button', { name: 'Save delivery' }) as HTMLButtonElement).disabled,
    ).toBe(true);
  });

  it('asks for a key when coalescing is switched on without one', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    fireEvent.click(screen.getByRole('switch', { name: /Coalesce events/ }));
    expect(screen.getByText('Key is required.')).toBeTruthy();
    expect(screen.getByText('Window is required.')).toBeTruthy();
    expect(screen.getByText('Fix 2 values to save.')).toBeTruthy();
  });
});

describe('the signing secret', () => {
  it('is rotated in the Danger zone, and the Overview does not mention it', async () => {
    renderDetailModal(trigger());
    expect(screen.queryByText('Signing secret')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Rotate' })).toBeNull();
    await openDangerZone();
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy();
  });
});

describe('a generic webhook source', () => {
  async function renderGeneric(
    method: string,
    onRotateSecret: (t: TriggerRow, secret?: string) => Promise<string | null> = vi.fn(),
    eventTypesFail = false,
    section = 'Danger zone',
  ) {
    api.fetchEventTypes.mockResolvedValue(
      eventTypesFail
        ? { eventTypes: null, error: 'unavailable' }
        : { eventTypes: { declared: [], observed: ['Issue'], category: 'webhook' }, error: null },
    );
    api.fetchProviderFields.mockResolvedValue({
      fields: [{ path: 'meta.event_type', group: 'meta', description: 'The event type' }],
      error: null,
    });
    const generic: SourceRow = {
      ...SOURCES[0]!,
      name: 'linear',
      provider: 'generic',
      config: { verification: { method } },
    };
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={trigger({ eventType: 'Issue', matchCondition: { 'data.state': ['open'] } })}
        sources={[generic]}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );
    await openSection(section);
  }

  it('takes a typed event type and filter field, with no sender restriction', async () => {
    await renderGeneric('hmac_sha256', vi.fn(), false, 'Event filters');
    const eventType = await screen.findByRole('textbox', { name: 'Event type' });
    expect(eventType).toHaveValue('Issue');
    expect(screen.getByRole('textbox', { name: 'Filter 1 field' })).toHaveValue('data.state');

    fireEvent.change(eventType, { target: { value: 'Comment' } });
    expect(screen.getByRole('button', { name: 'Save filters' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Senders/ })).toBeNull();
  });

  it('rotates to a generated secret or a pasted one', async () => {
    const onRotateSecret = vi.fn().mockResolvedValue('');
    await renderGeneric('standard_webhooks', onRotateSecret);
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy();
    expect(screen.getByText(/Standard Webhooks signature/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: 'whsec_x' } });
    fireEvent.click(screen.getByRole('button', { name: 'Replace secret' }));
    expect(onRotateSecret).toHaveBeenCalledWith(
      expect.objectContaining({ id: 't1' }),
      'whsec_x',
      false,
    );
    expect(await screen.findByText(/The new secret is stored/)).toBeTruthy();
  });

  it('drops a generated secret once a pasted one replaces it', async () => {
    const onRotateSecret = vi.fn().mockResolvedValueOnce('GENERATED123').mockResolvedValueOnce('');
    await renderGeneric('hmac_sha256', onRotateSecret);
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate secret' }));
    expect(await screen.findByText('GENERATED123')).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: 'pasted' } });
    fireEvent.click(screen.getByRole('button', { name: 'Replace secret' }));
    expect(await screen.findByText(/The new secret is stored/)).toBeTruthy();
    expect(screen.queryByText('GENERATED123')).toBeNull();
  });

  it('takes a typed event type when the event types fail to load', async () => {
    await renderGeneric('bearer', vi.fn(), true, 'Event filters');
    await waitFor(() => expect(api.fetchEventTypes).toHaveBeenCalled());
    expect(screen.getByRole('textbox', { name: 'Event type' })).toHaveValue('Issue');
  });

  it('refuses to save an event type that is only whitespace', async () => {
    await renderGeneric('hmac_sha256', vi.fn(), false, 'Event filters');
    fireEvent.change(await screen.findByRole('textbox', { name: 'Event type' }), {
      target: { value: '  ' },
    });
    expect(screen.getByText('Needs an event type')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save filters' })).toBeDisabled();
  });

  it('does not ask a schema source for an event type the picker sets', async () => {
    renderDetailModal(trigger({ eventType: '' }));
    await openSection('Event filters');
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });
    expect(screen.queryByText('Needs an event type')).toBeNull();
    expect(screen.getByRole('button', { name: 'Save filters' })).not.toBeDisabled();
  });

  it('offers only a generated bearer token', async () => {
    await renderGeneric('bearer');
    expect(screen.getByRole('button', { name: 'Rotate' })).toBeTruthy();
    expect(screen.queryByLabelText(/Signing secret/)).toBeNull();
    expect(screen.getByText(/bearer token in the Authorization header/)).toBeTruthy();
  });
});

describe('closing with unsaved edits', () => {
  function renderClosable(row: TriggerRow = trigger()) {
    const onClose = vi.fn();
    renderWithProviders(
      <TriggerDetailModal
        canRunAgent
        trigger={row}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="local.runkora.io:8443"
        onClose={onClose}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
      />,
    );
    return onClose;
  }

  async function dirtyFilters() {
    await openSection('Event filters');
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'closed' } });
  }

  it('closes at once when nothing is dirty', () => {
    const onClose = renderClosable();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
  });

  it('asks before Escape discards an edit, and keeps it on Cancel', async () => {
    const onClose = renderClosable();
    await dirtyFilters();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(await screen.findByText('Discard unsaved changes?')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByText('Discard unsaved changes?')).toBeNull();
    expect((screen.getByLabelText('Filter 1 values') as HTMLInputElement).value).toBe('closed');
  });

  it('asks before the close button discards, and closes on Discard changes', async () => {
    const onClose = renderClosable();
    await dirtyFilters();
    fireEvent.click(screen.getByRole('button', { name: /^close/i }));
    expect(await screen.findByText('Discard unsaved changes?')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe('delivery defaults and number fields', () => {
  it('does not flag a trigger row that lacks queue and timeout values', async () => {
    renderDetailModal(
      trigger({ maxQueueDepth: undefined, timeoutSeconds: undefined } as Partial<TriggerRow>),
    );
    await openSection('Delivery');
    expect(screen.queryByText(/Fix \d+ values? to save/)).toBeNull();
    expect((screen.getByLabelText('Queue size') as HTMLInputElement).value).toBe('32');
    expect((screen.getByLabelText('Time to start') as HTMLInputElement).value).toBe('30');
  });

  it('lets Queue size be emptied, calls it required and blocks save', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    const field = screen.getByLabelText('Queue size') as HTMLInputElement;
    fireEvent.change(field, { target: { value: '' } });
    expect(field.value).toBe('');
    expect(screen.getByText('Queue size is required.')).toBeTruthy();
    expect(field.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('Fix 1 value to save.')).toBeTruthy();
    expect(
      (screen.getByRole('button', { name: 'Save delivery' }) as HTMLButtonElement).disabled,
    ).toBe(true);

    fireEvent.change(field, { target: { value: '1a2' } });
    expect(field.value).toBe('12');
    expect(screen.queryByText('Queue size is required.')).toBeNull();
  });

  it('lets Time to start be emptied, calls it required, and restores it on Discard', async () => {
    renderDetailModal(trigger());
    await openSection('Delivery');
    const field = screen.getByLabelText('Time to start') as HTMLInputElement;
    const before = field.value;
    fireEvent.change(field, { target: { value: '' } });
    expect(field.value).toBe('');
    expect(screen.getByText('Time to start is required.')).toBeTruthy();
    expect(screen.getByText('Fix 1 value to save.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect((screen.getByLabelText('Time to start') as HTMLInputElement).value).toBe(before);
  });

  it('calls an emptied coalesce Window required', async () => {
    renderDetailModal(trigger({ coalesce: { key: 'k', windowSeconds: 45 } }));
    await openSection('Delivery');
    fireEvent.change(screen.getByLabelText('Window'), { target: { value: '' } });
    expect(screen.getByText('Window is required.')).toBeTruthy();
  });
});

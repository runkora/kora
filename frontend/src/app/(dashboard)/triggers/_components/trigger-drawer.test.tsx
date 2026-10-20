import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

// The drawer reads the source's schema catalogue from the API.
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

import { TriggerDrawer } from './trigger-drawer';
import type { SourceRow, TriggerRow } from '../_types';
import { makeTrigger } from '../_types/fixtures';

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
    category: 'webhook',
    enabled: true,
    signingSecretSet: true,
    createdAt: '2026-07-01T00:00:00Z',
    rejectionCount: 0,
    lastRejectionAt: null,
    lastRejectionReason: null,
  },
];

function renderDrawer(row: TriggerRow, onEdit?: () => Promise<string | null>) {
  return renderWithProviders(
    <TriggerDrawer
      canRunAgent
      trigger={row}
      sources={SOURCES}
      deliveries={[]}
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
  it('saves the ids and roles as the replacement restriction', async () => {
    const onEdit = vi.fn().mockResolvedValue(null);
    renderDrawer(trigger(), onEdit);
    fireEvent.change(await screen.findByLabelText('Allowed senders'), {
      target: { value: 'octocat, hubot' },
    });
    fireEvent.click(screen.getByText('Member'));
    fireEvent.click(screen.getByRole('button', { name: 'Save senders' }));

    expect(onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), {
      senderRestriction: { ids: ['octocat', 'hubot'], roles: ['MEMBER'] },
    });
  });

  it('seeds from the stored restriction', async () => {
    renderDrawer(trigger({ senderRestriction: { ids: ['octocat'], roles: ['OWNER'] } }));
    expect(((await screen.findByLabelText('Allowed senders')) as HTMLInputElement).value).toBe(
      'octocat',
    );
    expect(screen.getByRole('checkbox', { name: 'Owner' })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Member' })).not.toBeChecked();
  });

  it('is absent when the provider identifies no sender', async () => {
    api.fetchEventTypes.mockResolvedValue({
      eventTypes: { declared: ['alert.firing'], category: 'webhook' },
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
    renderWithProviders(
      <TriggerDrawer
        canRunAgent
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
    const { rerender } = renderWithProviders(
      <TriggerDrawer
        canRunAgent
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
        canRunAgent
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

describe('permissions', () => {
  function renderWith(scopes: string[], row: TriggerRow, canRunAgent = true) {
    return renderWithProviders(
      <TriggerDrawer
        canRunAgent={canRunAgent}
        trigger={row}
        sources={SOURCES}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
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

  it('disables the enable switch and edits for a read-only caller, under one notice', () => {
    renderWith(['triggers:read'], scheduled);
    expect(screen.getByRole('switch')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
    expect(screen.getByText("You don't have permission to edit triggers.")).toBeTruthy();
  });

  it('keeps the switch and edits for a caller with triggers:write', () => {
    renderWith(['triggers:read', 'triggers:write'], scheduled);
    expect(screen.getByRole('switch')).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Edit' })).toBeEnabled();
    expect(screen.queryByText("You don't have permission to edit triggers.")).toBeNull();
  });

  it('keeps run-shaping edits read only without run authority on the agent', () => {
    renderWith(['triggers:read', 'triggers:write'], scheduled, false);
    expect(
      screen.getByText(/You don't have permission to run this trigger's agent\./),
    ).toBeTruthy();
    expect(screen.getByRole('switch')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
  });
});

describe('a source whose provider issues the secret', () => {
  const SLACK: SourceRow[] = [
    { ...SOURCES[0]!, name: 'acme slack', provider: 'slack', category: 'chat' },
  ];

  function renderSlack(onRotateSecret: (t: TriggerRow, secret?: string) => Promise<string | null>) {
    renderWithProviders(
      <TriggerDrawer
        canRunAgent
        trigger={trigger()}
        sources={SLACK}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );
  }

  it('asks for the new secret instead of generating one', async () => {
    const onRotateSecret = vi.fn().mockResolvedValue('');
    renderSlack(onRotateSecret);
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
      <TriggerDrawer
        canRunAgent
        trigger={trigger()}
        sources={[{ ...SLACK[0]!, signingSecretSet: false }]}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
        onClose={vi.fn()}
        onToggleEnabled={vi.fn()}
        onDelete={vi.fn()}
        onEdit={vi.fn().mockResolvedValue(null)}
        onRotateSecret={onRotateSecret}
      />,
    );
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

  it('names the platform in the rotation hint', () => {
    renderSlack(vi.fn());
    expect(screen.getByText(/Regenerate it in your Slack app/)).toBeTruthy();
  });

  it('keeps the typed secret when the rotation fails', async () => {
    renderSlack(vi.fn().mockResolvedValue(null));
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
      <TriggerDrawer
        canRunAgent
        trigger={row}
        sources={CHAT_SOURCES}
        deliveries={[]}
        ingestHost="orbit.local.clearroute.io:8443"
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
    expect(((await screen.findByLabelText('Allowed senders')) as HTMLInputElement).value).toBe(
      'U012AB3CD',
    );
    expect(screen.queryByRole('checkbox')).toBeNull();
  });
});

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { TriggerDrawer } from './trigger-drawer';
import type { SourceRow, TriggerRow } from '../_types';

function trigger(overrides: Partial<TriggerRow> = {}): TriggerRow {
  return {
    id: 't1',
    kind: 'webhook',
    sourceId: 'src-1',
    name: 'gh-push',
    agentTypeId: 'at-1',
    agentSlug: 'coder',
    eventType: 'push',
    matchCondition: { action: ['opened'] },
    promptTemplate: 'Review {{ push.branch }}',
    enabled: true,
    createdAt: '2026-07-01T00:00:00Z',
    maxParallel: 4,
    overflow: 'wait',
    maxQueueDepth: 32,
    timeoutSeconds: 30,
    dedupeEnabled: true,
    autopauseEnabled: true,
    consecutiveFailures: 0,
    autoPausedAt: null,
    ...overrides,
  };
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

function renderDrawer(row: TriggerRow, onEdit?: () => Promise<string | null>) {
  return render(
    <TriggerDrawer
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

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import type { TriggerField } from '@orbit/shared/api';

// The wizard reads each provider's schema catalogue from the API.
const GITHUB_FIELDS: Record<string, TriggerField[]> = {
  issue_comment: [
    { path: 'action', group: 'event', description: 'What happened', enum: ['created', 'edited'] },
    { path: 'comment.body', group: 'comment', description: 'The comment text' },
    { path: 'meta.event_type', group: 'meta', description: 'The event type' },
  ],
};
const ALERT_FIELDS: TriggerField[] = [
  { path: 'status', group: 'event', description: 'firing or resolved' },
  { path: 'commonLabels.<name>', group: 'commonLabels', description: 'A label', pattern: true },
];
const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
}));
vi.mock('../../_services/triggers-api', () => api);

import { TriggerWizard } from './trigger-wizard';
import { StepDelivery } from './step-delivery';
import { StepReview } from './step-review';
import { StepTarget } from './step-target';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import { initialState } from './wizard-state';
import type { TriggerKind, WizardContext } from './types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github' }],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchProviderEventTypes.mockImplementation(async (provider: string) => ({
    eventTypes:
      provider === 'alertmanager'
        ? { declared: ['alert.firing'], associations: [] }
        : { declared: ['issue_comment', 'push'], associations: ['OWNER', 'MEMBER'] },
    error: null,
  }));
  api.fetchEventTypes.mockImplementation(async (sourceId: string) => ({
    eventTypes:
      sourceId === 'src-2'
        ? { declared: ['alert.firing'], associations: [] }
        : {
            declared: ['issue_comment', 'push'],
            observed: ['custom'],
            associations: ['OWNER', 'MEMBER'],
          },
    error: null,
  }));
  api.fetchProviderFields.mockImplementation(async (provider: string, eventType: string) => ({
    fields: provider === 'alertmanager' ? ALERT_FIELDS : (GITHUB_FIELDS[eventType] ?? []),
    error: null,
  }));
});

function renderWizard(initialKind?: TriggerKind) {
  return render(
    <TriggerWizard
      initialKind={initialKind}
      agents={CONTEXT.agents}
      sources={CONTEXT.sources}
      ingestHost={CONTEXT.ingestHost}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
}

describe('TriggerWizard picker phase', () => {
  it('opens on the picker when no kind is given', () => {
    renderWizard();
    expect(screen.getByText('Choose what starts it.', { exact: false })).toBeTruthy();
  });

  it('lists every registry entry, and every planned kind', () => {
    renderWizard();
    for (const id of KIND_ORDER) {
      const def = TRIGGER_KINDS[id];
      expect(screen.getAllByText(def.title).length).toBeGreaterThan(0);
      expect(screen.getByText(def.example)).toBeTruthy();
    }
    for (const planned of PLANNED_KINDS) {
      expect(screen.getByText(planned.title)).toBeTruthy();
    }
  });
});

describe('TriggerWizard step phase', () => {
  const FIRST_STEP: Record<TriggerKind, string> = {
    webhook: 'Source',
    scheduled: 'Schedule',
    manual: 'Inputs',
  };

  it.each(KIND_ORDER)('opens %s on the step that defines the kind', (kind) => {
    const view = renderWizard(kind);
    expect(screen.getAllByText(FIRST_STEP[kind]).length).toBeGreaterThan(0);
    expect(screen.getByText(`Step 1 of 4`)).toBeTruthy();
    view.unmount();
  });

  it('holds Continue closed until the step validates', () => {
    renderWizard('webhook');
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
  });
});

describe('filters and prompt fields', () => {
  async function chooseEvent(name: string) {
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    fireEvent.click(await screen.findByText(name));
  }

  function fillNameAndPrompt(text = 'go') {
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'gh' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = text;
    fireEvent.input(prompt);
  }

  it("lists the source's event types from its schema, and ones it has sent", async () => {
    renderWizard('webhook');
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    expect(await screen.findByText('issue_comment')).toBeTruthy();
    expect(screen.getByText('custom')).toBeTruthy();
    expect(screen.getByLabelText('Search events')).toBeTruthy();
  });

  it('holds Continue until an event is chosen', async () => {
    renderWizard('webhook');
    fillNameAndPrompt();
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
    await chooseEvent('issue_comment');
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);
  });

  it("offers the chosen event's fields, and meta, behind the Insert field picker", async () => {
    renderWizard('webhook');
    await chooseEvent('issue_comment');
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    expect(await screen.findByText('comment.body')).toBeTruthy();
    expect(screen.getByText('meta.event_type')).toBeTruthy();
  });

  it('inserts a placeholder and previews the first message with the data block', async () => {
    renderWizard('webhook');
    await chooseEvent('issue_comment');
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    fireEvent.click(await screen.findByText('comment.body'));
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    expect(prompt.querySelector('[data-slot=template-token]')).toBeTruthy();
    expect(screen.getByText('First message')).toBeTruthy();
    expect(screen.getByText(/<delivery-data>/)).toBeTruthy();
  });

  it('adds and removes a filter row', () => {
    renderWizard('webhook');
    expect(screen.queryByLabelText('Filter 1 field')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    expect(screen.getByLabelText('Filter 1 field')).toBeTruthy();
    expect(screen.getByLabelText('Filter 1 values')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Remove filter 1' }));
    expect(screen.queryByLabelText('Filter 1 field')).toBeNull();
  });

  it('picks a filter field, hints its allowed values, and blocks a half-written filter', async () => {
    renderWizard('webhook');
    fillNameAndPrompt();
    await chooseEvent('issue_comment');

    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Filter 1 field' }));
    fireEvent.click(await screen.findByText('What happened'));

    expect(screen.getByLabelText('Filter 1 values')).toHaveAttribute(
      'placeholder',
      'created | edited',
    );
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Finish or remove the empty filter')).toBeTruthy();

    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'created' } });
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);
  });

  it('completes a pattern field with the name the author enters', async () => {
    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[{ id: 'src-2', name: 'prod alerts', provider: 'alertmanager' }]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fillNameAndPrompt();
    await chooseEvent('alert.firing');
    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Filter 1 field' }));
    fireEvent.click(await screen.findByText('commonLabels.<name>'));
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'critical' } });

    // A pattern with no name is not a field yet.
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
    fireEvent.change(screen.getByLabelText('Filter 1 name'), { target: { value: 'severity' } });
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);
  });

  it('drops the typed senders when the source changes provider', async () => {
    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[
          { id: 'src-1', name: 'acme/widgets', provider: 'github' },
          { id: 'src-2', name: 'prod alerts', provider: 'alertmanager' },
        ]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.change(await screen.findByLabelText('Logins'), { target: { value: 'octocat' } });
    fireEvent.click(screen.getByText('Member'));

    fireEvent.click(screen.getByText('prod alerts'));
    fireEvent.click(screen.getByText('acme/widgets'));
    expect(((await screen.findByLabelText('Logins')) as HTMLInputElement).value).toBe('');
    expect(screen.getByRole('checkbox', { name: 'Member' })).not.toBeChecked();
  });

  it('offers a sender restriction only when the provider identifies senders', async () => {
    const view = renderWizard('webhook');
    expect(await screen.findByText('Senders')).toBeTruthy();
    expect(screen.getByLabelText('Logins')).toBeTruthy();
    expect(screen.getByText('Member')).toBeTruthy();
    view.unmount();

    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[{ id: 'src-2', name: 'prod alerts', provider: 'alertmanager' }]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    await screen.findByRole('button', { name: 'Event type' });
    expect(screen.queryByText('Senders')).toBeNull();
  });

  it("reads a new source's catalogue from the provider being created", async () => {
    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByText('New source'));
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    expect(await screen.findByText('issue_comment')).toBeTruthy();
    expect(api.fetchProviderEventTypes).toHaveBeenCalledWith('github');
    expect(api.fetchEventTypes).not.toHaveBeenCalled();
    expect(screen.queryByText('custom')).toBeNull();
  });
});

describe('shared steps', () => {
  const state = {
    ...initialState(TRIGGER_KINDS.webhook, CONTEXT.sources),
    name: 'github-push',
    agentTypeId: 'at-1',
  };
  const shared = {
    state,
    patch: vi.fn(),
    context: CONTEXT,
    definition: TRIGGER_KINDS.webhook,
    error: null,
  };

  it('asks the target step one question: which agent', () => {
    render(<StepTarget {...shared} />);
    expect(screen.getByText('Agent to run')).toBeTruthy();
    // Capacity is a rule about the queue, so it lives on Delivery with the
    // other two bounds rather than beside the agent picker.
    expect(screen.queryByText('Max parallel runs')).toBeNull();
    expect(screen.queryByText('Overlapping runs')).toBeNull();
  });

  it('gathers all three queue bounds on the delivery step, capacity first', () => {
    render(<StepDelivery {...shared} />);
    expect(screen.getByText('Overlapping runs')).toBeTruthy();
    expect(screen.getByText('Max parallel runs')).toBeTruthy();
    expect(screen.getByText('Max queued deliveries')).toBeTruthy();
    expect(screen.getByText('Timeout')).toBeTruthy();
    expect(screen.getByText('Suppress duplicates')).toBeTruthy();
  });

  it('summarises the three bounds as one sentence', () => {
    render(<StepDelivery {...shared} />);
    expect(
      screen.getByText(
        'Up to 4 at once. Further deliveries wait in order, up to 32 of them, and each has up to 30s to start once it is admitted.',
      ),
    ).toBeTruthy();
  });

  it('offers coalescing to a webhook that waits', () => {
    render(<StepDelivery {...shared} />);
    expect(screen.getByText('Coalesce bursts')).toBeTruthy();
  });

  it('hides coalescing unless overflow is wait', () => {
    render(<StepDelivery {...shared} state={{ ...state, overflow: 'replace' }} />);
    expect(screen.queryByText('Coalesce bursts')).toBeNull();
  });

  it('hides coalescing for scheduled and manual triggers', () => {
    render(<StepDelivery {...shared} definition={TRIGGER_KINDS.scheduled} />);
    expect(screen.queryByText('Coalesce bursts')).toBeNull();
  });

  it('hides the queue depth when nothing queues', () => {
    render(<StepDelivery {...shared} state={{ ...state, overflow: 'refuse' }} />);
    expect(screen.queryByText('Max queued deliveries')).toBeNull();
  });

  it('renders the review step, including a create error', () => {
    render(<StepReview {...shared} error="source already exists" />);
    expect(screen.getByText('source already exists')).toBeTruthy();
    // The overlap row follows its controls into the Delivery group.
    expect(screen.getByText('Up to 4 at once, then wait')).toBeTruthy();
  });
});

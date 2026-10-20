import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import type { TriggerEventTypes, TriggerField } from '@runkora/shared/api';

// The wizard reads each provider's schema catalogue from the API.
const GITHUB_FIELDS: Record<string, TriggerField[]> = {
  issue_comment: [
    { path: 'action', group: 'event', description: 'What happened', enum: ['created', 'edited'] },
    { path: 'comment.body', group: 'comment', description: 'The comment text' },
    { path: 'meta.event_type', group: 'meta', description: 'The event type' },
  ],
};
const CHAT_FIELDS: TriggerField[] = [
  { path: 'text', group: 'event', description: 'The message text' },
  { path: 'conversation.id', group: 'conversation', description: 'The conversation' },
  { path: 'thread.id', group: 'thread', description: 'The thread' },
  { path: 'user.id', group: 'user', description: 'The sender' },
];
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
const routingOptions = vi.hoisted(() => ({ useRoutingOptions: vi.fn(() => undefined) }));
vi.mock('./use-routing-options', () => routingOptions);

import { TriggerWizard } from './trigger-wizard';
import { StepDelivery } from './step-delivery';
import { StepReview } from './step-review';
import { StepTarget } from './step-target';
import { KIND_ORDER, PLANNED_KINDS, TRIGGER_KINDS } from './kinds';
import { initialState } from './wizard-state';
import type { ConsoleKind, TriggerWizardSource, WizardContext } from './types';

const CONTEXT: WizardContext = {
  agents: [{ id: 'at-1', slug: 'coder', name: 'Coder' }],
  sources: [{ id: 'src-1', name: 'acme/widgets', provider: 'github', category: 'webhook' }],
  ingestHost: 'orbit.local.clearroute.io:8443',
};

const ALERTS: TriggerWizardSource = {
  id: 'src-2',
  name: 'prod alerts',
  provider: 'alertmanager',
  category: 'webhook',
};
const SLACK: TriggerWizardSource = {
  id: 'src-3',
  name: 'acme slack',
  provider: 'slack',
  category: 'chat',
};

/** GitHub declares its roles; Alertmanager identifies no sender; Slack identifies senders without roles. */
const PROVIDER_EVENT_TYPES: Record<string, TriggerEventTypes> = {
  github: {
    declared: ['issue_comment', 'push'],
    category: 'webhook',
    senders: {
      roles: [
        { value: 'OWNER', label: 'Owner', description: 'Owns the repository' },
        { value: 'MEMBER', label: 'Member', description: 'Member of the organization' },
      ],
    },
  },
  alertmanager: { declared: ['alert.firing'], category: 'webhook' },
  slack: { declared: ['mention', 'direct_message'], category: 'chat', senders: { roles: [] } },
};

beforeEach(() => {
  vi.clearAllMocks();
  api.fetchProviderEventTypes.mockImplementation(async (provider: string) => ({
    eventTypes: PROVIDER_EVENT_TYPES[provider] ?? PROVIDER_EVENT_TYPES.github,
    error: null,
  }));
  api.fetchEventTypes.mockImplementation(async (sourceId: string) => ({
    eventTypes:
      sourceId === 'src-2'
        ? PROVIDER_EVENT_TYPES.alertmanager
        : sourceId === 'src-3'
          ? PROVIDER_EVENT_TYPES.slack
          : { ...PROVIDER_EVENT_TYPES.github, observed: ['custom'] },
    error: null,
  }));
  api.fetchProviderFields.mockImplementation(async (provider: string, eventType: string) => ({
    fields:
      provider === 'alertmanager'
        ? ALERT_FIELDS
        : provider === 'slack'
          ? CHAT_FIELDS
          : (GITHUB_FIELDS[eventType] ?? []),
    error: null,
  }));
});

function renderWizard(initialKind?: ConsoleKind, sources = CONTEXT.sources, onCreate = vi.fn()) {
  return render(
    <TriggerWizard
      initialKind={initialKind}
      agents={CONTEXT.agents}
      sources={sources}
      ingestHost={CONTEXT.ingestHost}
      onCreate={onCreate}
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

  it('offers Chat command as a kind that can be picked, not a planned one', () => {
    renderWizard();
    const chat = screen.getByRole('radio', { name: /Chat command/ });
    expect(chat.hasAttribute('disabled')).toBe(false);
    expect(PLANNED_KINDS.map((k) => k.title)).not.toContain('Chat command');
  });
});

describe('TriggerWizard step phase', () => {
  const FIRST_STEP: Record<ConsoleKind, string> = {
    webhook: 'Source',
    chat: 'Source',
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
        sources={[ALERTS]}
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
        sources={[...CONTEXT.sources, ALERTS]}
        ingestHost={CONTEXT.ingestHost}
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.change(await screen.findByLabelText('Allowed senders'), {
      target: { value: 'octocat' },
    });
    fireEvent.click(screen.getByText('Member'));

    fireEvent.click(screen.getByText('prod alerts'));
    fireEvent.click(screen.getByText('acme/widgets'));
    expect(((await screen.findByLabelText('Allowed senders')) as HTMLInputElement).value).toBe('');
    expect(screen.getByRole('checkbox', { name: 'Member' })).not.toBeChecked();
  });

  it('offers a sender restriction only when the provider identifies senders', async () => {
    const view = renderWizard('webhook');
    expect(await screen.findByText('Senders')).toBeTruthy();
    expect(screen.getByLabelText('Allowed senders')).toBeTruthy();
    expect(screen.getByText('Member')).toBeTruthy();
    view.unmount();

    render(
      <TriggerWizard
        initialKind="webhook"
        agents={CONTEXT.agents}
        sources={[ALERTS]}
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

describe('webhook kind beside chat sources', () => {
  it('lists only webhook sources, never a chat source', async () => {
    renderWizard('webhook', [...CONTEXT.sources, SLACK]);
    expect(screen.getByText('acme/widgets')).toBeTruthy();
    expect(screen.queryByText('acme slack')).toBeNull();
  });

  it('offers only the providers whose category is webhook', async () => {
    renderWizard('webhook', []);
    fireEvent.click(screen.getByText('New source'));
    expect(await screen.findByText('Prometheus Alertmanager')).toBeTruthy();
    expect(screen.getByText('GitHub')).toBeTruthy();
    expect(screen.queryByText('Slack')).toBeNull();
    expect(screen.queryByText('Discord')).toBeNull();
  });

  it('shows the role cards from the labels the provider declares', async () => {
    renderWizard('webhook');
    expect(await screen.findByText('Owns the repository')).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: 'Owner' })).toBeTruthy();
  });
});

describe('chat command kind', () => {
  function fillNameAndPrompt() {
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'ask' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = 'Answer {{ text }}';
    fireEvent.input(prompt);
  }

  it('lists only chat sources', () => {
    renderWizard('chat', [...CONTEXT.sources, SLACK]);
    expect(screen.getByText('acme slack')).toBeTruthy();
    expect(screen.queryByText('acme/widgets')).toBeNull();
  });

  it('starts on New source with Slack chosen when there is no chat source', async () => {
    renderWizard('chat', CONTEXT.sources);
    expect(screen.queryByText('acme/widgets')).toBeNull();
    expect(screen.getByText('Platform')).toBeTruthy();
    expect(await screen.findByText('manifest.json')).toBeTruthy();
    expect(screen.getByRole('radio', { name: /^Slack/ })).toHaveAttribute('aria-checked', 'true');
  });

  it('chooses no platform when Slack could not be loaded', async () => {
    api.fetchProviderEventTypes.mockImplementation(async (provider: string) =>
      provider === 'slack'
        ? { eventTypes: null, error: 'unavailable' }
        : { eventTypes: PROVIDER_EVENT_TYPES[provider], error: null },
    );
    renderWizard('chat', []);
    expect(await screen.findByText(/Could not load Slack/)).toBeTruthy();
    expect(screen.queryByText('manifest.json')).toBeNull();
  });

  it('names a prompt placeholder that is not a chat field before Continue', async () => {
    renderWizard('chat', [SLACK]);
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'ask' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = 'Answer {{ event.text }} for {{ user }}';
    fireEvent.input(prompt);
    fireEvent.click(await screen.findByRole('radio', { name: /Mention/ }));
    expect(
      await screen.findByText(
        'event.text is not a field of this message. Use one listed under Insert field.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);

    prompt.textContent = 'Answer {{ text }} for {{ user.id }}';
    fireEvent.input(prompt);
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false),
    );
  });

  it('keeps the message fields when the chosen message is picked again', async () => {
    renderWizard('chat', [SLACK]);
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'ask' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = 'Answer {{ text }}';
    fireEvent.input(prompt);
    const mention = await screen.findByRole('radio', { name: /Mention/ });
    fireEvent.click(mention);
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false),
    );
    fireEvent.click(mention);
    expect(screen.queryByText('Loading the message fields…')).toBeNull();
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false);
    expect(api.fetchProviderFields).toHaveBeenCalledTimes(1);
  });

  it('says so when the message fields could not be loaded', async () => {
    api.fetchProviderFields.mockResolvedValue({ fields: [], error: 'unavailable' });
    renderWizard('chat', [SLACK]);
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'ask' } });
    fireEvent.click(await screen.findByRole('radio', { name: /Mention/ }));
    expect(
      await screen.findByText(
        'Could not load the message fields. Close the wizard and open it again to retry.',
      ),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);
  });

  it('offers Slack, and shows Discord and Microsoft Teams as planned', async () => {
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    const slack = await screen.findByRole('radio', { name: /Slack/ });
    expect(slack.hasAttribute('disabled')).toBe(false);
    expect(screen.getByRole('radio', { name: /Discord/ }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('radio', { name: /Microsoft Teams/ }).hasAttribute('disabled')).toBe(
      true,
    );
    expect(screen.queryByText('GitHub')).toBeNull();
  });

  it('says so when a platform could not be loaded', async () => {
    api.fetchProviderEventTypes.mockImplementation(async (provider: string) =>
      provider === 'slack'
        ? { eventTypes: null, error: 'unavailable' }
        : { eventTypes: PROVIDER_EVENT_TYPES[provider], error: null },
    );
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    expect(await screen.findByText(/Could not load Slack/)).toBeTruthy();
    expect(screen.queryByRole('radio', { name: /^Slack/ })).toBeNull();
  });

  it('shows the Slack app manifest without a request URL and asks for the signing secret', async () => {
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    fireEvent.click(await screen.findByText('Slack'));
    expect(screen.getByText('manifest.json')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Copy code' })).toBeTruthy();
    expect(screen.getByLabelText(/Signing secret/)).toBeTruthy();
    expect(document.body.textContent).not.toContain('request_url');
    expect(api.fetchProviderEventTypes).toHaveBeenCalledWith('slack');
  });

  it('offers the chat events with readable labels', async () => {
    renderWizard('chat', [SLACK]);
    expect(await screen.findByRole('radio', { name: /Mention/ })).toBeTruthy();
    expect(screen.getByRole('radio', { name: /Direct message/ })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Event type' })).toBeNull();
  });

  it('offers the chat fields to the prompt and no role cards', async () => {
    renderWizard('chat', [SLACK]);
    fireEvent.click(await screen.findByRole('radio', { name: /Mention/ }));
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    expect(await screen.findByText('conversation.id')).toBeTruthy();
    expect(screen.getByLabelText('Allowed senders')).toHaveAttribute(
      'placeholder',
      'U012AB3CD, U045EF6GH',
    );
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('creates a webhook trigger on the new Slack source with its secret', async () => {
    const onCreate = vi.fn().mockResolvedValue({ ok: true, triggerId: 'trg-1' });
    renderWizard('chat', [], onCreate);
    fillNameAndPrompt();
    fireEvent.change(screen.getByLabelText(/Source name/), { target: { value: 'acme-slack' } });
    expect(await screen.findByText('Paste the signing secret from your Slack app.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: 'sig' } });
    fireEvent.click(await screen.findByRole('radio', { name: /Direct message/ }));

    const next = screen.getByRole('button', { name: /Continue/ });
    await waitFor(() => expect(next.hasAttribute('disabled')).toBe(false));
    fireEvent.click(next);
    fireEvent.click(screen.getByRole('radio', { name: 'Coder' }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));
    expect(screen.getByText('Chat command')).toBeTruthy();
    expect(screen.getAllByText('Direct message').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));

    expect(onCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'webhook',
        eventType: 'direct_message',
        source: { newName: 'acme-slack', provider: 'slack', signingSecret: 'sig' },
      }),
    );
  });

  it('reuses the source a failed attempt created when Create is retried', async () => {
    const onCreate = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        error: 'agent type not found',
        createdSource: { id: 'src-new', name: 'acme-slack', provider: 'slack' },
      })
      .mockResolvedValueOnce({ ok: true, triggerId: 'trg-1' });
    renderWizard('chat', [], onCreate);
    fillNameAndPrompt();
    fireEvent.change(screen.getByLabelText(/Source name/), { target: { value: 'acme-slack' } });
    fireEvent.change(await screen.findByLabelText(/Signing secret/), { target: { value: 'sig' } });
    fireEvent.click(await screen.findByRole('radio', { name: /Direct message/ }));
    const next = screen.getByRole('button', { name: /Continue/ });
    await waitFor(() => expect(next.hasAttribute('disabled')).toBe(false));
    fireEvent.click(next);
    fireEvent.click(screen.getByRole('radio', { name: 'Coder' }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));

    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));
    expect(await screen.findByText('agent type not found')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2));
    expect(onCreate.mock.calls[0]![0].source).toEqual({
      newName: 'acme-slack',
      provider: 'slack',
      signingSecret: 'sig',
    });
    expect(onCreate.mock.calls[1]![0].source).toEqual({ existingId: 'src-new' });
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
    expect(screen.getByText('Coalesce events')).toBeTruthy();
  });

  it('shows the key and window only when coalescing is on', () => {
    const { rerender } = render(<StepDelivery {...shared} />);
    expect(screen.queryByLabelText('Key')).toBeNull();
    rerender(<StepDelivery {...shared} state={{ ...state, coalesceEnabled: true }} />);
    expect(screen.getByLabelText('Key')).toBeTruthy();
    expect(screen.getByLabelText('Window')).toBeTruthy();
  });

  it('hides coalescing unless overflow is wait', () => {
    render(<StepDelivery {...shared} state={{ ...state, overflow: 'replace' }} />);
    expect(screen.queryByText('Coalesce events')).toBeNull();
  });

  it('offers coalescing to a chat command, keyed on the conversation in the example', () => {
    render(
      <StepDelivery
        {...shared}
        definition={TRIGGER_KINDS.chat}
        state={{ ...state, eventType: 'mention', coalesceEnabled: true }}
      />,
    );
    expect(screen.getByLabelText('Key')).toHaveAttribute(
      'placeholder',
      '{{ conversation.id }}:{{ thread.id }}',
    );
  });

  it('keys a direct message example on the conversation alone', () => {
    render(
      <StepDelivery
        {...shared}
        definition={TRIGGER_KINDS.chat}
        state={{ ...state, eventType: 'direct_message', coalesceEnabled: true }}
      />,
    );
    expect(screen.getByLabelText('Key')).toHaveAttribute('placeholder', '{{ conversation.id }}');
  });

  it('shows coalescing on the review step for a chat command', () => {
    render(<StepReview {...shared} definition={TRIGGER_KINDS.chat} />);
    expect(screen.getByText('Coalescing')).toBeTruthy();
  });

  it('hides coalescing for scheduled and manual triggers', () => {
    render(<StepDelivery {...shared} definition={TRIGGER_KINDS.scheduled} />);
    expect(screen.queryByText('Coalesce events')).toBeNull();
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

describe('let kora choose', () => {
  it.each([
    ['chat', true],
    ['webhook', true],
    ['scheduled', false],
  ] as const)('loads routing options for a %s trigger: %s', (kind, routed) => {
    render(
      <TriggerWizard
        initialKind={kind}
        agents={CONTEXT.agents}
        sources={CONTEXT.sources}
        ingestHost={CONTEXT.ingestHost}
        allowRouting
        onCreate={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(routingOptions.useRoutingOptions).toHaveBeenLastCalledWith(routed);
  });

  const routingContext = (models: { id: string; name: string }[]): WizardContext => ({
    ...CONTEXT,
    routing: {
      candidates: {
        status: 'ready',
        items: [
          {
            id: 'at-1',
            slug: 'coder',
            name: 'Coder',
            description: 'Implements a change from an issue and opens a pull request for it.',
            runnable: true,
          },
          {
            id: 'at-2',
            slug: 'billing',
            name: 'Billing',
            description: 'Reconciles invoices against usage and raises disputes.',
            runnable: false,
            reason: 'you do not hold agent:billing:run',
          },
          {
            id: 'at-3',
            slug: 'assistant',
            name: 'Assistant',
            description: '',
            identityMode: 'delegated',
            runnable: false,
            reason: 'delegated agents act for the person who runs them',
          },
        ],
      },
      models: { status: 'ready', items: models },
    },
  });
  const base = { ...initialState(TRIGGER_KINDS.webhook, CONTEXT.sources), name: 'route' };
  const props = (context: WizardContext, mode: 'one' | 'choose') => ({
    state: { ...base, routing: { ...base.routing, mode } },
    patch: vi.fn(),
    context,
    definition: TRIGGER_KINDS.webhook,
    error: null,
  });

  it('keeps the one-agent picker under the mode choice', () => {
    render(
      <StepTarget {...props(routingContext([{ id: 'liquid/d1', name: 'Liquid d1' }]), 'one')} />,
    );
    expect(screen.getByText('Route to one agent')).toBeTruthy();
    expect(screen.getByText('Agent to run')).toBeTruthy();
  });

  it('lists the candidates with the reason a disabled one cannot be picked', () => {
    render(
      <StepTarget {...props(routingContext([{ id: 'liquid/d1', name: 'Liquid d1' }]), 'choose')} />,
    );
    expect(screen.getByText('Candidate agents')).toBeTruthy();
    expect(screen.getByText('Cannot be picked: you do not hold agent:billing:run.')).toBeTruthy();
    expect(
      screen.getByText('Cannot be picked: delegated agents act for the person who runs them.'),
    ).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: 'Coder' })).toBeTruthy();
    expect(screen.queryByRole('checkbox', { name: 'Billing' })).toBeNull();
    expect(screen.getByText('Liquid d1')).toBeTruthy();
  });

  it.each([
    ['checkbox', () => screen.getByRole('checkbox', { name: 'Coder' })],
    ['card', () => screen.getByText('Coder')],
  ])('selects a candidate once when its %s is clicked', (_, target) => {
    const p = props(routingContext([{ id: 'liquid/d1', name: 'Liquid d1' }]), 'choose');
    render(<StepTarget {...p} />);
    fireEvent.click(target());
    expect(p.patch).toHaveBeenCalledTimes(1);
    expect(p.patch).toHaveBeenCalledWith({
      routing: expect.objectContaining({ candidateIds: ['at-1'] }),
    });
  });

  it('hides the routing settings when no decision model is enabled', () => {
    render(<StepTarget {...props(routingContext([]), 'choose')} />);
    expect(screen.getByText('No decision model is enabled for this workspace.')).toBeTruthy();
    expect(screen.queryByText('Candidate agents')).toBeNull();
    expect(screen.queryByText('Confidence')).toBeNull();
  });

  it('offers one agent only when routing is not offered', () => {
    render(<StepTarget {...props(CONTEXT, 'choose')} />);
    expect(screen.queryByText('Let kora choose')).toBeNull();
    expect(screen.getByText('Agent to run')).toBeTruthy();
  });

  it.each([
    ['mention', '{{ conversation.id }}:{{ thread.id }}'],
    ['direct_message', '{{ conversation.id }}'],
  ])('suggests the chat join key for a %s', (eventType, example) => {
    const chat = initialState(TRIGGER_KINDS.chat, []);
    render(
      <StepTarget
        {...props(routingContext([{ id: 'liquid/d1', name: 'Liquid d1' }]), 'choose')}
        state={{ ...chat, eventType, routing: { ...chat.routing, mode: 'choose' } }}
        definition={TRIGGER_KINDS.chat}
      />,
    );
    expect(screen.getByLabelText('Join key')).toHaveAttribute('placeholder', example);
  });

  it('reviews the candidates and settings of a routed trigger', () => {
    const context = routingContext([{ id: 'liquid/d1', name: 'Liquid d1' }]);
    render(
      <StepReview
        {...props(context, 'choose')}
        state={{
          ...base,
          routing: {
            ...base.routing,
            mode: 'choose',
            candidateIds: ['at-1', 'at-2'],
            modelId: 'liquid/d1',
            fallbackId: 'at-1',
          },
        }}
      />,
    );
    expect(screen.getByText('Coder, Billing')).toBeTruthy();
    expect(screen.getByText('Send to Coder')).toBeTruthy();
  });
});

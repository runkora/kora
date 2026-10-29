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
const GENERIC_FIELDS: TriggerField[] = [
  { path: 'meta.event_type', group: 'meta', description: 'The event type' },
];
const api = vi.hoisted(() => ({
  fetchEventTypes: vi.fn(),
  fetchProviderEventTypes: vi.fn(),
  fetchProviderFields: vi.fn(),
  createSource: vi.fn(),
  updateSource: vi.fn(),
}));
vi.mock('../../_services/triggers-api', () => api);
const routingOptions = vi.hoisted(() => ({ useRoutingOptions: vi.fn(() => undefined) }));
vi.mock('./use-routing-options', () => routingOptions);

const refresh = vi.hoisted(() => vi.fn());
vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh }) }));

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
  ingestHost: 'local.runkora.io:8443',
};

const ALERTS: TriggerWizardSource = {
  id: 'src-2',
  name: 'prod alerts',
  provider: 'alertmanager',
  category: 'webhook',
};
const GENERIC: TriggerWizardSource = {
  id: 'src-4',
  name: 'linear',
  provider: 'generic',
  category: 'webhook',
  config: { verification: { method: 'hmac_sha256', header: 'Linear-Signature' } },
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
  generic: { declared: [], category: 'webhook' },
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
        : sourceId === GENERIC.id
          ? { declared: [], observed: ['issue.created'], category: 'webhook' }
          : sourceId === 'src-3'
            ? PROVIDER_EVENT_TYPES.slack
            : { ...PROVIDER_EVENT_TYPES.github, observed: ['custom'] },
    error: null,
  }));
  api.fetchProviderFields.mockImplementation(async (provider: string, eventType: string) => ({
    fields:
      provider === 'alertmanager'
        ? ALERT_FIELDS
        : provider === 'generic'
          ? GENERIC_FIELDS
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

    fireEvent.mouseEnter(screen.getByLabelText('Filter 1 values'));
    fireEvent.click(screen.getByRole('button', { name: 'Remove filter 1' }));
    expect(screen.queryByLabelText('Filter 1 field')).toBeNull();
  });

  it('picks a filter field and blocks a half-written filter', async () => {
    renderWizard('webhook');
    fillNameAndPrompt();
    await chooseEvent('issue_comment');

    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Filter 1 field' }));
    fireEvent.click(await screen.findByText('What happened'));

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
    fireEvent.click(await screen.findByRole('radio', { name: 'Specific senders' }));
    fireEvent.change(await screen.findByLabelText('Additional senders'), {
      target: { value: 'octocat' },
    });
    fireEvent.click(screen.getByText('Member'));

    fireEvent.click(screen.getByText('prod alerts'));
    fireEvent.click(screen.getByText('acme/widgets'));
    // Back on a provider that identifies senders, the restriction starts over as anyone.
    expect(await screen.findByRole('radio', { name: 'Anyone' })).toBeChecked();
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
    expect(
      ((await screen.findByLabelText('Additional senders')) as HTMLTextAreaElement).value,
    ).toBe('');
    expect(screen.getByRole('checkbox', { name: 'Member' })).not.toBeChecked();
  });

  it('offers a sender restriction only when the provider identifies senders', async () => {
    const view = renderWizard('webhook');
    expect(await screen.findByText('Senders')).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
    expect(screen.getByLabelText('Additional senders')).toBeTruthy();
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
    fireEvent.click(await screen.findByRole('radio', { name: 'Specific senders' }));
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
    expect(await screen.findByRole('button', { name: 'Create source' })).toBeTruthy();
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

  /** Names the new Slack source and creates it, as the source step asks. */
  async function createSlackSource() {
    fireEvent.change(screen.getByLabelText(/Source name/), { target: { value: 'acme-slack' } });
    fireEvent.click(await screen.findByText('Slack'));
    fireEvent.click(screen.getByRole('button', { name: 'Create source' }));
    await screen.findByText('manifest.json');
  }

  async function saveSigningSecret() {
    fireEvent.change(screen.getByLabelText(/Signing secret/), { target: { value: ' sig ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save secret' }));
    await screen.findByText('Signing secret saved.');
  }

  async function chooseEventAndContinue() {
    fireEvent.click(await screen.findByRole('radio', { name: /Direct message/ }));
    const next = screen.getByRole('button', { name: /Continue/ });
    await waitFor(() => expect(next.hasAttribute('disabled')).toBe(false));
    fireEvent.click(next);
  }

  beforeEach(() => {
    api.createSource.mockResolvedValue({ source: { id: 'src-new' }, error: null });
    api.updateSource.mockResolvedValue({ source: { id: 'src-new' }, error: null });
  });

  it('creates the Slack source with no signing secret before it asks for one', async () => {
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    fireEvent.click(await screen.findByText('Slack'));
    expect(screen.queryByText('manifest.json')).toBeNull();
    expect(screen.queryByLabelText(/Signing secret/)).toBeNull();
    expect(document.body.textContent).not.toContain('<issued on create>');
    expect(api.createSource).not.toHaveBeenCalled();

    await createSlackSource();
    expect(api.createSource).toHaveBeenCalledWith({ name: 'acme-slack', provider: 'slack' });
    expect(refresh).toHaveBeenCalled();
    expect(screen.getByLabelText(/Signing secret/)).toBeTruthy();
  });

  it('shows the manifest and the request URL of the created source', async () => {
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    await createSlackSource();
    const url = `https://${CONTEXT.ingestHost}/api/v1/webhooks/src-new`;
    expect(screen.getByRole('button', { name: 'Copy code' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Copy request URL' })).toBeTruthy();
    expect(screen.getAllByText(url, { exact: false }).length).toBeGreaterThan(0);
    expect(document.body.textContent).toContain('"request_url"');
  });

  it('does not continue until the signing secret is saved', async () => {
    renderWizard('chat', []);
    fillNameAndPrompt();
    fireEvent.click(screen.getByText('New source'));
    await createSlackSource();
    fireEvent.click(await screen.findByRole('radio', { name: /Direct message/ }));
    expect(screen.getByText('Save the signing secret from your Slack app.')).toBeTruthy();
    expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(true);

    await saveSigningSecret();
    expect(api.updateSource).toHaveBeenCalledWith('src-new', { signing_secret: 'sig' });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Continue/ }).hasAttribute('disabled')).toBe(false),
    );
  });

  it('keeps the wizard on the source step when the source cannot be created', async () => {
    api.createSource.mockResolvedValue({ source: null, error: 'name already taken' });
    renderWizard('chat', []);
    fireEvent.click(screen.getByText('New source'));
    fireEvent.change(screen.getByLabelText(/Source name/), { target: { value: 'acme-slack' } });
    fireEvent.click(await screen.findByText('Slack'));
    fireEvent.click(screen.getByRole('button', { name: 'Create source' }));
    expect(await screen.findByText('name already taken')).toBeTruthy();
    expect(screen.queryByText('manifest.json')).toBeNull();
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
    fireEvent.click(screen.getByRole('radio', { name: 'Specific senders' }));
    fireEvent.click(screen.getByRole('button', { name: /Insert field/ }));
    expect(await screen.findByText('conversation.id')).toBeTruthy();
    expect(screen.getByLabelText('Allowed senders')).toHaveAttribute(
      'placeholder',
      'U012AB3CD, U045EF6GH',
    );
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  it('creates the trigger on the source the source step created', async () => {
    const onCreate = vi.fn().mockResolvedValue({ ok: true, triggerId: 'trg-1' });
    renderWizard('chat', [], onCreate);
    fillNameAndPrompt();
    await createSlackSource();
    await saveSigningSecret();
    await chooseEventAndContinue();
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
        source: { existingId: 'src-new' },
      }),
    );
    expect(api.createSource).toHaveBeenCalledTimes(1);
  });

  it('reuses the created source when Create is retried', async () => {
    const onCreate = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, error: 'agent type not found' })
      .mockResolvedValueOnce({ ok: true, triggerId: 'trg-1' });
    renderWizard('chat', [], onCreate);
    fillNameAndPrompt();
    await createSlackSource();
    await saveSigningSecret();
    await chooseEventAndContinue();
    fireEvent.click(screen.getByRole('radio', { name: 'Coder' }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));
    fireEvent.click(screen.getByRole('button', { name: /Continue/ }));

    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));
    expect(await screen.findByText('agent type not found')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2));
    expect(onCreate.mock.calls[0]![0].source).toEqual({ existingId: 'src-new' });
    expect(onCreate.mock.calls[1]![0].source).toEqual({ existingId: 'src-new' });
    expect(api.createSource).toHaveBeenCalledTimes(1);
  });
});

describe('generic webhook source', () => {
  function fillNameAndPrompt() {
    fireEvent.change(screen.getByLabelText(/Trigger name/), { target: { value: 'linear' } });
    const prompt = screen.getByRole('textbox', { name: 'Prompt' });
    prompt.textContent = 'Triage {{ data.title }}';
    fireEvent.input(prompt);
  }

  async function chooseGeneric() {
    fireEvent.click(screen.getByText('New source'));
    fireEvent.change(screen.getByLabelText(/Source name/), { target: { value: 'linear' } });
    fireEvent.click(await screen.findByText('Generic webhook'));
  }

  const continueButton = () => screen.getByRole('button', { name: /Continue/ });

  it('asks for the signature header, prefix and encoding of an HMAC source', async () => {
    renderWizard('webhook', []);
    fillNameAndPrompt();
    await chooseGeneric();
    expect(screen.getByText('Verification')).toBeTruthy();
    expect(screen.getByText('Choose how deliveries are verified.')).toBeTruthy();

    fireEvent.click(screen.getByText('HMAC-SHA256'));
    expect(screen.getByText('Name the header that carries the signature.')).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Signature header/), {
      target: { value: 'Linear-Signature' },
    });
    expect(screen.getByLabelText(/Prefix/)).toBeTruthy();
    expect(screen.getByRole('group', { name: 'Encoding' })).toBeTruthy();
    expect(continueButton().hasAttribute('disabled')).toBe(false);
  });

  it('offers to generate or paste the secret, and asks for a pasted one', async () => {
    renderWizard('webhook', []);
    fillNameAndPrompt();
    await chooseGeneric();
    fireEvent.click(screen.getByText('Standard Webhooks'));
    expect(screen.queryByLabelText(/^Secret/)).toBeNull();

    fireEvent.click(screen.getByText("Paste the sender's secret"));
    expect(screen.getByText("Paste the sender's signing secret.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/^Secret/), { target: { value: 'whsec_abc' } });
    expect(continueButton().hasAttribute('disabled')).toBe(false);
  });

  it('always generates a bearer token', async () => {
    renderWizard('webhook', []);
    await chooseGeneric();
    fireEvent.click(screen.getByText('Bearer token'));
    expect(screen.queryByText("Paste the sender's secret")).toBeNull();
    expect(screen.getByText(/generates the token and shows it once/)).toBeTruthy();
  });

  it('starts on the event type webhook and takes a typed one once a location is set', async () => {
    renderWizard('webhook', []);
    await chooseGeneric();
    const eventType = () => screen.getByRole('textbox', { name: 'Event type' });
    await waitFor(() => expect(eventType()).toHaveValue('webhook'));

    fireEvent.click(screen.getByText('Payload field'));
    expect(eventType()).toHaveValue('');
    fireEvent.change(screen.getByLabelText('Event type field'), { target: { value: 'type' } });
    fireEvent.change(eventType(), { target: { value: 'Issue' } });
    expect(eventType()).toHaveValue('Issue');
  });

  it('starts an existing generic source with no event type location on webhook', async () => {
    renderWizard('webhook', [{ ...GENERIC, config: { verification: { method: 'bearer' } } }]);
    expect(await screen.findByRole('textbox', { name: 'Event type' })).toHaveValue('webhook');
  });

  it('refuses an event type that is only whitespace', async () => {
    renderWizard('webhook', [GENERIC]);
    fillNameAndPrompt();
    fireEvent.change(await screen.findByRole('textbox', { name: 'Event type' }), {
      target: { value: '   ' },
    });
    expect(screen.getByText('Choose the event this trigger listens for.')).toBeTruthy();
    expect(continueButton().hasAttribute('disabled')).toBe(true);
  });

  it('trims the typed event type in the draft', async () => {
    const onCreate = vi.fn().mockResolvedValue({ ok: true, triggerId: 'trg-1' });
    renderWizard('webhook', [GENERIC], onCreate);
    fillNameAndPrompt();
    fireEvent.change(await screen.findByRole('textbox', { name: 'Event type' }), {
      target: { value: ' Issue ' },
    });
    fireEvent.click(continueButton());
    fireEvent.click(screen.getByRole('radio', { name: 'Coder' }));
    fireEvent.click(continueButton());
    fireEvent.click(continueButton());
    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));
    expect(onCreate).toHaveBeenCalledWith(expect.objectContaining({ eventType: 'Issue' }));
  });

  it('resets the event type to webhook when the location goes back to None', async () => {
    renderWizard('webhook', []);
    await chooseGeneric();
    const eventType = () => screen.getByRole('textbox', { name: 'Event type' });
    await waitFor(() => expect(eventType()).toHaveValue('webhook'));
    fireEvent.click(screen.getByText('Header'));
    fireEvent.change(eventType(), { target: { value: 'Issue' } });
    fireEvent.click(screen.getByText('None'));
    expect(eventType()).toHaveValue('webhook');
  });

  it('still clears the filters when a GitHub event is picked', async () => {
    renderWizard('webhook');
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    fireEvent.click(await screen.findByText('issue_comment'));
    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    expect(screen.getByLabelText('Filter 1 values')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Event type' }));
    fireEvent.click(await screen.findByText('push'));
    expect(screen.queryByLabelText('Filter 1 values')).toBeNull();
  });

  it('suggests the event types an existing source has received', async () => {
    renderWizard('webhook', [GENERIC]);
    fireEvent.change(await screen.findByRole('textbox', { name: 'Event type' }), {
      target: { value: 'x' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Suggestions for event type' }));
    fireEvent.click(await screen.findByText('issue.created'));
    expect(screen.getByRole('textbox', { name: 'Event type' })).toHaveValue('issue.created');
  });

  it('takes a typed filter field and refuses one that is not a path', async () => {
    renderWizard('webhook', [GENERIC]);
    fillNameAndPrompt();
    fireEvent.change(await screen.findByRole('textbox', { name: 'Event type' }), {
      target: { value: 'Issue' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Add filter/ }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter 1 field' }), {
      target: { value: 'data.state name' },
    });
    fireEvent.change(screen.getByLabelText('Filter 1 values'), { target: { value: 'open' } });
    expect(screen.getByText(/data.state name is not a field path/)).toBeTruthy();

    fireEvent.change(screen.getByRole('textbox', { name: 'Filter 1 field' }), {
      target: { value: 'data.state' },
    });
    expect(continueButton().hasAttribute('disabled')).toBe(false);
  });

  it('offers no sender restriction', async () => {
    renderWizard('webhook', [GENERIC]);
    await screen.findByRole('textbox', { name: 'Event type' });
    expect(screen.queryByText('Senders')).toBeNull();
  });

  it('sends the config and the pasted secret with the new source', async () => {
    const onCreate = vi.fn().mockResolvedValue({ ok: true, triggerId: 'trg-1' });
    renderWizard('webhook', [], onCreate);
    fillNameAndPrompt();
    await chooseGeneric();
    fireEvent.click(screen.getByText('HMAC-SHA256'));
    fireEvent.change(screen.getByLabelText(/Signature header/), { target: { value: 'X-Sig' } });
    fireEvent.change(screen.getByLabelText(/Prefix/), { target: { value: 'sha256=' } });
    fireEvent.click(screen.getByText("Paste the sender's secret"));
    fireEvent.change(screen.getByLabelText(/^Secret/), { target: { value: ' s3cret ' } });
    fireEvent.click(screen.getByText('Header'));
    fireEvent.change(screen.getByLabelText('Event type header'), {
      target: { value: 'X-Event-Key' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: 'Event type' }), {
      target: { value: 'Issue' },
    });
    fireEvent.click(continueButton());
    fireEvent.click(screen.getByRole('radio', { name: 'Coder' }));
    fireEvent.click(continueButton());
    fireEvent.click(continueButton());
    expect(screen.getByText('HMAC-SHA256, pasted secret')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Create trigger/ }));

    expect(onCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        eventType: 'Issue',
        source: {
          newName: 'linear',
          provider: 'generic',
          config: {
            verification: {
              method: 'hmac_sha256',
              header: 'X-Sig',
              prefix: 'sha256=',
              encoding: 'hex',
            },
            event_type: { header: 'X-Event-Key' },
          },
          signingSecret: 's3cret',
        },
      }),
    );
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
    expect(screen.queryByText('Capacity')).toBeNull();
  });

  it('gathers all three queue bounds on the delivery step, capacity first', () => {
    render(<StepDelivery {...shared} />);
    expect(screen.getByText('Capacity')).toBeTruthy();
    expect(screen.getByText('Max parallel runs')).toBeTruthy();
    expect(screen.getByText('Queue size')).toBeTruthy();
    expect(screen.getByText('Time to start')).toBeTruthy();
    expect(screen.getByText('Suppress duplicates')).toBeTruthy();
  });

  it('summarises the three bounds as one sentence', () => {
    render(<StepDelivery {...shared} />);
    const summary = screen.getByText(/more wait in order/);
    expect(summary.textContent).toBe(
      'Up to 4 run at once. Up to 32 more wait in order, and each has 30 seconds to start once admitted.',
    );
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
